# Prompt 39 — Production APK signing staging verification results

**Status:** complete — **staging verified; readiness proposal: READY**
(for the signing path; feed/release publish remains a separate approval).
**Date:** 2026-11-14/15. **Base:** main after `8b90c7c` + PR #34
(`f46443a`); staging branch `staging/apk-r5` (deleted after use).

## 1. Provisioning status (verified, presence-only)

| Prerequisite | State |
| --- | --- |
| Approval record | Filled in `docs/release/apk-signing-key-custody.md` (owner Lê Thanh Phong, APK-only scope, staging+production, custody, 12-month rotation, revocation contact, emergency recovery, fingerprint, approved-by) |
| `GH_RELEASE_APK_SIGNING_KEY` | Provisioned (presence confirmed via `gh secret list`; value never read/printed) |
| Public anchor `keys/goshisen-apk.pub.pem` | Merged via PR #34 (`f46443a`) |
| Approved fingerprint | `542bb7ae9058fee9b4d37ed30b0af44f234ea7cb8866daa1ffc71a7bc37c839d` — matches the anchor's DER-SPKI SHA-256 exactly (out-of-band record) |
| `GH_RELEASE_SIGNING_KEY` (CLI key) | NOT accepted for APK: its anchor fingerprint `24215e…` fails the gate's fingerprint check (exit 1) — proven |

## 2. Production gate run (GitHub Actions, production mode)

Workflow `.github/workflows/apk-sign.yml` (`workflow_dispatch`, mode=production)
on the staging branch, in the OpenWrt SDK container
(`openwrt/sdk:x86_64-25.12.6`, root, bash shell; apk-tools v3.0.5 static):

```
OK: anchor keys/goshisen-apk.pub.pem fingerprint 542bb7ae…
OK: signed dist-apk/goshisen-0.2.0-r5.apk
OK: dist-apk/goshisen-0.2.0-r5.apk (trusted signature + sha256 match)
OK: APK release gate passed (signed+verified) in dist-apk
```

Jobs: `sign` SUCCESS, `negative-controls` SUCCESS. Run: 43619751612
(also 43619447442). Artifact `signed-apks`:
`goshisen-0.2.0-r5.apk` sha256 `00c8376d8a70e48bf7a8c8894ab6c93b96f735d81a89a1502db176baee866fd9`
+ `provenance.txt` (apk → sha256).

Secret leakage: run log contains no private key material (only test-job
source lines with fixture/placeholder values; the production secret is
masked/injected, never printed).

## 3. Clean-VM package-manager verification (no --allow-untrusted)

OpenWrt 25.12.6 x86_64 clean VM; production anchor installed at
`/etc/apk/keys/goshisen-apk.pub.pem`.

| Check | Result |
| --- | --- |
| Install signed APK (no `--allow-untrusted`) | **PASS** — `(4/4) Installing goshisen (0.2.0-r5)`; `apk list` → `goshisen-0.2.0-r5 x86_64 [installed]`; binary sha `ed8087c2…` |
| Upgrade path (reinstall signed) | PASS — `OK: 26.9 MiB in 186 packages` |
| Uninstall | PASS — `Purging goshisen`; binary removed |
| Unsigned APK (no flag) | **REJECTED** — `ERROR: … UNTRUSTED signature` |
| Wrong key (anchor removed) | **REJECTED** — `ERROR: … UNTRUSTED signature` |
| Tampered (append/byte) — package manager | apk-tools v3 verifies the embedded signature/control block; local-install does NOT catch trailing/compressed-data tampering. **The release gate’s full-file `--sha256` check catches it** (below) |

## 4. Release-gate tamper/checksum/provenance negatives (production artifact)

On the **production-signed artifact** (sha `00c8376d…`):

| Case | Result |
| --- | --- |
| Pristine signed + `--sha256` recorded | OK (trusted signature + sha256 match) |
| Append-tampered + recorded sha256 | exit 4 (UNTRUSTED / mismatch) |
| In-band byte-tampered + recorded sha256 | exit 4 (UNTRUSTED / mismatch) |
| Fingerprint mismatch (CLI anchor) | exit 1 (BLOCKED) |
| Missing secret / missing anchor / missing apk | exit 1 / 1 / 2 (fail-closed; Go tests + workflow negatives) |

Workflow `negative-controls` job (fixture + real apk) PASS:
missing secret, wrong fingerprint, missing anchor, missing apk, wrong key,
unsigned, tampered, checksum mismatch, provenance present.

## 5. Readiness proposal

- **APK signing path: READY for staging** (all checks above PASS; secret
  provisioned; anchor approved; gate fail-closed).
- **Still NOT published**: no feed/release publish, no production asset
  upload, no production router install — those require a separate release
  approval.
- Owner/rotation/revocation/recovery records exist in
  `docs/release/apk-signing-key-custody.md`.
