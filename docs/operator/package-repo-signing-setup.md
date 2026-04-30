# One-time setup: signing key for pkg.openzro.io

The `publish_packages` job in
[`.github/workflows/release-binaries.yml`](../../.github/workflows/release-binaries.yml)
signs the APT `Release` / `InRelease` files and the RPM
`repodata/repomd.xml` so operators can trust packages installed from
`pkg.openzro.io`. This page is the one-time setup for generating
that signing key and stashing it as GitHub Actions secrets.

If those secrets aren't configured the publish job will fail and the
release will still ship binaries on GitHub Releases — operators
just won't be able to use the apt/yum repo until the secrets are in
place.

## Generate the key

On a trusted local machine (not CI), produce a fresh dedicated key.

The `%no-protection` line below skips the passphrase, which is fine
for a CI signing key — both the key and any passphrase would live
in the same secret store, so passphrase protection adds no defense
in practice. If you prefer one anyway, drop the `%no-protection`
line and you'll be prompted; then set `PKG_GPG_PASSPHRASE` to the
value you chose.

```sh
gpg --batch --gen-key <<'EOF'
%no-protection
Key-Type: rsa
Key-Length: 4096
Name-Real: openZro Package Signing
Name-Email: dev@openzro.io
Expire-Date: 0
EOF
```

Find the new key ID:

```sh
gpg --list-secret-keys --keyid-format=long
# look for: sec   rsa4096/<KEYID> <date> [SC]
```

## Export the secret material

```sh
# Private key (passphrase-protected) — goes into PKG_GPG_PRIVATE_KEY
gpg --armor --export-secret-keys <KEYID> > /tmp/openzro-pkg.priv.asc

# Public key — committed to gh-pages (operators import it)
gpg --armor --export <KEYID> > /tmp/openzro-pkg.pub.asc
```

## Stash secrets in the repo

Repository → Settings → Secrets and variables → Actions →
**New repository secret** for each:

| Secret | Value |
|---|---|
| `PKG_GPG_PRIVATE_KEY` | contents of `/tmp/openzro-pkg.priv.asc` |
| `PKG_GPG_PASSPHRASE` | the passphrase you set when generating, **or empty** if you used `%no-protection` |

## Stash the public key in gh-pages

The publish job re-exports the public key on every run, so this is
mostly a safety net for operators who lose the keyring during an
upgrade and need to rebootstrap. Commit the armored public key once:

```sh
git checkout gh-pages   # branch will be created on first publish run
mkdir -p .              # if you're starting fresh
cp /tmp/openzro-pkg.pub.asc openzro-archive-key.asc
git add openzro-archive-key.asc
git commit -m "add public package signing key"
git push origin gh-pages
```

## Verify

1. Cut a release tag (e.g. `v0.1.0-alpha.3`).
2. The `publish_packages` job runs after the main `release` job.
3. Visit https://pkg.openzro.io/ — landing page should appear.
4. Test the install path on a fresh Linux box per the
   [Linux install guide](https://docs.openzro.io/get-started/install/linux).

## Rotation

If the key is ever exposed:

1. Generate a fresh key (steps above).
2. Update `PKG_GPG_PRIVATE_KEY` and `PKG_GPG_PASSPHRASE` secrets.
3. Replace `openzro-archive-key.asc` on `gh-pages`.
4. Cut a new release tag — the job re-signs metadata with the new key.
5. Operators re-import the key on next install (the install commands
   in [`linux.mdx`](../src/pages/get-started/install/linux.mdx) fetch
   the public key from the URL, so they pick up the new one
   automatically).

The old key signature does **not** invalidate the binaries themselves
(those are checksummed independently). Only the repo metadata signing
changes.

## Cloudflare cache purge (optional)

`pkg.openzro.io` is served from `gh-pages` through Cloudflare. By
default GitHub Pages sets `Cache-Control: max-age=600`, so APT / YUM /
pacman clients see a new release index within ~10 minutes of cutting a
tag. To make new releases visible immediately, the `publish_packages`
job purges the index files from the Cloudflare edge after a
successful push.

This is opt-in: the step only fires when both secrets below are
present, and skips silently otherwise. Forks without a Cloudflare
account are unaffected.

### One-time setup

1. **Cloudflare API token** —
   `My Profile → API Tokens → Create Token → Custom token`:
   * Permission: `Zone → Cache Purge → Purge`.
   * Zone Resources: `Include → Specific zone → openzro.io` (or your
     vanity domain).
   * Save the token (Cloudflare shows it once).

2. **Zone ID** — `Cloudflare Dashboard → openzro.io → Overview` (right
   sidebar).

3. **GitHub secrets** — repo `openzro/openzro → Settings → Secrets and
   variables → Actions → New repository secret`:
   * `CF_API_TOKEN` — value from step 1.
   * `CF_ZONE_ID` — value from step 2.

### What the step purges

The job calls `POST /zones/$CF_ZONE_ID/purge_cache` with a `files`
list of just the **index** files (apt's
`Packages` / `InRelease` / `Release`, yum's `repomd.xml`, pacman's
`openzro.db` / `openzro.files`, and `/latest.json`). Package binaries
(`.deb`, `.rpm`, `.pkg.tar.zst`, `.msi`, `.pkg`) are **not** purged —
they're immutable per filename (every alpha.N has a unique URL), so
cached copies stay valid and the long-TTL CDN window keeps origin
load low.

### TTL recommendations (Cloudflare Cache Rules, free tier)

Pair the purge with two Cache Rules under
`Caching → Cache Rules → Create rule`:

```
Rule 1 — Index files (short TTL, freshness)
  When: hostname=pkg.openzro.io AND URI Path matches regex
        (Packages\.gz$|InRelease$|^/.*Release$|repomd\.xml$|\.db$|\.files$|/latest\.json$)
  Then: Edge TTL = 60s, Browser TTL = 60s

Rule 2 — Package files (long TTL, immutable per filename)
  When: hostname=pkg.openzro.io AND URI Path matches regex
        (\.deb$|\.rpm$|\.pkg\.tar\.zst$|\.tar\.gz$|\.msi$|\.pkg$)
  Then: Edge TTL = 30 days, Browser TTL = 7 days
```

With both pieces in place: operator runs `apt update` (or `pacman -Sy`,
or `dnf check-update`) immediately after a release and sees the new
version. Existing clients with cached package files keep their fast
downloads.
