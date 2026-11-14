# Prompt 35 — APK signing path: test implementation with fail-closed verification

**Status:** complete — test signing path **implemented + verified**;
**production APK signing remains BLOCKED** (no approved key/secret).
**Date:** 2026-11-14. **Base:** main `44248a9`.

## 1. What was added

| File | Purpose |
| --- | --- |
| `scripts/apk-sign.sh` | Sign v3 `.apk` files with `apk adbsign --sign-key` (PEM secret, injected via `--key`/`--key-file`/`--key-env`; never hardcoded/logged). Fail-closed: missing key → exit 1; missing apk → exit 2; sign failure → exit 3. |
| `scripts/apk-verify.sh` | Verify `.apk` against a trust dir (`--keys-dir` or `--key`) with `apk verify`; optional `--sha256 file=hash` full-file integrity gate (closes `apk verify`'s blind spot to trailing tampering). Exit 4 on any UNTRUSTED or hash mismatch. |
| `tests/package/apk_signing_test.go` | Go tests: missing-key/missing-file/missing-anchor gates (no apk required) + sign→verify roundtrip with a real apk when `TEST_APK`/`APK_TOOL` are provided (skip otherwise). |

## 2. Verification evidence (ephemeral test key, real r5 APK)

Environment: OpenWrt SDK host `apk` (apk-tools 3.0.5), ephemeral EC
P-256 PEM key (fingerprint of pub key DER SHA-256 `9736b23b…`), real
x86_64 + aarch64 r5 apks.

| Case | Result |
| --- | --- |
| `apk-sign.sh --apk <real apk> --key <ephemeral>` | OK (signature embedded) |
| `apk-verify.sh --keys-dir <trust> <signed apk>` | OK (trusted signature) |
| `apk-verify.sh --keys-dir <trust> --sha256 <signed>=<recorded>` | OK |
| unsigned apk (verify) | exit 4 UNTRUSTED |
| tampered apk (verify, no sha256) | apk verify OK → **sha256 gate exit 4** with recorded vs actual hash |
| missing signer key | exit 1 |
| missing apk file | exit 2 |
| missing trust anchor dir | exit 1 |

Clean-VM package-manager install (x86_64 OpenWrt guest, test pubkey in
`/etc/apk/keys/<fp>.pub`, **no `--allow-untrusted`**):

| Case | Result |
| --- | --- |
| `apk add <signed apk>` | **Installed** (`OK: … in 186 packages`, `goshisen-0.2.0-r5 x86_64 [installed]`, default-disabled postinst ran) |
| upgrade path (reinstall signed) | OK (idempotent) |
| uninstall (`apk del goshisen`) | OK (Purging goshisen; binary removed) |
| unsigned apk add (no flag) | `ERROR: … UNTRUSTED signature` (rejected) |
| tampered apk add (no flag) | `error: uninstallable` (rejected) |

Go test results: `TestApkSignVerifyRoundtrip` PASS on instabox with
`APK_TOOL` + `TEST_APK`; 3 gate tests PASS in all environments (skip
roundtrip without apk). `make check` PASS.

## 3. Production gap (unchanged, BLOCKED)

- No approved production APK signing key/secret; no public trust anchor in
  a feed trust store; no production wiring (release.yml unchanged — it
  signs CLI artifacts only).
- The implemented scripts are the **pluggable test path**: once an
  approved key is provisioned, production wiring = run
  `apk-sign.sh --key <approved>` on release apks + ship the pubkey to
  `/etc/apk/keys/` + enforce `apk-verify.sh --sha256` in the feed
  publish gate (never `--allow-untrusted`).
- Verification with the packaged anchor in a clean VM (no
  `--allow-untrusted`) is the mandatory production gate.

## 4. Scope exclusions honored

- No production key created/rotated/exported; no `GH_RELEASE_APK_SIGNING_KEY`
  read; no production trust anchor change; no tag/release/APK upload/feed
  publish; no production router; no policy/payload change.
