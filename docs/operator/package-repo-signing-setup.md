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

On a trusted local machine (not CI), produce a fresh dedicated key:

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

Or interactively:

```sh
gpg --full-generate-key
# Choose: RSA and RSA, 4096 bits, no expiration,
# Name: openZro Package Signing
# Email: dev@openzro.io
# Set a strong passphrase you'll save in step 3.
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
| `PKG_GPG_PASSPHRASE` | the passphrase you set when generating |

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
