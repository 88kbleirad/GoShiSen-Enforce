# Release Signing Proposal (v0.2.1+ / Issue #12)

Status: proposal. No keys or secrets are stored in this repository. Nothing
here is implemented yet; the release workflow continues to attach unsigned
assets until the maintainers approve an option and a key policy.

## Goals

- Let users verify the `.apk` (package) and the release tarballs come
  from the project (integrity + origin) without `--allow-untrusted`.
- Keep the change minimal: no new long-lived secrets in CI, no change to
  the build model, and no obligation for users to run extra tooling
  unless they want the stronger guarantee.
- Preserve the existing properties: assets are staged as draft releases,
  never auto-published; PRs never produce signed artifacts.

## Options considered

### A. Cosign keyless (Sigstore) — recommended for tarballs + SBOM

- Sign `goshisen-linux-{amd64,arm64}.tar.gz`, `checksums.txt`, and
  `sbom.spdx.json` with `cosign sign-blob` using the OIDC identity of
  the GitHub Actions workflow run (token from `actions/id-token`).
- Users verify with `cosign verify-blob --certificate-identity
  "https://github.com/88kbleirad/GoShiSen-Enforce/.github/workflows/release.yml@refs/tags/v0.2.1"
  --certificate-oidc-issuer https://token.actions.githubusercontent.com`.
- No keys to manage; trust = "the release was produced by this repo's
  release workflow on this tag". The GitHub release page shows each
  signature and certificate.
- Limitation: verification requires `cosign` (a single static binary) on
  the user side. Transparent entry in Rekor gives an audit trail and
  offline verification via the signed bundle.

### B. minisign — recommended for the `.apk`

- Generate one `minisign` key pair **outside** the repository. The
  secret key must be held in the maintainer's vault (or GitHub encrypted
  secret used only by the `release` workflow on `v*` tags).
- The public key (`minisign.pub`) is committed to the repository (e.g.
  `openwrt/files/etc/apk/keys/goshisen.pub` for the packaged key, and in
  the release notes for tarballs).
- The release workflow signs each artifact (`.apk`, tarballs,
  `checksums.txt`) and uploads the `.minisig` files next to them.
- Users verify: `minisign -Vm <file> -P <pubkey>` — no account, no
  network, works offline forever.
- Why minisign over a full OpenWrt key? The `.apk` format's native
  signing uses `usign` (ed25519) keys installed under `/etc/apk/keys/`.
  Generating an `usign` key would allow `apk add` to verify natively
  without `--allow-untrusted`; however `usign` key generation + storage
  is more involved and the key must have a file-name convention
  (`<keyname>-<fingerprint>.pub`). Since the package is also distributed
  outside OpenWrt, minisign keeps one signing tool for all assets;
  apk-native `usign` can be layered on later without breaking this.

### Trust model (both options)

- Signing identities are bound to the tag that produced them: a binary
  signed by the release workflow for `v0.2.1` cannot be replayed as
  `v0.2.2`.
- Compromise of the GitHub Actions token does not forge a signature: the
  OIDC identity pins the workflow path + ref (Cosign), and the
  minisign secret key is not present in CI (only used in a protected
  action or vault).
- The project publishes the public key out of band (release notes, the
  package's `/etc/apk/keys/`) so first-install trust is anchored
  independently of the artifact channel.
- We do **not** recommend `--allow-untrusted` as a default; the sha256
  check remains a secondary control, not a substitute for a signature.

## What the user verifies

- Tarballs: `cosign verify-blob` (presence of the signed bundle in the
  release) or `minisign -Vm` (the `.minisig` file).
- `.apk`: `apk add` without `--allow-untrusted` (once the release public
  key is in `/etc/apk/keys/`), or `minisign -Vm` on the `.apk`.

## Proposed minimal implementation (when approved)

1. Add a `sign` job to `.github/workflows/release.yml` that runs only on
   `v*` tags, has `permissions: id-token: write, contents: write`, and:
   - installs `cosign` (pinned version, verified checksum) and
     `minisign`;
   - signs `dist/*` with `cosign sign-blob` and writes
     `*.sig`/`*.cert`/`*.bundle`;
   - signs `dist/*` with `minisign` using the secret key from
     `GH_SECRET_MINISIGN_KEY` (a GitHub Actions secret, never
     committed);
   - uploads all signature artifacts to the draft release.
2. Add a `verify-signatures` CI step for PRs that checks the *shape* of
   the signing job (presence, expected files) without doing real
   signing (no secrets on PRs).
3. Document the commands in `docs/installation.md` and
   `docs/openwrt.md` (replacing the `--allow-untrusted` examples once
   keys exist).

## Explicitly out of scope

- Key management UI or key rotation automation.
- Signing of the SBOM digest separately from the tarballs (the SBOM is
  already inside the tarball and listed in `checksums.txt`).
- A web-of-trust / reviews model for keys.
