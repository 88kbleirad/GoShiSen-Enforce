# Prompt 36 — APK signing integrated into the production release gate

**Status:** complete — pipeline test **end-to-end PASS**; **production
APK signing remains BLOCKED** until an approved key/secret and trust
anchor are provisioned. **Date:** 2026-11-14. **Base:** main `3250f9d`.

## 1. What was added

| File | Purpose |
| --- | --- |
| `scripts/apk-release-gate.sh` | Production gate orchestrator: secret-gated sign (`apk-sign.sh`, `adbsign --sign-key`) → verify (`apk-verify.sh --keys-dir` + `--sha256` full-file integrity) → provenance manifest. Fail-closed: missing secret→1 (BLOCKED), anchor missing/fingerprint mismatch→1, missing apk→2, sign fail→3, verify/provenance fail→4. Never `--allow-untrusted`, never publishes. |
| `.github/workflows/apk-sign.yml` | `workflow_dispatch` workflow. `test` mode uses an ephemeral key end-to-end; `production` mode requires `GH_RELEASE_APK_SIGNING_KEY` + approved anchor + fingerprint. `negative-controls` job runs fail-closed gate cases. No upload/publish step. |
| `Makefile` | `apk-sign`, `apk-verify`, `apk-release-gate-test` targets (test path; reference for the production wiring). |
| `tests/package/apk_signing_test.go` | +3 gate-orchestrator tests (missing secret, fingerprint mismatch, missing anchor). |

## 2. Test-mode verification (ephemeral key, real APK)

On instabox with the OpenWrt SDK host `apk` (apk-tools 3.0.5) + real
x86_64 r5 apk:

- Gate e2e (sign → verify → provenance) **PASS**; signed apk + `provenance.txt`
  (sha256) emitted.
- Negative gates exit correctly: missing secret → 1 (BLOCKED), wrong
  fingerprint → 1, missing anchor → 1, missing apk → 2.
- Go suite: `TestApkSignVerifyRoundtrip` PASS (APK_TOOL+TEST_APK); all 7
  `Apk`-prefixed tests PASS.
- Clean-VM install without `--allow-untrusted` (Prompt 35 evidence,
  re-used): signed install/upgrade/uninstall OK; unsigned rejected
  (`UNTRUSTED signature`); tampered rejected (`uninstallable`).

## 3. Production wiring (BLOCKED → READY on approval)

Reproduction: an approved APK signing key/secret + an approved public
anchor and its fingerprint.

```sh
# production gate (runs in the release path once provisioned)
APK_SIGN_KEY="$(cat ${GH_RELEASE_APK_SIGNING_KEY})" \
  bash scripts/apk-release-gate.sh \
    --apk goshisen-0.2.0-r5.apk \
    --anchor keys/goshisen-apk.pub.pem \
    --fingerprint <approved-fp> \
    --commit <sha> --tag v0.2.2 --builder release.yml/apk \
    --outdir dist-apk
# then install the anchor into the feed trust store:
#   install -m 0644 keys/goshisen-apk.pub.pem /etc/apk/keys/
# and publish only the gate-passed artifacts (never --allow-untrusted).
```

The gate emits a provenance manifest (`dist-apk/provenance.txt`, apk →
sha256) that must be recorded with the release.

## 4. Scope exclusions honored

- No production key created/rotated/exported; `GH_RELEASE_APK_SIGNING_KEY`
  never read; no production trust-anchor change; no tag/release/APK
  upload/feed publish; no production router; no policy/payload change.
