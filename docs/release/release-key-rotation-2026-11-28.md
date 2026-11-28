# CLI release anchor rotation — 2026-11-28

**Type:** production trust-anchor rotation for the CLI release signing key
(`GH_RELEASE_SIGNING_KEY`). **No runtime, CLI, or packaged-artifact change.**
**Applies to:** every release after v0.2.9. v0.2.1–v0.2.9 keep verifying with
the retired anchor (still committed, §3).

## 1. Why the anchor moved

The owner's workstation held a local Ed25519 keypair that was **not** the
production anchor: it had been generated on 2026-11-14 for local dry-run
experiments, matched only its own `.pub` file, and was rejected by the
production path (`verify-signatures.sh` → exit 4, `check-key-match.sh` → exit 1).
It was therefore dead weight — a key that could not sign anything accepted by
the project, and a decoy that could be mistaken for the production key.

Owner decision (2026-11-28): **rotate the anchor to that key** rather than delete
it, so the key material already held by the owner is the production anchor
instead of an unreferenced orphan. No new key material was generated; the
private half was relocated into `~/.keys/goshisen-release-key/` (0700/0600) with
a custody `README.txt`.

## 2. Key facts

| | Retired anchor | Current anchor |
| --- | --- | --- |
| Algorithm | Ed25519 (PKCS#8 PEM) | Ed25519 (PKCS#8 PEM) |
| Fingerprint (SHA-256 DER SPKI) | `24215e016621925215dec0ad6ea14ac74253a4e97249a973e89d649a2217cca0` | `e9b4bf1194ce90d2377720102748460404eacbc7aba9aa8ad48532cd63294fb9` |
| Key ID (first 16 hex of raw pubkey) | `f955ede984245d64` | `bdda690b97a4f49e` |
| Created | 2026-11-13 | 2026-11-14 (promoted 2026-11-28) |
| Signed releases | v0.2.1 – v0.2.9 | after v0.2.9 |
| Public key in git | `keys/goshisen-release-2026-11-13.pub.pem` (archived) | `keys/goshisen-release.pub.pem` |
| Private key | GitHub secret (environment `signing`), superseded | GitHub secret (environment `signing`) + owner custody |

Both public keys are committed, so an old release can always be verified from the
repository alone — no trust gap and no re-signing of published artifacts.

## 3. What changed in the repository

- `keys/goshisen-release.pub.pem` — now the rotated anchor (public half only).
- `keys/goshisen-release-2026-11-13.pub.pem` — **new**: the retired anchor,
  committed so v0.2.1–v0.2.9 stay verifiable. `scripts/check-backup-key.sh`
  scans `keys/*.pub.pem`, so a pre-rotation offline backup is now reported as
  "matches: goshisen-release-2026-11-13.pub.pem" instead of a bare mismatch.
- **Four** fingerprint pins (see `docs/release/f02-environment-migration.md` §0.1
  — that table previously listed only two; the test fixtures are hard pins too):
  `.github/workflows/release.yml` (`expected=` + header comment),
  `scripts/check-backup-key.sh` (`EXPECTED=` + label),
  `tests/package/production_signing_test.go` (`wantPubFingerprint`, `wantKeyID`),
  `tests/ci/release-workflow-guard-test.sh` (dry-run output assertion), plus the
  comment in `scripts/check-release-workflow-structure.py`.
- Live docs: `docs/installation.md`, `docs/openwrt.md`, `docs/release-checklist.md`,
  `docs/release/signing-design.md`, `docs/release/apk-signing-key-custody.md`,
  and the Vietnamese translations of `installation.md` / `openwrt.md` /
  `CHANGELOG.md`. Historical evidence records (`v0.2.*-notes.md`,
  `evidence-index` rows, `f02-*` migration record) are **not** rewritten — they
  state what was true when written.
- Install docs now tell users to fetch `public_key.pem` **from the release they
  are installing** instead of from `main`, which is what makes a rotated anchor
  safe for old releases.

## 4. Local verification (executed before the PR)

```sh
# the private half really is the committed anchor
bash scripts/check-backup-key.sh --key ~/.keys/goshisen-release-key/key.pem   # expect: MATCH (e9b4bf11...), exit 0
bash scripts/check-key-match.sh ~/.keys/goshisen-release-key/key.pem keys/goshisen-release.pub.pem   # exit 0

# the repo's own gates, on the rotated tree
bash tests/ci/release-workflow-guard-test.sh      # exercises the real anchor gate + dry-run path
bash tests/ci/check-backup-key-test.sh
python3 scripts/check-release-workflow-structure.py
go test ./tests/package -count=1                  # production_signing_test pins the anchor fp + key ID
make check

# end-to-end signing round trip with the rotated key (no secrets, local temp dir)
RELEASE_SIGN_KEY="$(cat ~/.keys/goshisen-release-key/key.pem)" \
  bash scripts/release-sign.sh --dist <dummy-dist> --commit <sha> --tag dry-run --builder local/rotation --toolchain "$(go version)"
bash scripts/verify-signatures.sh --dist <dummy-dist> --public-key keys/goshisen-release.pub.pem --commit <sha> --require-sign   # VERIFY-PASS

# the retired anchor must NOT verify a rotated-key signature (negative control)
bash scripts/verify-signatures.sh --dist <dummy-dist> --public-key keys/goshisen-release-2026-11-13.pub.pem --commit <sha> --require-sign   # exit 4 (bad signature)
```

Results (2026-11-28, base `9834600`, tree with the rotation applied):

| Check | Result |
| --- | --- |
| `check-backup-key.sh --key ~/.keys/goshisen-release-key/key.pem` | `MATCH` (e9b4bf11...), exit 0 |
| `check-key-match.sh <key> keys/goshisen-release.pub.pem` | exit 0 |
| `tests/ci/release-workflow-guard-test.sh` | `RELEASE-WORKFLOW-GUARD-TEST-PASS` (executes the real anchor gate + dry-run step bodies) |
| `tests/ci/check-backup-key-test.sh` | `CHECK-BACKUP-KEY-TEST-PASS` |
| `scripts/check-release-workflow-structure.py` | `RELEASE-WORKFLOW-STRUCTURE-PASS` |
| `go test ./tests/package -count=1` | ok (19.3s) — includes the anchor fp / key-ID pin |
| `make check` | ok (fmt, vet, full test suite, static build) |
| `release-sign.sh` with the rotated key | `VERIFY-PASS` against the new anchor, exit 0 |
| `verify-signatures.sh` with the new anchor | `VERIFY-PASS`, exit 0 |
| `verify-signatures.sh` with the **retired** anchor (negative control) | `VERIFY-FAIL: bad-signature`, exit 4 |
| `release-sign.sh` with a foreign throwaway key (negative control) | exit 1, key-mismatch, no key material printed |

## 5. Execution log (executed 2026-11-28)

| # | Step | Result |
| --- | --- | --- |
| 1 | Anchor + archived anchor + 4 pin sites + live docs merged to `main` | PR #152 → squash `2c364bf` (base `9834600`); CI runs `46107252523` / `46107257204` — every job success, incl. `release-workflow-guard (F-02)`, `check-backup-key (F-02)`, `sign + verify release artifacts (test key, fail-closed)` |
| 2 | `gh secret set GH_RELEASE_SIGNING_KEY --env signing` with the rotated private key | done (`2026-11-28T04:35:08Z`); set only after `check-backup-key.sh` printed `MATCH (e9b4bf11...)` and exit 0; value never echoed |
| 3 | Dry run #1 on `main` — environment secret must resolve to the rotated key | run [`46108232674`](https://github.com/88kbleirad/GoShiSen-Enforce/actions/runs/46108232674) on `2c364bf`, **success** (waited for the `signing` required reviewer; approved via the deployments API). Proof: `OK: trust anchor fingerprint verified e9b4bf11...`, `VERIFY-PASS`, `OK: dry-run signature verified (public key fingerprint e9b4bf11...)`; leak scan: 0 private-key lines |
| 4 | Delete the **repository-level** `GH_RELEASE_SIGNING_KEY` copy (it still held the retired private key) | deleted; names-only listings now show the CLI key **only** under environment `signing` (plus the untouched `GH_RELEASE_APK_SIGNING_KEY`) |
| 5 | Dry run #2 on `main` — key must resolve from the environment scope alone | run [`46108443415`](https://github.com/88kbleirad/GoShiSen-Enforce/actions/runs/46108443415), **success**, same three proof lines; `Download build artifacts` / `Sign release artifacts` / `Upload release assets` all skipped (dry-run path), leak scan clean |
| 6 | Evidence follow-up: run IDs + leak scan recorded here, in `docs/release/evidence-index.md` and `docs/release/apk-signing-key-custody.md` §6 | this revision |

### Post-rotation secret state

- Environment `signing` (id `27311707581`, required reviewer `88kbleirad`,
  custom branch policies) holds **only** the rotated CLI key
  (`GH_RELEASE_SIGNING_KEY`, `2026-11-28T04:35:08Z`) alongside
  `GH_RELEASE_APK_SIGNING_KEY`.
- The repository-level copy is gone — this also completes the F-02 §0.1 step 5
  that was still outstanding (`docs/release/f02-environment-migration.md`).
- The **retired private key no longer exists in GitHub**: step 4 deleted the
  repository-level copy and the environment secret now holds the rotated key. The
  retired private half is **not** in this repository, and this record states no
  path for any copy of it — whether one still exists is the owner's to confirm.
  This does not affect verification of published releases, because the
  retired *public* anchor is committed
  (`keys/goshisen-release-2026-11-13.pub.pem`) and shipped as each release's
  `public_key.pem` asset. A rollback that requires *signing* with the retired key
  therefore needs that private half from outside this repository.
- No tag, release, or published asset was created, moved, or re-signed at any
  step; the rotation takes effect on the next tag after v0.2.9.

## 6. Rollback

- **Repin the anchor (preferred):** restore `keys/goshisen-release.pub.pem` from
  `keys/goshisen-release-2026-11-13.pub.pem`, revert the four pins, and re-store
  the retired private key in the environment secret. That re-store needs the
  retired private half, which is **not** in this repository: step 4 deleted the
  repository-level copy, and this record states no other location. Whether a copy
  still exists — the owner's own, or one taken before that deletion — is for the
  owner to confirm.
- **Rotate again:** generate a fresh Ed25519 key, commit its public half, update
  the four pins, store the new secret. Old releases keep verifying against the
  archived anchors either way.
- Published artifacts are never re-signed or withdrawn by a rollback; a new
  anchor signs the next release only.

## 7. Custody

- Private key: `~/.keys/goshisen-release-key/key.pem` (0600, dir 0700) on the
  maintainer's workstation + GitHub environment secret `signing`
  (`GH_RELEASE_SIGNING_KEY`, required reviewer `88kbleirad`).
- Public key: `keys/goshisen-release.pub.pem` (committed; also shipped as the
  `public_key.pem` asset of every release).
- The key is **not** the APK key (`keys/goshisen-apk.pub.pem`, EC P-256) and the
  two channels are never interchanged — see
  `docs/release/apk-signing-key-custody.md`.
