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

## What the gate does NOT prove

- That the pin is the tag commit itself (impossible by construction).
- Mirror-hash correctness (`PKG_MIRROR_HASH`) — that is proven by the
  clean SDK download/verify step (release checklist §5/Phase 11).
- Binary reproducibility or apk signature/anchor validity — that is
  the job of the builds and `scripts/apk-release-gate.sh`.
- That the release ran on the tagged commit — the gate caller must
  point `--payload`/`--apk` at the actual artifacts of that run.

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
5. Pre-tag dry run (unchanged from §5 phase-11): build the package from
   the tag commit `T` with the previous pin and record apk + payload
   sha256 in `/data/` — proves the mirror hash and reproducibility at
   the exact tagged source.
6. Record both in the release notes: the source commit the apk was
   built from (the pin, `T`) and the feed commit (`F`).

If a source change is needed after tagging, do NOT edit the tag: make a
new release (bump `PKG_VERSION`/`PKG_RELEASE`) or a follow-up release
commit that the gate will catch (source diff non-empty → gate fails).
