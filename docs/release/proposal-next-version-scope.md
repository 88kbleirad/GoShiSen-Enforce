# Proposal — v0.2.4 scope (after v0.2.3)

**Status: IMPLEMENTATION IN PROGRESS.** Date: 2026-11-15.
Base: main after v0.2.3 publish. Investigation results from
`docs/release/issue-5-investigation.md` (2026-11-15) are folded in.

## Context

v0.2.3 (2026-11-15) shipped the production-signed OpenWrt apk
(`0.2.3-r1`) and signed CLI archives. Post-publish verification pass.
This proposal records the v0.2.4 scope and its implementation status.

## Planned scope for v0.2.4

1. **Package pin fix (issue #9 / #49):** pin `PKG_SOURCE_VERSION` to the
   exact release tag commit (create the tag first, then pin to its SHA);
   make `PKG_VERSION`/apk release agree with the tag (e.g.
   `0.2.4-r1` from `v0.2.4`); the release-gate check that fails if the
   pin ≠ tag commit, the package version ≠ tag name, or the extracted
   payload `--version` ≠ `PKG_VERSION`. (This is the concrete fix for the
   v0.2.3 apk pin mismatch documented in #49.)
   - **DONE:** `scripts/release-pin-gate.sh` + unit tests (matching,
     source-diff, version-mismatch, missing-pin, apk-filename) + release
     procedure docs (`docs/release/release-pin-gate.md`) + checklist
     item. PR #51. The gate proves: PKG_VERSION == release version, pin
     ancestor of tag, empty runtime diff over cmd/internal/pkg/go.mod/
     go.sum/openwrt/files, payload `--version` == PKG_VERSION, apk
     filename == PKG_VERSION-r<PKG_RELEASE>. It does NOT prove the pin
     IS the tag (impossible); the release procedure pins the tag commit
     after tagging via a Makefile-only follow-up commit.
2. **Stateful IPv6 ACCEPT (issue #5):** investigation complete —
   **PASS on kernel 6.12.112** (CT-INFO delivered; canonical policy
   completes the flow). v0.2.4 work items:
   - keep the documented canonical rule pattern when
     `docs/release/issue-5-investigation.md` (already merged),
   - add a **stateful IPv6 ACCEPT step to the OpenWrt e2e CI job**
     (paired IPv4 control + positive IPv6 HTTP 200 test; asserts the
     accepted-set and the bidirectional established rule),
   - update `docs/openwrt.md` to state stateful IPv6 works with the
     canonical pattern (replacing the blanket "stateful v6 blocked"
     wording).
   - **DONE:** `scripts/openwrt-lab-v6.sh` (dual-tap IPv4+IPv6 lab),
     `scripts/openwrt-e2e-v6.sh` (scenarios: DENY blocks, stateful
     ALLOW completes, one-directional blocked v4+v6, ALLOW→DENY switch;
     each with paired v4 control and counter evidence), Go test
     `TestOpenWrtE2EV6`, `e2e-runner.sh` v6 mode, CI job
     `openwrt-integration-v6` (fails on SKIP, records runner kernel).
     Lab run in progress on OpenWrt 25.12.6 x86/64 (kernel 6.12.112).
     Docs corrected: README, `docs/openwrt.md`, `docs/installation.md`,
     `docs/release/v0.2.0-notes.md`, `docs/release/v0.2.3-notes.md`,
     `docs/release-checklist.md`; erratum draft for the v0.2.3 release
     description at `docs/release/erratum-v0.2.3.md` (maintainer pastes
     into the release body).
3. **Immutable-releases checklist (issue #44):** enforce the post-release
   checklist in CI (already partially in `release-checklist.md`); add a
   workflow gate that verifies published release assets are not mutated.
   - **Not started** — remains for the release run.
4. **Other architectures (issue #7):** add runtime verification for
   further OpenWrt targets beyond x86_64/aarch64 (e.g. armv7, mips) in
   CI when runners exist; hardware arm64 remains optional.
   - **Not started** — remains.

## Remaining for the v0.2.4 release run (maintainer)

A release run requires, in order (none performed here):

1. **Merge** the PRs (pin gate #51, v6 e2e, docs corrections).
2. **Approve** tagging: `git tag v0.2.4` at the source head, then a
   Makefile-only follow-up commit pinning `PKG_SOURCE_VERSION` to that
   tag commit.
3. **Run the pin gate** against the release build
   (`scripts/release-pin-gate.sh`); it must PASS.
4. **Signing environment check:** `GH_RELEASE_APK_SIGNING_KEY`
   provisioned + approved anchor/fingerprint present (production APK
   signing is BLOCKED without it; the agent never provisions secrets).
5. **CI green** on the tag: build, package, verify-signatures,
   integration (no SKIPs), openwrt e2e (slirp + v6), arm64 cross-build.
6. **Publish** the release + assets (maintainer only; immutable releases
   #44 gate active).

## Out of scope for v0.2.4

- **LuCI (issue #8)** — separate feature; depends on offload health flag;
  explicitly out of scope.
- **Hardware arm64 runner** — optional; QEMU TCG is the CI/lab baseline.
- Engine-side stateful tracking / ignoring ctInfo for IPv6 (option 4 in
  the investigation) — rejected (bypass risk).

## Guardrails (unchanged)

- No version bump/tag without a release authorization; draft-only.
- Mirror hash must be derived from an official SDK container; document
  any toolchain drift.
- Fail-closed invariants remain: no `--allow-untrusted` in production
  flows, no NFQA_CFG_F_FAIL_OPEN, INVALID → DROP.
