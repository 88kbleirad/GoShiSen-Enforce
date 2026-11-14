# Prompt 37 — Provision production APK key + staging verification

**Status:** complete (audit/runbook) — **verdict: BLOCKED**.
**Date:** 2026-11-14. **Base:** main `94bd7df`.

## 1. Prerequisite audit (exact commit `94bd7df`)

| Prerequisite | Required | Observed |
| --- | --- | --- |
| Approved production APK signing authority | explicit approval | **not evidenced** (no record in repo/docs) |
| `GH_RELEASE_APK_SIGNING_KEY` secret | provisioned | **MISSING** (GitHub secrets list has only `GH_RELEASE_SIGNING_KEY` — the existing CLI-release key, NOT an APK key) |
| Public trust anchor for APK | committed + fingerprint | **not present** (`keys/goshisen-apk.pub.pem` does not exist; only the CLI-release anchor `keys/goshisen-release.pub.pem`) |
| Full public-key fingerprint | from approved trust source | unavailable (no APK anchor to derive from) |
| Owner/rotation/rollback records | documented | absent for APK key |
| main branch protection | recommended | `Branch not protected` (404) |

The audit is read-only; no private key was created/read/exported, no
secret was read (presence-only via `gh secret list`), no trust anchor was
modified.

## 2. Gate wiring verification (production mode, fail-closed)

At `94bd7df`, `scripts/apk-release-gate.sh` with **no**
`APK_SIGN_KEY` (production mode) fails exactly as designed:

```
FAIL: APK_SIGN_KEY (GH_RELEASE_APK_SIGNING_KEY) is missing
rc=1
```

So the pipeline is ready to consume the approved key the moment it is
provisioned; it cannot silently fall back to `--allow-untrusted` or to
the CLI-release key.

## 3. Staging verification runbook (to execute once the key is provisioned)

```sh
# 1. Provision secret (approved secret manager / GitHub Actions secret):
#     name: GH_RELEASE_APK_SIGNING_KEY (content = PEM private key)
#     Never log/export; workflow receives it only via env injection.

# 2. Commit the approved public anchor + record its full fingerprint:
#     openssl pkey -pubin -in keys/goshisen-apk.pub.pem -outform DER \
#       | sha256sum      # full fingerprint -> record in this doc

# 3. Run the production gate at staging (real r5 APK):
APK_SIGN_KEY="$(cat ${GH_RELEASE_APK_SIGNING_KEY})" \
  bash scripts/apk-release-gate.sh \
    --apk goshisen-0.2.0-r5.apk \
    --anchor keys/goshisen-apk.pub.pem \
    --fingerprint <approved-fp> \
    --commit 94bd7df6b8f717d4957edacd811b4d26f98e5ae1 \
    --tag v0.2.2-staging --builder staging/apk-sign \
    --outdir dist-apk
# Expect: "OK: APK release gate passed" + dist-apk/provenance.txt

# 4. Install the anchor into the staging feed trust store:
install -m 0644 keys/goshisen-apk.pub.pem /etc/apk/keys/
# 5. Clean-VM install WITHOUT --allow-untrusted; verify install/upgrade/
#     uninstall; verify unsigned/tampered/wrong-key rejection.
# 6. Negative controls (production-like): missing secret, missing anchor,
#     fingerprint mismatch, checksum mismatch, provenance mismatch,
#     signature tampering -> all must fail closed.
```

## 4. What blocks READY (unchanged, missing)

1. Explicit approval of the production APK signing key (owner approval
   record).
2. Provisioning of `GH_RELEASE_APK_SIGNING_KEY` (secret manager).
3. Committed approved APK public anchor + full fingerprint (from the
   approved trust source, cross-checked out-of-band).
4. Owner / rotation / revocation / rollback records for the APK key.
5. (Recommended) main branch protection for the release path.

No feed/release publish, no production asset upload, no production router
install.

## 5. Scope exclusions honored

- No production key created/rotated/exported; `GH_RELEASE_APK_SIGNING_KEY`
  not read (presence-only); no trust-anchor modification; no
  tag/release/feed publish; no production router; no policy/payload
  change. `--allow-untrusted` never used on any production path.
