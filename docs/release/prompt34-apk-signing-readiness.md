# Prompt 34 — Production APK signing readiness audit

**Status:** complete — **verdict: BLOCKED** (missing production APK
signing key + package-feed trust-store access; no APK signing step is
wired in the release workflow). **Date:** 2026-11-14. **Base:** main
`94dddac` (after Prompt 32; CI 43605482804 6/6 SUCCESS).

## 1. Roles in the trust path (explicitly distinguished)

| Role | Mechanism (current) | Status |
| --- | --- | --- |
| Release-artifact signer | Ed25519 (OpenSSL raw-sign), production key `GH_RELEASE_SIGNING_KEY`, release.yml tag-only job | **Implemented** (v0.2.2 signed) |
| Public trust anchor | `keys/goshisen-release.pub.pem` (committed) | Present; fingerprint `24215e016621925215dec0ad6ea14ac74253a4e97249a973e89d649a2217cca0` verified at 94dddac (2026-11-13 key) |
| Verify tooling | `scripts/verify-signatures.sh` (sig + provenance + arch, fail-closed exit codes 2–8), `verify-single-artifact.sh`, `check-key-match.sh` | Implemented + tested |
| **APK package signer** | **none** — release.yml never touches `.apk`; no `usign`/`apk index --sign` step | **Missing** |
| APK package-manager verification | `apk` trust (`/etc/apk/keys/`, `usign`) | Not provisioned (no key in any trust store) |
| Transition path | manual `openssl ... -verify` + `apk add --allow-untrusted` | Documented transition exception only — **not** production trust |

## 2. Workflow wiring facts (at 94dddac)

- `release.yml`: tag-only (`v*`), package+verify+publish job, DRAFT-only
  (never auto-publishes). Signs dist artifacts (`dist/goshisen-linux-*`,
  checksums, SBOM) with `scripts/release-sign.sh`:
  1. `RELEASE_SIGN_KEY` missing → exit 1 before signing (fail-closed);
  2. `check-key-match.sh` enforces secret↔public-anchor equivalence;
  3. `sign-artifacts.sh --sign` writes `.sig` + provenance + `.sig.pem`;
  4. `verify-signatures.sh --require-sign` must pass.
- **No APK appears in the release workflow** — the published artifact set
  is CLI tarballs only. The `.apk` builds are produced by the SDK (lab
  / OpenWrt feed), not signed, not uploaded, not registered in a feed
  index.
- `docs/openwrt.md` §Signing **incorrectly claims** "Release `.apk`
  artifacts are signed with the release Ed25519 key … signatures attach
  as `<artifact>.sig`". That is aspirational; no such signature files are
  produced. Corrected in this prompt (see §5).

## 3. Negative controls (all PASS, fail-closed)

Run on `scripts/verify-signatures.sh` (Ed25519) with an **ephemeral
test key** (never production; no secrets read/exported) against the r5
APK artifact:

| Case | Expect | Observed |
| --- | --- | --- |
| A. unsigned artifact (--require-sign) | exit 3 missing-signature | exit 3 |
| B. tampered artifact | exit 4 bad-signature | exit 4 |
| C. valid sig + provenance + arch | PASS | VERIFY-PASS |
| D. wrong key | exit 4 bad-signature | exit 4 |
| E. provenance hash mismatch (re-signed) | exit 5 provenance-mismatch | exit 5 |
| F. missing provenance | exit 8 missing-provenance | exit 8 |

Unit suite (16 tests) at `tests/package/{signing_artifacts,production_signing}_test.go` (missing key, wrong key, tampered, missing sig, stale provenance, provenance mismatch, arch mismatch, foreign signature, workflow tag-only/secret) — all PASS.

Note: these controls validate the **tooling** that an APK signing step
would reuse. They do NOT sign or trust any APK, and are not production
evidence.

## 4. Prerequisites to move BLOCKED → READY

1. A production APK signing key (and its public trust anchor) authorized
   by the repo owner — the same approval gate as the existing release
   key (do not reuse `GH_RELEASE_SIGNING_KEY` unless explicitly approved
   for package signing; prefer a dedicated APK/usign key if the feed
   policy requires it).
2. Key management: store the private key as a GitHub secret (e.g.
   `GH_RELEASE_APK_SIGNING_KEY`); never commit/export it. Distribute the
   public key to the package feed trust store (`/etc/apk/keys/` on
   consuming devices / feed repo).
3. Wire a signing step in the package build/release path: either native
   `usign` (`apk index --sign` + key under `/etc/apk/keys/`) or
   per-file Ed25519 `.sig` + provenance (reusing
   `verify-signatures.sh` semantics) with a **fail-closed gate**: missing
   key / mismatch / invalid signature aborts publish.
4. Release-checklist gate item: forbid `--allow-untrusted` on the
   production path (transition exception only for the lab).
5. Post-sign verification: install in a clean VM WITHOUT
   `--allow-untrusted` using only the packaged trust anchor; assert the
   package manager accepts it.

Exact commands to run once a production key is granted (not executed
here):

```sh
# build + sign APK in the SDK/feed context:
# (native usign variant)
usign -S -s <apk-key.sec> -c goshisen-0.2.0-r5.apk -o goshisen-0.2.0-r5.apk.sig
# per-file Ed25519 variant (existing tooling):
openssl pkeyutl -sign -rawin -inkey <key.pem> -in goshisen-0.2.0-r5.apk \
  -out goshisen-0.2.0-r5.apk.sig
# verify with the committed/authorized public anchor:
openssl pkeyutl -verify -rawin -pubin -inkey keys/goshisen-release.pub.pem \
  -in goshisen-0.2.0-r5.apk -sigfile goshisen-0.2.0-r5.apk.sig
# package-manager install (production path, NO --allow-untrusted):
install -d /etc/apk/keys && install -m 0644 <pub-key> /etc/apk/keys/
apk add /path/to/goshisen-0.2.0-r5.apk
```

## 5. Documentation corrected

- `docs/openwrt.md` §Signing: reworded to state that APK signing is a
  **planned/backlog** item; the current install relies on the manual
  Ed25519 check + `--allow-untrusted` transition exception; no APK sig
  artifacts are produced by the release workflow today.
- `docs/release/prompt34-apk-signing-readiness.md` (this report) +
  evidence-index row 33.

## 6. Rollback / runbook

- If a future APK signing step misbehaves: revert the signing-workflow
  change (squash revert), keep the public anchor record, never delete or
  rotate the production private key, and fall back to the documented
  transition path for lab use.
- A failed post-sign install gate must block the feed publish (no
  partial/unsigned publication).

## 7. Scope exclusions honored

- No key created/rotated/exported; no secret read (only presence/absence
  behavior and match checks via scripts). No tag/release/APK upload/feed
  publish. No policy/payload change. No production router install.
