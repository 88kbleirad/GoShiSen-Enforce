# Release-run wrapper (`scripts/release-run.sh`, P3.11 / issue #142)

One script for a whole release run: pre-tag gate → tag → release workflow →
post-tag pin PR → apk build A/B → manifest + pin gates → unsigned VM smoke →
draft upload → production apk signing → signed smoke/attach → publish flip →
post-publish sweep.

**Status: slice 1 implemented — `--dry-run` only.** It prints, per step, the
exact command lines and the evidence that step must produce, and executes
nothing: no tag, no build, no signing, no upload, no publish, no file written.
`--execute` is not implemented yet and **exits 3** rather than half-running a
plan; the remaining slice (issue #142) runs the credential-free steps, stops at
every credential-gated step with the exact `gh` command, and writes one evidence
file per run (step → command → output → sha256) under
`evidence/release-run/<tag>-<UTC>/`.

## Usage

```sh
scripts/release-run.sh --tag vX.Y.Z --dry-run [--evidence-dir DIR] [--repo-slug OWNER/NAME]
```

| exit | meaning |
| --- | --- |
| 0 | dry-run plan printed; nothing executed |
| 2 | usage (missing or conflicting arguments) |
| 3 | `--execute` — not implemented in this slice |
| 4 | tag rejected by `scripts/validate-release-tag.sh` (S-003) |

The tag name is validated **before** it is used anywhere: the printed commands
interpolate it, so a specially-named tag must not reach a shell.

## The plan

| # | step | owner | mutates |
| --- | --- | --- | --- |
| 1 | tag-name guard + F-01 pre-tag gate (`scripts/release-pretag-check.sh`) | agent | no |
| 2 | annotated tag + push | maintainer | yes |
| 3 | release workflow `validate`→`build`→`sign` (draft, `environment: signing` approval) | maintainer | yes |
| 4 | post-tag pin PR (`openwrt/Makefile` only) | maintainer | yes |
| 5 | apk build A/B with `scripts/build-apk-release.sh` | agent | yes |
| 6 | `check-apk-build-manifest.sh` + `release-pin-gate.sh --manifest` | agent | no |
| 7 | unsigned VM smoke (privileged QEMU lab) | agent | yes |
| 8 | upload the unsigned apk into the draft | maintainer | yes |
| 9 | `apk-sign.yml` production dispatch | maintainer | yes |
| 10 | signed VM smoke + attach the signed apk + notes | maintainer | yes |
| 11 | publish flip (`gh release edit --draft=false`) | maintainer | yes |
| 12 | post-publish sweep (`verify-release-assets.sh` + unauthenticated API check) | agent | no |

Steps marked *maintainer* are credential-gated: the wrapper never holds or uses
the CLI or APK signing key; it prints the `gh` invocation instead.

### Hard stops (never crossed automatically)

* step 1 fails — pre-tag gate red → **no tag**;
* step 5 produces non-identical A/B hashes → no gate, no upload, no signing;
* step 6 fails — manifest or pin/version gate red → no upload, no signing.

Any failing step stops the run; a later step never “carries on”.

## Where the plan comes from

Codified from the v0.2.9 run, not invented: `docs/release/v0.2.9-notes.md`
(“Reproduction”), `docs/release/v0.2.9-apk-build-recipe.md` §9,
`docs/release/release-pin-gate.md`, `docs/release-checklist.md`
§ *Release mechanics* / *Post-release verification*, and the dispatch inputs of
`.github/workflows/release.yml` + `apk-sign.yml`.

One step is honest about a gap: the v0.2.9 VM smoke was driven by
`run-029-smoke.sh`, a handoff script that is **not** committed. Step 7 prints the
committed lab surface (`scripts/openwrt-lab.sh`, `scripts/openwrt-console.py`)
and the checks that were run, and says the driver is missing rather than
pretending a committed one exists.

## Verification

```sh
bash tests/ci/release-run-test.sh     # RELEASE-RUN-TEST-PASS
```

The test pins the contract: every step and its commands are present, the tag is
validated by the S-003 guard, `--execute` fails closed, and the dry-run creates
no tag, no directory and no working-tree change. It runs in CI as
`release-run dry-run (P3.11)`.
