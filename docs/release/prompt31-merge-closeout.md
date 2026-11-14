# Prompt 31 — Merge PR #33 + post-merge verification (OpenWrt package r5)

**Status:** complete. **Date:** 2026-11-14.

## 1. Pre-merge gate

| Item | Value |
| --- | --- |
| PR | https://github.com/88kbleirad/GoShiSen-Enforce/pull/33 |
| State | OPEN → MERGEABLE → CLEAN |
| Head | `b0fecdc1b5342ae14afe252efc872ccbde185f8c` (CI-tested) |
| Checks | 6/6 SUCCESS on both push `43595366862` and PR `43595369104` |
| Repo merge convention | squash (main history is single-parent squash commits) |

## 2. Merge

- Method: `gh pr merge 33 --squash --delete-branch` (repo convention).
- Merge commit: `68ba7abab91673b26d0a4d5dc3183cae53824877` (squash of
  the PR; single parent `9aa1ecf`).
- MergedAt: `2026-11-14T12:49:44Z`; source branch deleted.
- Post-merge main CI: push run `43596714400` on `68ba7ab` →
  **6/6 SUCCESS** (build/vet/test; arm64 cross-build+static ELF;
  package+verify; sign+verify test-key; integration netns; openwrt e2e
  QEMU).

## 3. Post-merge verification (clean checkout)

| Check | Result |
| --- | --- |
| `openwrt/Makefile` pin | `PKG_SOURCE_VERSION=c5de6e8…` (v0.2.2), `PKG_RELEASE=5`, `PKG_MIRROR_HASH=c5017b88…`, `PKG_GOARCH` param |
| Validation report | `docs/release/openwrt-v022-package-validation.md` present |
| `docs/openwrt.md` | r5 status + aarch64 BLOCKED + downgrade caveat documented |
| Evidence index | row 30 present (CI run IDs recorded) |
| `go test ./tests/package -run 'OpenWrt|Lifecycle'` | PASS |
| `make check` | PASS (incl. openwrt_integration; static build ok) |

## 4. Limits preserved (unchanged)

- **aarch64 runtime: BLOCKED (environment)** — armsr QEMU UEFI serial
  issue on the sandbox; build/APK-level aarch64 evidence only.
- **Production APK signing: BLOCKED** — no production APK key in repo;
  lab used `--allow-untrusted` transition exception only.
- No new release/tag/assets; no package-feed publish; no production
  router.

## 5. Rollback plan

- Revert merge commit `68ba7ab` (clean squash revert) restores main to
  `9aa1ecf` (package pin f5f211a, r4).
- Package-side: reinstall baseline r4 apk (`goshisen-0.2.0-r4.apk`,
  released hash `5020b861…`); downgrade procedure in `docs/openwrt.md`.
