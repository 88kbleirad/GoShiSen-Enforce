# Artifact Signing & Provenance — Design & Migration (Prompt 12)

**Status:** verified, promoted to production. Prompt 13 bootstrap is in review
(PR #24) — release tag signing is enabled and fail-closed; `v0.2.1` and its
assets are untouched. Final activation only requires the maintainer to store the
production private key as `GH_RELEASE_SIGNING_KEY` (public key already committed
as trust anchor).
**Applies to:** release artifacts — `goshisen-linux-amd64.tar.gz`,
`goshisen-linux-arm64.tar.gz`, `checksums.txt`, `sbom.spdx.json`, and the OpenWrt
`.apk` (same key, offline `openssl` verification until native `usign` is added).
**Does NOT change:** tag `v0.2.1`, its assets, or any runtime binary/policy engine.

---

## 1. Trust model

- **Algorithm:** Ed25519 (OpenSSL `pkeyutl -sign/-verify`, raw sign). Available in
  every ubuntu-24.04 runner and on user machines (`openssl`).
- **Key layout:**
  - `keys/goshisen-release.pub.pem` — the release public key, committed to the
    repository as the trust anchor (verification material is public by design).
  - Private key: **never committed**. On the release workflow it exists only as
    the GitHub Actions secret `GH_RELEASE_SIGNING_KEY`; the tag job writes it to
    `$RUNNER_TEMP` (ephemeral), verifies it matches the committed public key
    (`scripts/check-key-match.sh`), signs, then removes the file via trap.
    PRs/forks never run the release workflow (tag-only trigger) and can never
    reach the secret.
- **Trust anchor:** `keys/goshisen-release.pub.pem` is published (a) in this
  repository, (b) in release notes/install docs, and (c) out-of-band by the
  maintainers. First-install trust is anchored independently of the artifact
  channel.
- **Public key fingerprint (SHA-256 of DER SPKI):**
  `24215e016621925215dec0ad6ea14ac74253a4e97249a973e89d649a2217cca0`
- **Key identifier (first 16 hex of raw pubkey):** `f955ede984245d64`
- **Created:** 2026-11-13 (UTC), maintainer-generated on a trusted machine.
- **Rotation:** a new key replaces `keys/goshisen-release.pub.pem` (older
  fingerprints remain in docs for verifying older releases). Procedure: maintainer
  generates a new key, stores the new secret, opens a PR committing the new public
  key + updated fingerprints, verifies the new key against a tag release in a
  staging environment, then flips production tags to the new key. Rollback = keep
  verifying with the previous key (it stays published in docs and git history).
- **Revocation:** an Ed25519 key cannot be revoked on-chain; compromise is handled
  by rotation to a fresh key and immediate re-release of affected artifacts.
  Because the trust anchor is the committed public key, a compromised release key
  is replaced and the old fingerprint removed from docs — users re-anchor on the
  new key. No `--allow-untrusted`-style fallback is ever used to paper over a
  revoked key.
- **Rollback (of this feature):** revert the release.yml signing steps via PR and
  re-publish unsigned assets. The public key record is retained in git/docs for
  audit; the secret is left in place (never force-deleted) and simply unused
  until re-enabled.

## 2. Per-artifact signature & provenance

For every release artifact `A` we produce:

| File | Content |
| --- | --- |
| `A.sig` | Ed25519 signature (64 bytes) over `A` |
| `A.sig.pem` | copy of the public key used (for offline verification) |
| `provenance.json` | source commit, tag, builder (workflow), toolchain (Go version), per-artifact sha256, build timestamp, target architecture |
| `provenance.json.sig` | signature over the provenance file |

`checksums.txt` is itself signed; the SBOM is signed like any artifact.

## 3. Provenance record (minimal)

```json
{
  "version": 1,
  "artifact": "goshisen-linux-amd64.tar.gz",
  "source_commit": "<full sha>",
  "tag": "v0.2.2",
  "builder": "release.yml / package job / @ 8e007a5d",
  "toolchain": "go1.25.x / CGO_ENABLED=0 / linux/amd64",
  "target_arch": "amd64",
  "build_timestamp": "2026-11-13T00:00:00Z",
  "sha256": "<artifact hash>"
}
```

## 4. Verification gate (fail closed) — `scripts/verify-signatures.sh`

Run before any upload (release workflow step) and locally. Exits non-zero unless:

1. every artifact in the manifest has a signature file (`A.sig`), and
2. every signature verifies with the supplied public key (the committed
   `keys/goshisen-release.pub.pem` in production), and
3. the provenance file exists, is signed by the same key, and its `sha256`
   matches the actual artifact bytes, and
4. the source commit recorded in provenance matches the commit being built
   (stale-provenance rejection), and
5. for archives: the embedded binary architecture matches the filename
   (`amd64`/`arm64`) — architecture mismatch rejection.

Errors are distinct: `missing-signature`, `bad-signature`,
`provenance-mismatch`, `stale-provenance`, `arch-mismatch`, `missing-key`.

## 5. CI/release integration (production key only on tags)

- **`verify-signatures` job (ci.yml):** runs on every PR with a **test key
  generated in-flow** and stored in `$RUNNER_TEMP`. It builds the archives, signs
  them, then (a) runs the verifier against the correct key → must pass, and
  (b) tampers with a copy of `dist/` and asserts the verifier exits non-zero with
  a `bad-signature` error → must fail. This keeps the gate honest without touching
  secrets.
- **Go suite (`tests/package/signing_artifacts_test.go` + `production_signing_test.go`):**
  covers all verifier states end-to-end with throwaway keys, plus the production
  bootstrap gates: committed public-key validity + fingerprint, foreign-key
  rejection, missing-secret fail-closed, key-mismatch refusal, tag-only/secret
  isolation of release.yml, and single-artifact (installer) rejection of
  unsigned/tampered artifacts.
- **release.yml:** a `Sign release artifacts with production key` step (tag-only
  job) imports `GH_RELEASE_SIGNING_KEY` into `$RUNNER_TEMP`, runs
  `scripts/check-key-match.sh` (private key must match the committed public key),
  signs via `scripts/sign-artifacts.sh`, then verifies with
  `scripts/verify-signatures.sh --require-sign`. Any failure — missing secret,
  mismatched key, bad signature, stale provenance — fails the release before
  upload. The private key file is removed via trap. PRs/forks cannot trigger this
  workflow and never see the secret.

## 6. OpenWrt `.apk`

- The APK is a zip; we sign the raw `.apk` blob with the same Ed25519 key
  (`<apk>.sig`), which verifies offline (`openssl pkeyutl -verify`).
- Native `apk` verification requires `usign` keys under `/etc/apk/keys/`
  (`<keyname>-<fingerprint>.pub`). That is a follow-up (backlog #1), and it
  will **remove `--allow-untrusted` from the production install path**.
- Today the production apk install path (docs/openwrt.md) is: verify the
  `.apk` signature with `openssl` against the committed public key first
  (REQUIRED), then `apk add --allow-untrusted` — an explicit transition
  exception with no silent fallback: if signature verification fails the install
  is refused. `--allow-untrusted` also appears only in (a) `scripts/openwrt-image-prep.sh`
  installing *kmods* whose checksums were already verified with
  `sha256sum -c --strict` before install (lab path, not release), and
  (b) historical docs. No production release path ships an unverified install as
  a default.

## 7. Migration plan

1. ✅ PR #24 (tooling + provenance writer + fixture + CI `verify-signatures` job
   + unit tests for all failure modes + design note) — merged with no production
   secret.
2. ✅ PR #24 (this bootstrap): committed production public key
   `keys/goshisen-release.pub.pem` (fingerprint
   `24215e016621925215dec0ad6ea14ac74253a4e97249a973e89d649a2217cca0`), tag-only
   fail-closed signing in release.yml (`scripts/release-sign.sh`), installer docs
   updated to verify with the committed public key, production signing tests.
   Final activation: maintainer stores private key as `GH_RELEASE_SIGNING_KEY`
   (fingerprint verified offline before storing); then the next tag release is
   signed and verified end-to-end.
3. Optional later: native OpenWrt `usign` key for `apk` (backlog #1) — removes
   the `--allow-untrusted` transition exception.

## 8. Secrets safety

- Test keys are generated per-job and discarded; their public halves are fixtures.
- No private key material is committed, logged, or embedded in artifacts.
- The production signing secret only ever exists as the GitHub Actions secret
  `GH_RELEASE_SIGNING_KEY`; in the workflow it is written to `$RUNNER_TEMP`
  and removed by trap. It is never echoed, printed, or uploaded.
