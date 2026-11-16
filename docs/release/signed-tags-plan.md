# Signed release tags — plan for v0.2.5

**Status: PROPOSAL (docs only).** Date: 2026-11-16.
Context: issue #44 (immutable releases + post-release checklist). This
document records why tags are currently unsigned, the signing options,
what each requires from the maintainer, how verification would be
documented for users, how it would be added to the release gate, and the
migration for v0.2.5. It does not generate, request, or handle any key;
the maintainer decides.

## Why tags are currently unsigned

All release tags (`v0.1.0` … `v0.2.4`) are **annotated** but **unsigned**:
`git tag -v v0.2.4` → `error: no signature found` (verified 2026-11-16 on
`v0.2.4`). The release *assets* are independently integrity-protected:

- CLI tarballs/checksums/SBOM: Ed25519 release key (`public_key.pem`
  asset), verified in the release workflow and post-publish.
- OpenWrt apk: EC P-256 anchor (`keys/goshisen-apk.pub.pem`, fingerprint
  `542bb7ae…`), verified by apk-tools.

The tag itself is the point of trust for *what source* a release was
built from (the tag commit is pinned into `PKG_SOURCE_VERSION` and is the
`source_commit` in provenance). An unsigned annotated tag can be
re-created at a different commit with the same name; users who fetch the
tag have no way to distinguish.

## Options

| Option | What it is | What the maintainer needs | Verification for users |
| --- | --- | --- | --- |
| **GPG** (OpenPGP) | `git tag -s` with a GPG key; `git tag -v` verifies | GPG keypair; publish public key (or `gpg --export` as a repo asset); configure `user.signingkey` | `gpg --import public-gpg.asc`; `git tag -v v0.2.5`; key fingerprint pinned in docs |
| **SSH signing** (`allowed_signers`) | `git tag -s` with an SSH key; git verifies against a repo-committed `allowed_signers` file | Existing SSH key; commit `docs/trust/allowed_signers` (one line: `email namespace="git" <pubkey>`); set `gpg.format=ssh` + `user.signingkey=ssh-ed25519 …` | `git tag -v v0.2.5` uses the committed `allowed_signers`; no external key server |
| **sigstore/gitsign** | Keyless signing via OIDC (GitHub identity); `git tag -s` via gitsign; verification uses the sigstore bundle | No long-lived key; gitsign installed in the release environment; GitHub identity for OIDC | `gitsign verify` / `git tag -v` after `gitsign` setup; relies on sigstore transparency log |

Recommended default for this repo: **SSH signing with a committed
`allowed_signers` file** — reuses an existing key type, verification is
hermetic (no external key server/transparency log dependency), and the
trust anchor lives in the repository itself alongside the release docs.

## What each option requires from the maintainer

- Any option: decide the signing key, add the public material to the
  repo (`docs/trust/`), and sign the first release tag. The signing
  must happen on the maintainer's machine (or a dedicated release
  environment); the agent never handles keys or signs tags.
- GPG: Gink the fingerprint; consider expiry/rotation cadence; decide
  whether the key is also used for anything else.
- SSH: choose which SSH key (recommend a dedicated key, not a general
  host key); the `allowed_signers` file must list the exact allowed
  key(s); remember `git` needs `gpg.format=ssh` and
  `user.signingkey` to sign, and `gpg.ssh.allowedSignersFile` to verify.
- sigstore: needs gitsign binary + GitHub OIDC login in the release
  environment; verification depends on sigstore's availability.

## How verification is documented for users

- `docs/installation.md` + `docs/release/v0.2.5-notes.md`: "Tag
  verification" section — one command (`git tag -v v0.2.5`), the trust
  anchor location (`docs/trust/allowed_signers` or GPG key asset), and
  the fingerprint to compare.
- Release checklist: add a "tag signature verified" step (below).
- README "Releases" section: state that tags are signed as of
  v0.2.5 and that provenance combines tag signature + asset signatures.

## How the release gate enforces it

`scripts/release-pin-gate.sh` (v0.2.4) proves `PKG_SOURCE_VERSION` is an
ancestor of the tag and the runtime diff is empty; it cannot prove the
tag signature. Add to the gate (as a second, explicit check that runs
on the maintainer side where the key lives):

1. `git tag -v v0.2.5` must succeed (exit 0) against the committed
   trust anchor — hard failure otherwise.
2. If the tag was created with SSH signing: `git tag -v` fails unless
   `gpg.ssh.allowedSignersFile` is configured; document the config one
   line in the gate output for the maintainer, or verify with
   `git verify-tag --raw` and record the raw result.

The CI release workflow cannot reach the maintainer's private key; the
gate runs locally in the release run, and its output (verification
stdout + fingerprint) is recorded in the release evidence doc, same as
the apk signing approval record.

## Migration for v0.2.5

1. Maintainer adds the trust anchor file (`docs/trust/allowed_signers`
   or GPG key asset) in a docs PR before the release run.
2. Maintainer signs the v0.2.5 tag with the chosen key.
3. Release run records `git tag -v v0.2.5` output in the release notes /
   evidence; the pin gate includes the signature check.
4. Post-publish verification adds `git tag -v v0.2.5` on a fresh clone.
5. Older tags (`v0.1.0`…`v0.2.4`) remain unsigned; document that
   signature coverage starts at v0.2.5 (no retroactive signing without
   maintainer decision — re-signing moves tags and changes what build
   provenance points to).

## Boundaries

- This plan does not create keys, sign tags, or change repository
  settings. Publishing, issue closing, and repository/environment
  settings changes need explicit per-run maintainer authorization.
