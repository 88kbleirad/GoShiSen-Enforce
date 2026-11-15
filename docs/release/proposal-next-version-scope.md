# Proposal — next version scope (after v0.2.3)

**Status: PROPOSAL — no implementation.** Date: 2026-11-15.
Base: main after v0.2.3 publish.

## Context

v0.2.3 (2026-11-15) shipped the production-signed OpenWrt apk
(`0.2.3-r1`) and signed CLI archives. Post-publish verification pass.
This proposal records the next-version scope without implementing it.

## Suggested scope (any subset; owner decides)

1. **Stateful IPv6 ACCEPT** (issue #5): investigate kernel
   nfnetlink_queue CT-attribute delivery for IPv6 (the v6 queue
   currently classifies replies INVALID without CT attrs → fail-closed
   DROP). Options: document stateless ACCEPT as the supported v6 mode,
   probe `NFQA_CFG_F_GSO`/conntrack flag differences, or follow the
   kernel CT glue changes. No release-blocking gap (fail-closed holds).
2. **Other architectures** (issue #7): add runtime verification for
   additional OpenWrt targets beyond x86_64/aarch64 (e.g. armv7,
   mips) in the CI matrix.
3. **LuCI** (issue #8): implement the LuCI frontend from the existing
   design doc (package `luci-app-goshisen`).
4. **Package pin policy** (issue #9): decide whether to re-pin to the
   v0.2.3 tag commit (with a new mirror hash) or keep the
   self-reference-free pin; document the choice in the next release
   notes.
5. **Release hygiene** (issue #44): immutable releases + post-release
   checklist CI enforcement.
6. **Docs** (issue #45): fold the IPv6 queue-101 stateful-v6 ACCEPT
   limitation into docs/openwrt.md.

## Guardrails (unchanged)

- No version bump/tag without a release authorization; draft-only.
- Mirror hash must be derived from an official SDK container; document
  any toolchain drift.
- Fail-closed invariants remain: no `--allow-untrusted` in production
  flows, no NFQA_CFG_F_FAIL_OPEN, INVALID → DROP.
