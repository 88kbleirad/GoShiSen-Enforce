# Release pin/version agreement gate (issue #49)

**Status:** implemented (PR pending). Introduced for the v0.2.4 release
run; backfilled into the release checklist.

## Problem

The OpenWrt package Makefile (`openwrt/Makefile`) lives in the same
repository as the daemon source, so the tag commit cannot contain its
own hash. The v0.2.3 release deliberately pinned `PKG_SOURCE_VERSION`
to the v0.2.2 tag commit (`c5de6e8`) while labeling the package
0.2.3 (issue #49). The runtime source was byte-identical between the
two commits, so there was no functional impact, but the version label
and the source commit did not identify the same release — a
traceability gap.

Fake equality is not possible (self-referential hash), so this gate
replaces "pin == tag" with checkable claims that are sufficient to
make the published artifact traceable to a single source tree.

## What the gate proves

`scripts/release-pin-gate.sh` — run against the release build outputs:

1. **PKG_VERSION equals the release version** (`--tag vX.Y.Z` →
   `X.Y.Z` must equal `PKG_VERSION` in `openwrt/Makefile`).
2. **PKG_SOURCE_VERSION is an ancestor of (or equal to) the tag
   commit** (`git merge-base --is-ancestor`). This lets the release pin
   the tag commit itself (recommended) or a commit on the path to it.
3. **Source equivalence** — `git diff <pin> <tag> -- cmd internal pkg
   go.mod go.sum openwrt/files` is EMPTY. If not, the gate fails and
   prints the diff. This is the claim that replaces "the pin IS the
   tag": the pinned source and the tagged source are identical over the
   entire runtime tree plus the packaged payload files.
4. **The payload binary `--version` equals `PKG_VERSION`** (the
   extracted `usr/sbin/goshisen` from the apk).
5. **The apk filename version equals `PKG_VERSION-r<PKG_RELEASE>`**
   (e.g. `goshisen-0.2.4-r1.apk`).
6. **Tag signature** (SSH `allowed_signers`, enabled with
   `GATE_TAG_SIGN_CHECK=1`): the release tag must be signed by a key in
   `docs/trust/allowed_signers` (signed-tags-plan, v0.2.5+).
7. **S-005 payload Go-version gate** (`GATE_S005_CHECKS=1`): the apk
   payload's embedded Go toolchain must be >= `MIN_GO_VERSION`
   (`packaging/go-toolchain.env`). Runs
   `scripts/check-apk-payload-go.sh`.
8. **S-005 payload govulncheck gate** (`GATE_S005_CHECKS=1`):
   govulncheck binary-mode on the extracted payload; fails on any
   reachable stdlib/module vulnerability not in
   `packaging/govulncheck-allowlist.txt` (empty by default). Runs
   `scripts/check-apk-payload-govuln.sh`. The payload must carry Go
   buildinfo (the v0.2.6 package recipe keeps buildid).
9. **apk build manifest** (opt-in, `--manifest FILE` /
   `GATE_MANIFEST=FILE`): the manifest written by
   `scripts/build-apk-release.sh` must describe THIS commit (the tag
   commit) and THIS apk. `scripts/check-apk-build-manifest.sh` re-derives
   every claim: the `openwrt/**` file set and hashes equal the committed
   bytes, the overlay/`sdk_config` hashes equal the committed files,
   replaying committed Makefile + committed overlay + the manifest's
   declared substitutions reproduces `recipe.makefile_final_sha256`,
   `pkg.source_version` is an ancestor-or-equal of the commit, the Go
   version/toolchain hashes satisfy `packaging/go-toolchain.env`, the SDK
   digest equals the digest pinned in `.github/workflows/apk-sign.yml`,
   and the apk/payload hashes match the files (with `--compare`, the A/B
   partner must agree on inputs and artifact hashes).

## Build recipe (what produces the artifact this gate inspects)

`scripts/build-apk-release.sh` builds the unsigned apk from committed
inputs only: it `git archive`s `--commit` into a scratch dir, applies the
committed overlay `packaging/apk-build/no-golang-host.patch` (never the
working tree's `openwrt/Makefile`), folds in
`packaging/apk-build/sdk-x86_64.config`, mounts the digest-pinned SDK
image and a sha256-verified official go1.27.4, derives (or verifies)
`PKG_MIRROR_HASH` with the zero-hash two-pass procedure, and writes
`build-manifest-<label>.json` next to the apk. Two runs (`--label A`,
`--label B`) must produce identical apk and payload hashes; that A/B
comparison is the reproducibility claim the notes may make. The release
run passes `--canonical-name` so the staged asset is named exactly
`goshisen-<ver>-r<rel>.apk`.

`packaging/apk-build/README.md` explains the two inputs; the v0.2.8
reconstruction and the exact provenance of that (uncommitted) recipe are in
`docs/release/v0.2.9-apk-build-recipe.md`.

## What the gate does NOT prove

- That the pin is the tag commit itself (impossible by construction).
- Mirror-hash correctness (`PKG_MIRROR_HASH`) — that is proven by the
  clean SDK download/verify step (release checklist §5/OpenWrt release gates).
- Binary reproducibility or apk signature/anchor validity — that is
  the job of the builds and `scripts/apk-release-gate.sh`.
- That the release ran on the tagged commit — the gate caller must
  point `--payload`/`--apk` at the actual artifacts of that run.
- (Step 9 only) that the mirror download was uncompromised beyond the
  SDK's sha256 check, or that a *signed* apk is trustworthy — that is
  the anchor's job (`scripts/apk-verify.sh`, `apk-sign.yml`).

## Next release procedure (v0.2.4)

1. Merge the implementation to `main`; all CI green.
2. Create the tag `v0.2.4` at the source head `T`:
   `git tag v0.2.4 && git push origin v0.2.4`.
3. Immediately after, commit a **Makefile-only follow-up** on `main`
   (`F`): set `PKG_VERSION:=0.2.4`, `PKG_RELEASE:=1`, and
   `PKG_SOURCE_VERSION:=<T>` (the tag commit SHA). Only
   `openwrt/Makefile` (and possibly docs/CI) change in `F`; the
   runtime tree stays identical to `T`.
4. Run the gate against the release build from `F`:
   ```sh
   scripts/release-pin-gate.sh \
     --apk goshisen-0.2.4-r1.apk \
     --tag v0.2.4 \
     --payload <extracted usr/sbin/goshisen>
   ```
   It must print `PASS`.
5. Pre-tag dry run (unchanged from §5 OpenWrt release gates): build the package from
   the tag commit `T` with the previous pin and record apk + payload
   sha256 in `/data/` — proves the mirror hash and reproducibility at
   the exact tagged source.
6. Record both in the release notes: the source commit the apk was
   built from (the pin, `T`) and the feed commit (`F`).

If a source change is needed after tagging, do NOT edit the tag: make a
new release (bump `PKG_VERSION`/`PKG_RELEASE`) or a follow-up release
commit that the gate will catch (source diff non-empty → gate fails).
