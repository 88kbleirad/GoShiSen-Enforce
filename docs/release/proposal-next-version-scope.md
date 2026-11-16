# Proposal — v0.2.5 scope (after v0.2.4)

**Status: IMPLEMENTATION IN PROGRESS.** Date: 2026-11-16.
Base: main after v0.2.4 publish (tag 9492da1; signed apk 0.2.4-r1
sha256 8ec8ff6c…; anchor fp 542bb7ae…; immutable releases enabled).

## Context

v0.2.4 (2026-11-16) shipped the production-signed OpenWrt apk
(`0.2.4-r1`) and signed CLI archives; post-publish verification pass
(`docs/release/v0.2.4-postpublish-evidence.md`). This proposal records the
v0.2.5 scope and its implementation status.

## Planned scope for v0.2.5

1. **Upgrade hook fix (issue #57):** after `apk upgrade` from
   v0.2.3 to v0.2.4 the new daemon binary only took effect after a
   manual restart — the post-upgrade hook hit the existing nft chain
   while the old process ran. Reproduced on a fresh pinned OpenWrt
   25.12.6 VM (0/43 deny probes passed during upgrade — no fail-open
   window, but version skew). Fix: controlled restart in postinst on
   `PKG_UPGRADE=1` (keeps hook/boot guard in place; never stop-then-
   start), `goshisen-boot start` idempotent (never re-add/replace an
   existing app-owned chain).
   - **DONE:** fix in `openwrt/Makefile` postinst + `files/etc/init.d/
     goshisen-boot`; contract tests (`tests/package/
     openwrt_upgrade_contract_test.go`); repro doc `docs/release/
     v0.2.4-upgrade-repro.md`; branch `fix/apk-upgrade-restart`
     (PR pending; fix proof in progress: clean SDK build + fresh-VM
     downgrade→upgrade run with continuous deny probes).
2. **Release-apk evidence gaps (v0.2.4):**
   - queue 100 **and** 101 bound + stateful-IPv6 scenarios re-run on
     the release apk itself (fresh VMs, anchor installed, no
     `--allow-untrusted`),
   - tampered/unsigned/anchor-removed negative controls ON a VM
     (v0.2.4 ran them host-side only),
   - two clean SDK builds of the v0.2.4 pin (17d1ad9d tree / tag
     9492da1 source) with matching hashes vs published pre-signing
     `eb975a19…`.
   - **In progress** — evidence doc `docs/release/
     v0.2.4-supplementary-evidence.md` (PR pending).
3. **Signed tags (follow-up to issue #44):** tags are annotated but
   unsigned (`git tag -v v0.2.4` → `no signature found`). Plan written:
   options (GPG / SSH `allowed_signers` / sigstore), maintainer
   requirements, user verification docs, release-gate check, v0.2.5
   migration. Key/signing decisions are maintainer-only.
   - **DONE (plan):** `docs/release/signed-tags-plan.md` (PR pending).
     Implementation = maintainer action at release time.

## Remaining for the v0.2.5 release run (maintainer)

A release run requires, in order (none performed here):

1. **Merge** the PRs (upgrade fix, supplementary evidence, signed-tags
   plan, scope update).
2. **Approve + create** the signed tag `v0.2.5` at the source head
   (signing method per `signed-tags-plan.md`), then a Makefile-only
   follow-up commit pinning `PKG_SOURCE_VERSION` to that tag commit.
3. **Run the pin gate** against the release build
   (`scripts/release-pin-gate.sh`) + the tag-signature check (`git tag
   -v v0.2.5`); both must PASS.
4. **Signing environment check:** `GH_RELEASE_APK_SIGNING_KEY`
   provisioned + approved anchor/fingerprint present (production APK
   signing is BLOCKED without it; the agent never provisions secrets).
5. **CI green** on the tag: build, package, verify-signatures,
   integration (no SKIPs), openwrt e2e (slirp + v6), arm64 cross-build.
6. **Publish** the release + assets (maintainer only; immutable releases
   #44 gate active).
7. **Post-publish evidence:** tag signature verification on a fresh
   clone, apk smoke incl. queues 100/101 (release apk), negative
   controls on VM, upgrade run (v0.2.4 → v0.2.5) with continuous deny
   probes and `/proc/<pid>/exe` hash check after restart.

## Tracking references

- Issue #7 (other architectures): **pending** — runtime verification
  beyond x86_64/aarch64 (e.g. armv7, mips) when runners exist; hardware
  arm64 optional. Not scheduled for v0.2.5.
- Issue #8 (LuCI): **explicitly out of scope** — separate feature;
  depends on offload health flag surfacing (issue #6).
- Issue #12 (tracking): updated with this scope (upgrade fix, apk
  evidence, signed tags).

## Boundaries

Publishing, issue closing, and repository/environment settings changes
require **explicit per-run maintainer authorization**. The agent never
creates/moves/deletes tags, never signs/publishes releases, never
dispatches the apk-sign workflow, never touches secrets/signing keys/
the APK anchor, never force-pushes, and never merges PRs.

## Guardrails (unchanged)

- No version bump/tag without a release authorization; draft-only.
- Mirror hash must be derived from an official SDK container; document
  any toolchain drift.
- Fail-closed invariants remain: no `--allow-untrusted` in production
  flows, no NFQA_CFG_F_FAIL_OPEN, INVALID → DROP.
