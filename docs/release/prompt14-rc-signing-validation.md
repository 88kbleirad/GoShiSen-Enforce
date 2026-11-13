# Release-Candidate Signing Validation — v0.2.1-rc.2 (Prompt 14)

**Date:** 2026-11-13 (UTC)
**Status:** PASS — candidate validated end-to-end; candidate tag + draft release
removed after validation (cleanup done). No production release modified.

---

## 1. Preconditions

- PR #24 (production bootstrap) explicitly approved by maintainer before the run.
- Production trust anchor committed: `keys/goshisen-release.pub.pem`.
- Fingerprint (SHA-256 of DER SPKI): `24215e016621925215dec0ad6ea14ac74253a4e97249a973e89d649a2217cca0`
- Key ID: `f955ede984245d64`; created 2026-11-13.
- Secret `GH_RELEASE_SIGNING_KEY` stored (mac only; content never printed).
- Candidate tag on a **reviewed commit**: `v0.2.1-rc.2` → `bf433c437189efbcfff262bd7333e0a5f46656e0`
  (annotated tag object `f4455e2798519ad1165d4403bdba2c346220f152`).

## 2. Workflow event filter audit

- `release.yml` triggers **only** on `push: tags: ["v*"]` — no
  `pull_request` trigger. Verified by `rg` and `gh workflow view release.yml`.
- `secrets.GH_RELEASE_SIGNING_KEY` referenced only inside the tag-only
  `package` job (`Sign release artifacts with production key` step); forks cannot
  invoke it and never receive the secret.

## 3. Candidate workflow runs

| Candidate | Run ID | Head commit | Result | Notes |
| --- | --- | --- | --- | --- |
| v0.2.1-rc.1 | 43450615117 | `ddb91c4` | success (41s) | **Gap found:** per-artifact `*.provenance.json(.sig)` NOT uploaded → verifier over downloaded assets fails with `missing-provenance: checksums.txt` (exit 8). Fix committed (`bf433c4`) + regression test added; rc.1 tag/release deleted, re-run as rc.2 |
| v0.2.1-rc.2 | 43451010054 | `bf433c4` | success (59s) | All steps ✓ (build → verify-package → **sign** → **verify** → upload); log scan: no key material, `RELEASE_SIGN_KEY: ***` masked, `check-key-match` pass |

Run log evidence: `gh run view 43451010054 --log` — step output shows only
public status lines (`OK: private key matches public key .../keys/goshisen-release.pub.pem`),
no private key bytes.

## 4. Downloaded asset verification (v0.2.1-rc.2)

Assets (19): 4 artifacts + `*.sig` + `*.provenance.json` + `*.provenance.json.sig`
+ `provenance-index.txt(.sig)` + `public_key.pem`.

| Check | Result |
| --- | --- |
| `sha256sum -c checksums.txt` | both archives OK |
| `public_key.pem` fingerprint | `24215e...cca0` — matches committed anchor (byte-identical `diff`) |
| Signature verify (openssl rawin, committed anchor) | all 5 `*.sig` → `Signature Verified Successfully` |
| `verify-signatures.sh --require-sign --commit bf433c4` | PASS 4/4 (sig + provenance + arch) |
| Provenance `sha256` vs actual | match (per artifact) |
| Embedded binary arch | amd64 `x86-64` ✓; arm64 `aarch64` ✓ (both static, stripped ELF) |
| Provenance `source_commit` | `bf433c437189efbcfff262bd7333e0a5f46656e0` ✓ |
| `tag` in provenance | `v0.2.1-rc.2` ✓ |

## 5. Fail-closed adversarial proofs (committed anchor)

| Case | Exit | Verified |
| --- | --- | --- |
| Tampered archive (one byte appended) | 4 | reject ✓ |
| Wrong key (different throwaway) | 4 | reject ✓ |
| Missing signature (removed `*.sig`) | 3 | reject ✓ |
| Installer single-artifact gate: valid | 0 | accept ✓ |
| Installer gate: unsigned (sig missing) | 1 | reject ✓ |
| Installer gate: tampered | 4 | reject ✓ |

## 6. Installer verification

- amd64 archive: extracted, runs (`goshisen v0.2.1-rc.2`), static ELF x86-64.
- arm64 archive: cross-arch — cannot execute on amd64 host; verified via ELF
  metadata (`ARM aarch64`, statically linked) + signature/provenance/arch check
  in the verifier. (arm64 runtime validation remains CI-gated per docs.)
- Signature check is **mandatory before install**; `--allow-untrusted` remains
  only as the time-boxed transition exception for the native `usign` path
  (backlog #1) — never a silent fallback (see `docs/openwrt.md`).

## 7. Cleanup

- rc.1 and rc.2 draft releases deleted via `gh release delete ... --yes --cleanup-tag`
  (drafts; never published).
- Candidate tags removed from origin (`git ls-remote --tags` clean).
- `v0.2.1` (tag `6eb19f3`, release published 2026-11-13T12:43:29Z, assets
  checksums/amd64/arm64/sbom) — **untouched**; `v0.2.0`, `v0.1.0` untouched.
- No force-push; no production tag/release modified.

## 8. Evidence / audit trail

- PR #24 commits involved: `51b62ca`, `c33c427`, `ddb91c4`, `bf433c4`,
  `c0fa57f`.
- CI: PR runs + candidate run `43451010054` all 5/5 jobs green (final head
  `c0fa57f` build/vet/test, package+verify, integration, openwrt e2e,
  verify-signatures).
- `make check` (fmt/vet/test/build/verify-static) passes; `go test ./tests/...`
  passes (16 signing tests incl. 7 production, now hermetic).

## 9. Findings fixed during validation

1. **Missing provenance upload (rc.1):** release upload lacked per-artifact
   `*.provenance.json(.sig)`, so downloaded assets could not be fully verified
   (`missing-provenance`, exit 8). Fixed in `bf433c4`: upload includes all 8
   provenance files per artifact pair; regression test asserts presence.
2. **Non-hermetic tests:** `TestReleaseSignFailsWithoutSecret` /
   `TestReleaseSignFailsOnKeyMismatch` inherited `RELEASE_SIGN_KEY` from the
   shell → false results. Fixed in `c0fa57f` via `cleanSignEnv()`.
