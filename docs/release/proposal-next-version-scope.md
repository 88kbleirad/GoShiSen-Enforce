# Proposal — v0.2.4 scope (after v0.2.3)

**Status: PROPOSAL — no implementation.** Date: 2026-11-15.
Base: main after v0.2.3 publish. Investigation results from
`docs/release/issue-5-investigation.md` (2026-11-15) are folded in.

## Context

v0.2.3 (2026-11-15) shipped the production-signed OpenWrt apk
(`0.2.3-r1`) and signed CLI archives. Post-publish verification pass.
This proposal records the v0.2.4 scope without implementing it.

## Planned scope for v0.2.4

1. **Package pin fix (issue #9 / #49):** pin `PKG_SOURCE_VERSION` to the
   exact release tag commit (create the tag first, then pin to its SHA);
   make `PKG_VERSION`/apk release agree with the tag (e.g.
   `0.2.4-r1` from `v0.2.4`); add a release-gate check that fails if the
   pin ≠ tag commit, the package version ≠ tag name, or the extracted
   payload `--version` ≠ `PKG_VERSION`. (This is the concrete fix for the
   v0.2.3 apk pin mismatch documented in #49.)
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
3. **Immutable-releases checklist (issue #44):** enforce the post-release
   checklist in CI (already partially in `release-checklist.md`); add a
   workflow gate that verifies published release assets are not mutated.
4. **Other architectures (issue #7):** add runtime verification for
   further OpenWrt targets beyond x86_64/aarch64 (e.g. armv7, mips) in
   CI when runners exist; hardware arm64 remains optional.

## Out of scope for v0.2.4

- **LuCI (issue #8)** — separate feature; depends on offload health flag.
- **Hardware arm64 runner** — optional; QEMU TCG is the CI/lab baseline.
- Engine-side stateful tracking / ignoring ctInfo for IPv6 (option 4 in
  the investigation) — rejected (bypass risk).

## Guardrails (unchanged)

- No version bump/tag without a release authorization; draft-only.
- Mirror hash must be derived from an official SDK container; document
  any toolchain drift.
- Fail-closed invariants remain: no `--allow-untrusted` in production
  flows, no NFQA_CFG_F_FAIL_OPEN, INVALID → DROP.
