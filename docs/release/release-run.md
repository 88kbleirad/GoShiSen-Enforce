# Release-run wrapper (`scripts/release-run.sh`, P3.11 / issue #142)

One script for a whole release run: pre-tag gate → tag → release workflow →
post-tag pin PR → apk build A/B → manifest + pin gates → unsigned VM smoke →
draft upload → production apk signing → signed smoke/attach → publish flip →
post-publish sweep.

**Status: both modes implemented.** `--dry-run` prints the plan and executes
nothing. `--execute` runs the steps whose owner is `agent`, then stops at the
first `maintainer` step (needs credentials/settings) or `manual` step (needs the
interactive lab) with the exact commands to run, and records one evidence file
per run.

## Usage

```sh
# plan only
scripts/release-run.sh --tag vX.Y.Z --dry-run

# run what needs no credential, then hand over the gated steps
scripts/release-run.sh --tag vX.Y.Z --execute \
  [--from-step N] [--repo DIR] [--go-root DIR] [--out DIR] [--assets-dir DIR] \
  [--evidence-dir DIR] [--repo-slug OWNER/NAME]
```

| exit | meaning |
| --- | --- |
| 0 | dry-run printed, or every `agent` step completed |
| 1 | a step failed (fail-closed; later steps do not run) |
| 2 | usage (missing or conflicting arguments) |
| 4 | tag rejected by `scripts/validate-release-tag.sh` (S-003) |
| 5 | stopped at a `maintainer` step — run the printed commands, then re-run with `--from-step <next>` |
| 6 | stopped at the `manual` step (step 7, the lab smoke) |
| 7 | a step needs an input you did not pass (a `<placeholder>` would have been executed); nothing was run for that step |
| 8 | preflight failed: dirty tree, not on `main`, or `main` ≠ `origin/main` |

The tag name is validated **before** it is used anywhere: the printed commands
interpolate it, so a specially-named tag must not reach a shell.

## The plan

| # | step | owner | mutates |
| --- | --- | --- | --- |
| 1 | tag-name guard + F-01 pre-tag gate (`scripts/release-pretag-check.sh`) | agent (runs) | no |
| 2 | annotated tag + push | maintainer | yes |
| 3 | release workflow `validate`→`build`→`sign` (draft, `environment: signing` approval) | maintainer | yes |
| 4 | post-tag pin PR (`openwrt/Makefile` only) | maintainer | yes |
| 5 | apk build A/B with `scripts/build-apk-release.sh`, compared by `check-apk-build-manifest.sh --compare` | agent (runs) | yes |
| 6 | `check-apk-build-manifest.sh` + `release-pin-gate.sh --manifest` | agent (runs) | no |
| 7 | unsigned VM smoke (privileged QEMU lab) | **manual** | yes |
| 8 | upload the unsigned apk into the draft | maintainer | yes |
| 9 | `apk-sign.yml` production dispatch | maintainer | yes |
| 10 | signed VM smoke + attach the signed apk + notes | maintainer | yes |
| 11 | publish flip (`gh release edit --draft=false`) | maintainer | yes |
| 12 | post-publish sweep (`verify-release-assets.sh` + unauthenticated API check) | agent (runs) | no |

Steps 1, 5, 6 and 12 are run by `--execute`; every other step stops the run with
its exact command block (exit 5 or 6). Resume with `--from-step N`, which records
the skipped prefix as `skipped` in the evidence.

### Hard stops (never crossed automatically)

* step 1 fails — pre-tag gate red → **no tag**;
* step 5 produces non-identical A/B hashes → no gate, no upload, no signing;
* step 6 fails — manifest or pin/version gate red → no upload, no signing;
* any failing step stops the run; a later step never “carries on”.

## Evidence

`--execute` writes **one evidence file per run** at
`$HOME/release-evidence/<tag>-<UTC>/run.md` (override with `--evidence-dir`):
a table of `step | owner | status | exit | output sha256` plus, per step, the
tail of its output; the raw output of each run step is kept next to it as
`outputs/NN-step.log`. The default deliberately lives **outside** the worktree —
step 1's F-01 gate requires the tree to be exactly a commit, so evidence written
into the repo would block the tag it is meant to record.

## Where the plan comes from

Codified from the v0.2.9 run, not invented: `docs/release/v0.2.9-notes.md`
(“Reproduction”), `docs/release/v0.2.9-apk-build-recipe.md` §9,
`docs/release/release-pin-gate.md`, `docs/release-checklist.md`
§ *Release mechanics* / *Post-release verification*, and the dispatch inputs of
`.github/workflows/release.yml` + `apk-sign.yml`.

Two steps are honest about gaps instead of inventing commands:

* step 7 is `manual`: the v0.2.9 VM smoke was driven by `run-029-smoke.sh`, a
  handoff script that is **not** committed (v0.2.9 §9 says so), so the wrapper
  prints the committed lab surface (`scripts/openwrt-lab.sh`) and waits;
* the input flags (`--go-root`, `--out`, `--assets-dir`) replace the
  `<placeholder>`s of a plan whose values are not known yet; `--execute` refuses
  to run a step whose block still holds one.

## Verification

```sh
bash tests/ci/release-run-test.sh     # RELEASE-RUN-TEST-PASS
```

The test pins both contracts — every step and its commands, the S-003 guard, the
dry-run writing nothing — and drives `--execute` against scratch git fixtures
(`--repo`): preflight refusals (dirty tree, wrong branch), the F-01 gate failing
without tagging anything, the run stopping at step 2 with the step-1 output
sha256 recorded, `--from-step` resume marking the prefix `skipped`, and a step
with missing inputs never running. It runs in CI as
`release-run (P3.11)`.
