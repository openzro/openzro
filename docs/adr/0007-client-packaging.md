# ADR-0007: Client packaging — native installers, signing, and the package matrix

- **Status**: Accepted (Phase 0 + Phase 1 shipped in `v0.53.1-alpha.1`,
  2026-04-29; Phase 2 + Phase 3 tracked as GitHub issues #1–#7 with
  the `packaging` label)
- **Date**: 2026-04-28
- **Decision-makers**: openZro maintainers

## Context

The openZro client agent runs on Linux, macOS, and Windows. Today
the release pipeline (`.github/workflows/release-binaries.yml` →
goreleaser) produces:

- `.deb` and `.rpm` packages (via `nfpms`), published to
  [pkg.openzro.io](https://pkg.openzro.io) by `scripts/publish-packages.sh`
- Plain CLI / UI binary archives (`.tar.gz` for Linux+macOS, `.zip`
  for Windows) attached to GitHub Releases
- A distro-detecting `release_files/install.sh` that wraps the above
  (apt / yum / dnf / zypper / pacman → AUR / brew / binary fallback),
  served at `https://pkg.openzro.io/install.sh` after the publish-
  packages script runs

What's **missing**, all of it material to a customer-ready install
flow:

| Gap | Impact |
|---|---|
| No Windows `.msi` installer | Customers extract a `.zip`, run `openzro-ui.exe` as admin manually. `client/openzro.wxs` (WiX 4) and `client/installer.nsis` (NSIS) exist in-tree but no CI invokes them. |
| No Windows code signing | SmartScreen blocks the .exe on first run with "Windows protected your PC" until the binary builds telemetry-based reputation (~weeks). EV cert eliminates this immediately. |
| No macOS `.pkg` installer | Customers extract a `.tar.gz` and run a binary. No `pkgbuild` step in CI. |
| No macOS signing / notarization | Gatekeeper blocks the binary on first run. Workaround is `xattr -d com.apple.quarantine` or right-click → Open. Apple Developer ID + notarization eliminates the warning. |
| No Homebrew tap | macOS users with `brew` can't `brew install openzro`. The dashboard's MacOS tab references `brew install openzro/tap/openzro` but the `homebrew-tap` repo doesn't exist. |
| No pacman repo / AUR package | Arch / CachyOS / Manjaro / EndeavourOS users — meaningful share among self-hosters — fall through to install.sh's binary-tarball branch. The AUR branch in install.sh references `aur.archlinux.org/openzro.git` which is unpublished. |
| No Windows package managers (Scoop / Winget / Chocolatey) | Self-managing Windows users in dev environments fall back to the .zip download. |

The interim "caminho A" landed in this branch wires the dashboard's
SetupModal to GitHub Releases via a `useLatestRelease()` hook — a
real binary download with version-aware buttons, but still extract-
and-run UX. That unblocks customer demos this week. This ADR is the
medium-term plan to close the gap.

## Decision

Roll out client packaging in **four phases**, gated on customer
signal and budget for signing certificates. Each phase is
independently committable and gives the user a strictly better
install experience than the previous one.

The phasing is deliberate: phases 1–3 incur **only engineering
time** (no recurring costs); phase 2 brings recurring costs (~$99–
$300/year for Apple Developer ID; SignPath Foundation EV cert is
free for OSS, see below). Phase 4 is "polish on top".

### Phase 0 — interim, already shipped

- Dashboard fetches GitHub Releases JSON via `useLatestRelease()`,
  surfaces version-aware download buttons for Windows + macOS.
- `install.sh` now published at `pkg.openzro.io/install.sh` (the
  `publish-packages.sh` extension landed alongside this ADR).
- `pkgs.openzro.io` typo → `pkg.openzro.io` rename across 7 source
  files, internal repo paths aligned (`/yum/` → `/rpm/$basearch`,
  `/debian/` → `/apt/`).
- Android / iOS tabs hidden in the SetupModal (no apps shipped yet).

### Phase 1 — native installers, unsigned

**Windows `.msi`**
- New CI job `release_msi` in `release-binaries.yml`, runs on
  `windows-latest`. Installs WiX 4 via `dotnet tool install -g wix`,
  runs `wix build client/openzro.wxs` against the freshly-built
  `openzro_windows_amd64.zip` extract, uploads `openzro_<version>_windows_amd64.msi`
  to the GitHub Release.
- The .msi is unsigned. SmartScreen will warn ("Windows protected
  your PC") on first download for ~weeks until reputation builds.
  Acceptable for a launch beta; phase 2 fixes this with EV cert.
- **NSIS alternative**: `client/installer.nsis` exists too. WiX is
  the modern Microsoft path; NSIS is the historical alternative
  with simpler UI but no signing improvements. We pick one — WiX —
  and delete the unused `installer.nsis` to remove the choice.

**macOS `.pkg`**
- New CI job `release_pkg` in `release-binaries.yml`, runs on
  `macos-14`. Stages the universal binary (`openzro-ui` +
  `openzro` CLI) into a tree, runs:

  ```sh
  pkgbuild --identifier io.openzro.client \
           --version "${VERSION#v}" \
           --install-location /usr/local/bin \
           --root staging \
           openzro-${VERSION}.pkg
  ```

  Uploads the .pkg to GH Release.
- The .pkg is unsigned. Gatekeeper will refuse to run it from the
  Finder — users right-click → Open, or `xattr -d com.apple.quarantine`.
  Documentation on the dashboard's macOS tab makes this explicit.
- **No DMG**: a .pkg installer is enough; DMG is a packaging-of-a-
  packaging that adds visual polish but no install function.

**Pipeline wiring**
- `publish-packages.sh` extends to `cp` the `.msi` and `.pkg` from
  the GH Release into `$WORK/repo/windows/x64/openzro.msi` and
  `$WORK/repo/macos/openzro.pkg` (universal). Dashboard's existing
  links (`pkg.openzro.io/windows/x64`, `pkg.openzro.io/macos/*`)
  start working. The `useLatestRelease()` hook is preserved as a
  fallback / "advanced" download.

### Phase 2 — signing

**Windows EV signing via SignPath Foundation**
- [SignPath Foundation](https://signpath.org/foundation) provides
  a free Authenticode EV certificate to qualifying open-source
  projects. openZro qualifies (BSD-3-Clause, public CI, public
  repo). Signing happens server-side via their REST API — we
  upload the unsigned `.msi`, get back the signed `.msi`. No USB
  HSM in our CI.
- Application: a form submission + project review on signpath.org
  (1–2 weeks). Need to add a `SIGNPATH_API_TOKEN` secret to the
  `production` environment.
- CI step: between `release_msi` and `publish_packages`, add a
  `sign_msi` job that POSTs the unsigned MSI to SignPath's signing
  policy, polls until signed, downloads. Their GitHub Action
  ([SignPathHQ/github-action-submit-signing-request](https://github.com/SignPathHQ/github-action-submit-signing-request))
  encapsulates this.
- Result: zero SmartScreen warning from first download. Customers
  click "Run" without scary prose.

**macOS signing + notarization via Apple Developer ID**
- Apple Developer Program membership: $99/year (no OSS discount).
- Generate a "Developer ID Installer" certificate in Apple's
  developer console; export as a `.p12` to a CI secret.
- CI step: extends `release_pkg` to call `productsign --sign "Developer ID Installer: …"`
  and then `xcrun notarytool submit --wait` for Apple notarization.
- Notarization uploads the .pkg to Apple's servers, which scan
  for malware and "staple" a notarization ticket. Once stapled,
  Gatekeeper accepts the package on every Mac without phoning
  home. Apple's review is automated, completes in 2–15 minutes.
- Result: no Gatekeeper warning at all. Customers double-click,
  enter admin password, install is done.

**Recurring costs after phase 2**: $99/year (Apple). SignPath stays
free as long as we remain a verifiable OSS project.

### Phase 3 — package managers

**Homebrew tap** (`openzro/homebrew-tap`)
- Five-line `brews:` block in `.goreleaser.yaml` that publishes a
  formula to a separate `openzro/homebrew-tap` repo on every release.
  goreleaser handles the formula generation + the cross-repo push
  via a `HOMEBREW_TAP_TOKEN` secret.
- Users: `brew install openzro/tap/openzro` (CLI) and
  `brew install --cask openzro/tap/openzro-ui` (GUI). Both already
  documented in the dashboard's MacOS tab — they just need the
  tap repo to exist.
- The dashboard's existing copy works without change once the tap
  repo lands.

**Pacman repo + AUR**
- Add `archlinux` to the existing `nfpms` formats (3-line YAML
  change, produces `.pkg.tar.zst`).
- Extend `publish-packages.sh` to run `repo-add openzro.db.tar.gz *.pkg.tar.zst`
  for x86_64 and aarch64, ship to `pkg.openzro.io/pacman/{arch}/`.
- Update `install.sh`'s pacman branch from "git clone AUR + makepkg"
  (current) to "add the openzro pacman repo, `pacman -S openzro`"
  (mirrors the apt / yum branches).
- AUR PKGBUILD in parallel — separate `aur.archlinux.org/openzro.git`
  repo that points `source=` at the GitHub Releases tarball. Pure
  discoverability; users who run `paru -S openzro` get a build-from-
  tarball path. CI is harder (AUR is over SSH, separate auth) — do
  this manually for the first few releases, automate later if it
  becomes load-bearing.

**Windows package managers** (in priority order)
- **Scoop bucket** (`openzro/scoop-bucket`): goreleaser's `scoops:`
  block. Uses the EV-signed MSI from phase 2 directly, zero extra
  signing work. `scoop bucket add openzro https://github.com/openzro/scoop-bucket; scoop install openzro`.
- **Winget**: Microsoft's official package manager. Manifest needs to
  be PR-ed to `microsoft/winget-pkgs`. goreleaser's `winget:` config
  generates the manifest; the PR can be auto-opened via a CI step.
  Requires the MSI to be signed (phase 2 dep).
- **Chocolatey**: optional. Older Windows package manager with a
  user base, but Winget supersedes it for new installs. Skip unless
  customers ask.

### Phase 4 — auto-update + telemetry (optional)

`version/update.go` already polls GitHub's "latest release" API and
notifies the daemon when a newer version exists. Currently that
notification surfaces in dashboard and CLI but doesn't trigger an
install. Two follow-ups:

- **Background self-update** for the desktop UI: download the new
  signed installer (MSI / PKG), prompt the user, run the installer
  silently (`msiexec /i openzro.msi /quiet` / `installer -pkg openzro.pkg`).
  Requires phase 2 (signed installers) — auto-running an unsigned
  installer is a non-starter.
- **Anonymous telemetry** for install success / failure rates so we
  catch regressions before customers report them. Opt-in only,
  separate ADR if we go there.

This phase is not load-bearing for "ship to customers"; included
here for completeness.

## Trade-offs considered

### Alternative A — accept Phase 0 forever

Stay on the GH-Releases `.zip` / `.tar.gz` extract-and-run UX. Pros:
zero further investment, the install.sh wrapper covers Linux already.
Cons: meaningful customer base (Windows/Mac end-users) drops off at
the "extract zip, run as admin" step. Reflects on the brand:
"openZro is a half-finished alternative to Tailscale".

Rejected: we have committed customers on these platforms.

### Alternative B — adopt a paid B2B package signing service (DigiCert / Sectigo)

EV code-signing certificates from commercial CAs run $300–$700/year
and require shipping a USB HSM token to a designated key custodian.
Building CI around the HSM is brittle (USB pass-through to GH
runners isn't supported; cloud HSMs add cost).

Rejected: SignPath Foundation gives the same trust outcome (EV
cert with zero SmartScreen warning) for free for OSS, with a
cleaner CI integration via REST API.

### Alternative C — defer Phase 1, jump straight to signing

Wait until SignPath approves us (1–2 weeks) and Apple Developer ID
is provisioned, then do Phase 1 + Phase 2 together.

Rejected: SignPath approval and Apple Developer ID provisioning
are independent of building the artifacts. Building unsigned
installers in Phase 1 unblocks customer demos this week and
exposes pipeline bugs while we wait on signing infra. The unsigned
installers are temporary — Phase 2 ships them signed without
changing the artifact format.

### Alternative D — outsource macOS to Homebrew, skip the .pkg

Tell macOS users to install via `brew install openzro/tap/openzro`
instead of building a .pkg. Pros: no Apple Developer ID needed
(brew bypasses Gatekeeper), no notarization. Cons: assumes brew
is installed (most macOS devs have it, but not all customer
end-users); the .pkg path is the de-facto macOS install standard
that customer IT teams expect.

Partial accept: ship both. Homebrew is Phase 3 (free, easy);
Apple Dev ID + .pkg is Phase 2 for the customers who don't
brew. Some openZro users will get one path, some the other.

## Plan

Sequential, each independently shippable. Assumes one engineer-day
per CI job unless noted.

### Stage 1 — Phase 1 Windows MSI
1. Add `release_msi` job to `release-binaries.yml`.
2. Smoke-test `wix build client/openzro.wxs` locally on a Windows
   VM against a snapshot binary build. Fix any path / file
   references that drifted from upstream.
3. Update dashboard's WindowsTab to prefer the `.msi` asset over
   the `.zip` (regex: `/openzro_.*_windows_amd64\.msi$/`).
4. Tag a snapshot release, verify the MSI installs on Win 10/11.

### Stage 2 — Phase 1 macOS PKG
1. Add `release_pkg` job to `release-binaries.yml`.
2. Build the staging tree: `openzro-ui` + `openzro` CLI + a postinstall
   script that registers the daemon (`launchctl load`). Test locally.
3. Update dashboard's MacOSTab to prefer the `.pkg` asset over the
   `.tar.gz`.
4. Tag, verify install on macOS 14+ (Intel + Apple Silicon).

### Stage 3 — Phase 2 Windows EV signing
1. Submit application to SignPath Foundation (form on signpath.org).
   Wait 1–2 weeks for review.
2. Once approved: configure the SignPath project (signing policy,
   API token), add `SIGNPATH_API_TOKEN` secret to `production`
   environment.
3. Add `sign_msi` step using `SignPathHQ/github-action-submit-signing-request`.
4. Verify signed MSI installs on a fresh Win 11 VM with no
   SmartScreen warning.

### Stage 4 — Phase 2 macOS signing + notarization
1. Apple Developer Program enrollment ($99). Generate "Developer ID
   Installer" cert, export `.p12`, commit to `production` env as
   `APPLE_DEVELOPER_ID_P12` + `APPLE_DEVELOPER_ID_P12_PASSWORD`.
2. Generate an app-specific password for `notarytool` (
   appleid.apple.com → Sign-In and Security → App-Specific Passwords),
   commit as `APPLE_NOTARYTOOL_PASSWORD` + `APPLE_TEAM_ID`.
3. Add `productsign` + `notarytool submit --wait` + `stapler staple`
   steps to `release_pkg`.
4. Verify install on a fresh macOS VM with no Gatekeeper warning.

### Stage 5 — Phase 3 Homebrew tap
1. Create `openzro/homebrew-tap` repo.
2. Add `brews:` block to `.goreleaser.yaml`. Token wiring.
3. Tag, verify `brew install openzro/tap/openzro` works.

### Stage 6 — Phase 3 pacman repo + AUR
1. Add `archlinux` format to nfpms.
2. Extend `publish-packages.sh` to repo-add x86_64/aarch64.
3. Update `install.sh`'s pacman branch to use the repo (drop AUR
   build path).
4. Publish AUR PKGBUILD to `aur.archlinux.org/openzro.git` (manual
   for first few releases).

### Stage 7 — Phase 3 Windows package managers
1. Scoop bucket (`openzro/scoop-bucket`) + goreleaser `scoops:` config.
2. Winget manifest PR to microsoft/winget-pkgs (depends on Stage 3
   for signed MSI). Optional Chocolatey if customers ask.

### Stage 8 — Phase 4 polish
- Self-update logic in the desktop UI.
- Telemetry ADR if we go there.

## Consequences

### User experience

After Phase 1: customers double-click an installer (MSI / PKG)
instead of extracting a zip. Still see warnings on first run.

After Phase 2: customers double-click and install with no warning.
Equivalent to Tailscale / WireGuard install UX.

After Phase 3: macOS devs get `brew install openzro/tap/openzro`,
Arch / CachyOS users get `pacman -S openzro` (after one-time repo
add), Windows devs get `scoop install openzro`.

### Recurring costs

| Phase | Annual cost |
|---|---|
| 0–1 | $0 |
| 2 (after) | $99 (Apple Developer Program) |
| 3 (after) | $99 (no extra) |
| 4 | $99 (no extra) |

Microsoft Defender SmartScreen reputation is free; Apple Developer
ID is the only recurring vendor charge.

### Maintenance

Each new package format adds CI steps. Failures in any phase don't
block a release — the GitHub Release with raw archives is always
the canonical artifact, and `useLatestRelease()` falls back to it.

The most fragile pieces are:
- Apple notarization (occasional Apple service incidents block
  releases for hours; mitigated by `--wait` + retry)
- Winget PR review (microsoft/winget-pkgs requires human review
  per release; can lag)
- AUR (manual maintenance; community can also fork)

### Ownership

A single maintainer can own all phases. Phase 2 needs the org's
billing / Apple Developer enrollment, which is a one-time
administrative step.

### Backwards compatibility

Existing `.deb` / `.rpm` repo URLs unchanged. `install.sh` semantics
unchanged (just a different pacman branch in Stage 6, transparent
to users). GitHub Releases asset names unchanged. Customers on
Phase 0 continue to work; Phase 1+ adds new artifacts alongside.

## Open questions

- **SignPath Foundation eligibility**: confirm openZro qualifies
  before promising customers Phase 2 dates. Form submission first.
- **AUR maintainership**: who owns the AUR account? Organisational
  vs personal trust trade-off.
- **DMG vs PKG-only**: do customers expect the macOS app to come
  in a DMG with drag-to-Applications? PKG installer to /usr/local/bin
  is unusual for GUI apps but matches our CLI-friendly model.
  Revisit if customers ask.
- **Snap / Flatpak**: skipped here. Linux customers are largely
  apt/yum/zypper/pacman crowd; Snap and Flatpak add maintenance
  surface for a small audience. Revisit on demand.
