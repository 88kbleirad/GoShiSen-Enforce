# F-02 re-check — PR #114 + #115 merged; environment secret re-set; dry run re-dispatch pending approval

Date: 2026-11-22. Scope: verify the merged F-02 state (issue #100) and
record the evidence so a re-check reviewer can confirm each item
independently.

## 1. What is on `main` (merged, verified)

| Commit | PR | Content |
| --- | --- | --- |
| `99de354` | #114 | release.yml split into `validate`/`build`/`sign`; `sign` under `environment: release-cli` (required reviewer), the only `contents: write` job; fail-closed anchor-fingerprint + asset-checksum gates; `workflow_dispatch` dry-run path (main-only, dummy file, prints only verification + fingerprint); F-02 structural+behavioural guard + fixtures wired into ci.yml; docs. |
| `073b365` | #115 | `scripts/release-sign.sh` secret-shape diagnosis (PEM private key / public / other / not PEM — length + category only, never the value); guard fixtures for base64/public-key mis-pastes; migration doc history + release-checklist F-02 pre-flight; rollback triage rows. |

Re-verified on `main` at `073b365` (2026-11-22, fresh workspace):

```
RELEASE-WORKFLOW-GUARD-TEST-PASS
actionlint 1.7.7 (checksum-verified): exit 0 on .github/workflows/*.yml
python3 -m py_compile scripts/check-release-workflow-structure.py  → OK
bash -n scripts/release-sign.sh tests/ci/release-workflow-guard-test.sh → OK
```

## 2. Environment / secret state (names and timestamps only — no values)

| Item | State |
| --- | --- |
| `release-cli` exists | yes; required reviewer `88kbleirad` |
| Deployment policies | `main` (branch), `v*` (**branch**) — tag-type `v*` still required for real tag releases |
| Environment secret | `GH_RELEASE_SIGNING_KEY` — **updated 2026-11-22T04:54:12Z** (re-set by the maintainer after the first two dry-run failures) |
| Repository-level secret | `GH_RELEASE_SIGNING_KEY` still present (removed by the maintainer only after a green dry run) |

## 3. Dry-run runs (evidence)

| Run | Ref | Outcome | Evidence |
| --- | --- | --- | --- |
| `44981019938` | main | failure, fail-closed before signing: `ERROR: cannot derive public key from …/goshisen-release-key.pem` ⇒ environment secret was not a PEM private key | issue #100 comment `7074012726` |
| `44981062376` | `test/f02-dryrun-nonmain` | failure — environment policy rejection: `Branch "test/f02-dryrun-nonmain" is not allowed to deploy to release-cli due to environment protection rules.` (`sign` steps `[]`) | issue #100 comment `7074012726` |
| `44985559779` | main | failure, fail-closed before signing: `FAIL: RELEASE_SIGN_KEY is not a PEM private key (67 chars; no '-----BEGIN ...' line)` — diagnostics (#115) named the shape | issue #100 comment `7074402671` |

Leak scans on all three run logs: 0 PEM private-key blocks, 0 unmasked
secret prints, `RELEASE_SIGN_KEY: ***` masked, all ≥40-char tokens are
hex SHAs or the public fingerprint → clean. No release/tag/artifact was
created by any dry run (releases list unchanged; artifact count 0).

## 4. Next step

With the environment secret re-set (`updated=2026-11-22T04:54:12Z`),
re-dispatch the dry run on `main` (`gh workflow run release.yml --ref main
-f dry_run=true`), await the reviewer approval, and record the run ID,
`VERIFY-PASS`, and the `24215e01…` fingerprint. Only after a green dry
run: remove the repository-level secret per
`docs/release/f02-environment-migration.md`, then add the `v*`
tag-type deployment policy before the next `v*` release.
