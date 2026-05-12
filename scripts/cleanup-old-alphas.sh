#!/usr/bin/env bash
#
# cleanup-old-alphas.sh — drop GitHub Releases + GHCR container image
# versions for alpha tags outside the safety window.
#
# Policy:
#   - Keep the latest N alpha releases by creation date (KEEP_LAST_N).
#   - Keep everything tagged within the last KEEP_DAYS, even if
#     more than N.
#   - Apply to the four release-pipeline images: management, signal,
#     relay, dashboard. (charts/* OCI artifacts are handled by Helm's
#     own retention. `dex` is the subchart's upstream image.)
#   - Git tags are NOT touched — they cost ~100 bytes each and are
#     useful for archaeological `git log` against a SHA.
#
# Default mode is dry-run. Pass --apply to actually delete.
#
#   ./scripts/cleanup-old-alphas.sh              # dry-run
#   ./scripts/cleanup-old-alphas.sh --apply      # for real
#
# Requires: gh CLI, jq, an authenticated session with admin on
# openzro/openzro + write:packages on the openzro org.

set -euo pipefail

# ─── policy ────────────────────────────────────────────────────────────
KEEP_LAST_N=10
KEEP_DAYS=7
PACKAGES=(management signal relay dashboard)
ORG=openzro
RELEASE_REPO=openzro/openzro

# ─── flags ─────────────────────────────────────────────────────────────
APPLY=false
[[ "${1:-}" == "--apply" ]] && APPLY=true

if $APPLY; then
  echo "APPLY MODE — deletions will happen"
else
  echo "DRY-RUN MODE — nothing will be deleted (use --apply to commit)"
fi
echo "policy: keep last $KEEP_LAST_N alphas + everything from last $KEEP_DAYS days"
echo

CUTOFF=$(date -u -d "$KEEP_DAYS days ago" +%Y-%m-%dT%H:%M:%SZ)

# ─── identify victims ──────────────────────────────────────────────────
mapfile -t ALL_ALPHAS < <(
  gh release list --repo "$RELEASE_REPO" --limit 1000 \
    --json tagName,createdAt \
    --jq '.[] | select(.tagName | test("alpha")) | "\(.createdAt)|\(.tagName)"' \
    | sort -r
)

declare -A KEEP_TAGS
i=0
for entry in "${ALL_ALPHAS[@]}"; do
  created="${entry%%|*}"
  tag="${entry##*|}"
  if [[ $i -lt $KEEP_LAST_N ]]; then
    KEEP_TAGS["$tag"]=1
  elif [[ "$created" > "$CUTOFF" ]]; then
    KEEP_TAGS["$tag"]=1
  fi
  i=$((i+1))
done

DELETE_TAGS=()
for entry in "${ALL_ALPHAS[@]}"; do
  tag="${entry##*|}"
  if [[ -z "${KEEP_TAGS[$tag]:-}" ]]; then
    DELETE_TAGS+=("$tag")
  fi
done

echo "total alpha releases:  ${#ALL_ALPHAS[@]}"
echo "keeping (window):      ${#KEEP_TAGS[@]}"
echo "delete candidates:     ${#DELETE_TAGS[@]}"
echo

if [[ ${#DELETE_TAGS[@]} -eq 0 ]]; then
  echo "Nothing to do."
  exit 0
fi

echo "=== TAGS TO DELETE (releases + container images) ==="
for tag in "${DELETE_TAGS[@]}"; do
  echo "  $tag"
done
echo

echo "=== TAGS TO KEEP ==="
for entry in "${ALL_ALPHAS[@]}"; do
  tag="${entry##*|}"
  created="${entry%%|*}"
  if [[ -n "${KEEP_TAGS[$tag]:-}" ]]; then
    printf "  %-25s (created %s)\n" "$tag" "${created%T*}"
  fi
done
echo

echo "=== EXECUTION PLAN ==="
for tag in "${DELETE_TAGS[@]}"; do
  echo "  release  > gh release delete $tag --yes --cleanup-tag=false"
  image_tag="${tag#v}"
  for pkg in "${PACKAGES[@]}"; do
    ids=$(
      gh api --paginate \
        "/orgs/$ORG/packages/container/$pkg/versions?per_page=100" 2>/dev/null \
        | jq -r --arg t "$image_tag" '
            .[] |
            select(
              .metadata.container.tags as $tags |
              [$t, ($t + "-amd64"), ($t + "-arm64")] | any(. as $x | $tags | index($x))
            ) | .id'
    )
    if [[ -n "$ids" ]]; then
      while read -r id; do
        [[ -z "$id" ]] && continue
        echo "  image    > DELETE /orgs/$ORG/packages/container/$pkg/versions/$id  (tag $image_tag)"
      done <<< "$ids"
    fi
  done
done

if ! $APPLY; then
  echo
  echo "end of dry-run.  Re-run with --apply to execute."
  exit 0
fi

echo
echo "=== APPLYING ==="
for tag in "${DELETE_TAGS[@]}"; do
  echo "$tag"
  gh release delete "$tag" --repo "$RELEASE_REPO" --yes --cleanup-tag=false || true

  image_tag="${tag#v}"
  for pkg in "${PACKAGES[@]}"; do
    ids=$(
      gh api --paginate \
        "/orgs/$ORG/packages/container/$pkg/versions?per_page=100" 2>/dev/null \
        | jq -r --arg t "$image_tag" '
            .[] |
            select(
              .metadata.container.tags as $tags |
              [$t, ($t + "-amd64"), ($t + "-arm64")] | any(. as $x | $tags | index($x))
            ) | .id'
    )
    while read -r id; do
      [[ -z "$id" ]] && continue
      echo "  deleting $pkg version $id ($image_tag)"
      gh api -X DELETE "/orgs/$ORG/packages/container/$pkg/versions/$id" || true
    done <<< "$ids"
  done
done

echo
echo "done."
