# APK signing key — custody, approval, rotation, revocation (handoff)

**Status: BLOCKED — awaiting owner approval + authorized provisioning.**
**Base:** main `8b90c7c`. **Date:** 2026-11-14.

This document is the governance handoff for the production APK signing
key. The engineering path is complete and fail-closed
(`scripts/apk-release-gate.sh`, `.github/workflows/apk-sign.yml`,
`docs/release/prompt37-apk-staging-verification.md`); what remains is
administrative and MUST be performed by the authorized key manager /
repository owner. The agent does not create, read, export, rotate, or
revoke private keys, and does not provision secrets.

## 1. Current state (verified 2026-11-14 at `8b90c7c`)

| Item | State |
| --- | --- |
| `GH_RELEASE_APK_SIGNING_KEY` | **absent** (only `GH_RELEASE_SIGNING_KEY` exists — CLI-release key; MUST NOT be reused for APK) |
| `keys/goshisen-apk.pub.pem` | **absent** |
| Approval record | **absent** |
| Owner / rotation / revocation record | **absent** |
| Engineering gate | implemented + fail-closed (missing secret → exit 1 BLOCKED) |
| Verdict | **BLOCKED** |

## 2. Approval record (to be completed by the owner)

Copy this into the approval ticket/PR and fill every field before any
provisioning step:

```
APK SIGNING KEY APPROVAL
  owner:                 Lê Thanh Phong (maintainer)
  scope:                 OpenWrt .apk package signing only (NOT CLI releases)
  environments:          staging: yes   production: yes
  key custody:           GitHub Actions secret GH_RELEASE_APK_SIGNING_KEY; offline backup held by the owner only
  algorithm/format:      EC P-256 (prime256v1) PEM
  rotation period:       12 months / rotation owner: Lê Thanh Phong
  revocation contact:    thanhphong2k52203@gmail.com
  emergency recovery:    offline backup held by the owner; if lost, treat as revocation + rotation
  fingerprint (SHA-256 DER SPKI): 542bb7ae9058fee9b4d37ed30b0af44f234ea7cb8866daa1ffc71a7bc37c839d  -
  approved by:           Lê Thanh Phong, 2026-11-14, <PR/ticket>
  note: GH_RELEASE_SIGNING_KEY (CLI-release key) is NOT an approved APK signing key
```

The record MUST also state explicitly that `GH_RELEASE_SIGNING_KEY`
(the CLI-release key) is **not** an approved APK signing key.

## 3. Provisioning procedure (authorized personnel only)

1. **Generate** the keypair with the approved tool in the approved
   environment (never in a repo, never in CI logs):
   `openssl ecparam -name prime256v1 -genkey -noout -out apk-key.pem`
   `openssl ec -in apk-key.pem -pubout -out goshisen-apk.pub.pem`
2. **Store the private key** in the approved secret manager under the
   name `GH_RELEASE_APK_SIGNING_KEY` (GitHub Actions secret). Never
   commit it, never paste it into logs or tickets.
3. **Commit only the public anchor** `keys/goshisen-apk.pub.pem` via a
   dedicated PR (reviewed; not part of a packaging change).
4. **Record the full fingerprint** (SHA-256 of DER SPKI):
   `openssl pkey -pubin -in keys/goshisen-apk.pub.pem -outform DER | sha256sum`
   Cross-check it out-of-band with the key owner; paste it into the
   approval record and the readiness report.
5. **Enable main branch protection** if approved (require PR review +
   status checks for the release path).
6. **Verify without reading**: `gh secret list` shows the name only
   (value never printed); confirm workflow masking covers any
   accidentally-echoed value.

## 4. Staging verification checklist (run after provisioning)

```sh
# a) anchor fingerprint matches the approval record
openssl pkey -pubin -in keys/goshisen-apk.pub.pem -outform DER | sha256sum

# b) production gate on the real r5 APK
APK_SIGN_KEY="$(cat ${GH_RELEASE_APK_SIGNING_KEY})" \
  bash scripts/apk-release-gate.sh --apk goshisen-0.2.0-r5.apk \
    --anchor keys/goshisen-apk.pub.pem --fingerprint <approved-fp> \
    --commit 8b90c7c --tag v0.2.2-staging --builder staging/apk \
    --outdir dist-apk
# expect: "OK: APK release gate passed" + dist-apk/provenance.txt (sha256)

# c) trust store + clean-VM install (no --allow-untrusted)
install -m 0644 keys/goshisen-apk.pub.pem /etc/apk/keys/
apk add <signed.apk>            # install
apk add <signed.apk>            # upgrade/idempotent
apk del goshisen                # uninstall

# d) negative controls (all must fail closed)
#    missing secret / missing anchor / fingerprint mismatch / wrong key /
#    tampered APK / checksum mismatch / provenance mismatch
```

## 5. Rotation and revocation

- **Rotation** (on schedule or incident): generate a new keypair, add the
  new anchor alongside the old in `/etc/apk/keys/` during overlap,
  re-sign packages, then retire the old anchor; record dates + fingerprints
  in this file and the approval ticket.
- **Revocation** (compromise): remove the anchor from `/etc/apk/keys/`
  and the feed trust store, delete the GitHub secret, revoke the ticket,
  and re-sign all distributable packages with the replacement key. Update
  this document's history section.
- **Recovery**: if the secret is lost, treat as revocation + rotation;
  never reconstruct the private key from artifacts.

## 6. History

| Date | Event | Fingerprint | Reference |
| --- | --- | --- | --- |
| 2026-11-14 | handoff created; no key provisioned | — | this document |
