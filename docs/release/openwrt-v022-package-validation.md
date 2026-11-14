# OpenWrt package v0.2.2 source-pin validation (Prompt 30)

**Status:** complete. **Date:** 2026-11-14.

## 1. Scope

- Re-pin `openwrt/Makefile` `PKG_SOURCE_VERSION` from
  `f5f211a173dab2934a42e3aa8220ab2d54fa12da` (r4, v0.2.1-era) to
  `c5de6e8ec52a55b84ed16bdb3c9e5cff654f4dc6` (release **v0.2.2**).
- `PKG_VERSION=0.2.0` kept (OpenWrt package version independent of the CLI
  release; repo policy). `PKG_RELEASE` bumped `4` → `5` (version comparator
  monotonic: `0.2.0-r5` > `0.2.0-r4`).
- `PKG_MIRROR_HASH` set to the SDK-verified sha256 of the source tarball
  the SDK itself generates from the pinned commit (`c5017b88…`, see §4).
- Built clean r5 `.apk` for x86_64 and armsr/armv8 (aarch64) targets, ran
  package-manager lifecycle/runtime validation on live OpenWrt QEMU labs.
- No CLI tag/release/assets changed; no production keys touched; no
  package-feed publish; no production router.

## 2. Version mapping

| Package version | Daemon source SHA | CLI release | SDK mirror hash (tar.zst) |
| --- | --- | --- | --- |
| `0.2.0-r4` (baseline) | `f5f211a173dab2934a42e3aa8220ab2d54fa12da` | v0.2.1-era | `f922e983…` |
| `0.2.0-r5` (this PR) | `c5de6e8ec52a55b84ed16bdb3c9e5cff654f4dc6` | v0.2.2 | `c5017b88…` |

The package recipe (Makefile + `files/`) is a **separate input** from the
daemon source pin; it comes from this repo checkout at the PR head. The
daemon binary inside the `.apk` comes **only** from the pin.

## 3. SDK / build environment

| Item | Value |
| --- | --- |
| SDK | OpenWrt SDK 25.12.6 (x86-64 `gcc-14.3.0_musl`; armsr `gcc-14.3.0_musl`) |
| SDK sha256 (x86-64 tarball) | `1864152619092e781fa36a401960c8e9ae42d6a364c44ee5f0b197ca5f3c0fcc` |
| SDK sha256 (armsr tarball) | `cbf3343bc3d6dc6180fc84077b966c13596a79904adaadbfe68409d563b0b596` |
| Host Go | go1.26.9 (SDK golang/host feed), CGO_ENABLED=0 |
| Build targets | `x86_64` (generic) and `aarch64_generic` (armsr/armv8) |
| Feed | `src-link goshisen` → this repo `openwrt/` |

Build commands (clean):

```sh
# x86_64
make package/goshisen/download V=s     # verifies PKG_MIRROR_HASH
make package/goshisen/compile V=s
# aarch64
make package/goshisen/compile V=s PKG_GOARCH=arm64
```

`PKG_GOARCH` maps the SDK target arch to Go GOARCH (amd64 for x86/64,
arm64 for armsr/armv8); the SDK's own gcc toolchain is not used for the Go
build.

## 4. Source-pin / hash verification

1. `PKG_SOURCE_VERSION` = full 40-hex commit SHA of v0.2.2.
2. `PKG_MIRROR_HASH` was **not** hand-computed: the SDK's own download
   path was run with a stale hash; the SDK printed the expected sha256 of
   the tarball it produces from the pin, and that value was pinned:
   `c5017b88fe13f0c3b5295de1fbe75dfa09bdca47abdcba05e6790d89e38b6f34`.
3. A subsequent clean `make package/goshisen/download` succeeds with no
   hash mismatch, and the tarball is byte-identical across SDKs
   (x86-64 and armsr hosts fetched the same pinned tarball).

## 5. Built artifacts

| Target | APK | sha256 | Binary sha256 (from APK) |
| --- | --- | --- | --- |
| x86_64 | `goshisen-0.2.0-r5.apk` | `aa5352bfd5ea766bfe109205ac49c15b6bafc68503d0344ec01adbeac7326eeb` | `ed8087c248dcbf1ebe7381ce9b045550ffe3fd44a79d04201c7643b03312b542` |
| aarch64 | `goshisen-0.2.0-r5.apk` | `ab3d8ca6fea2cec7a97069cd25623b56f44eb406ace7c34b2ef94f6ab2b8072d` | `b8d19dfca6ed3ca192bdee0ef7ce37b21d5fe8d506a00c4810c792e57da4beba` |

- x86_64 binary: `ELF 64-bit LSB executable, x86-64, statically linked`.
- aarch64 binary: `ELF 64-bit LSB executable, ARM aarch64, statically linked`.
- `ldd` → `not a dynamic executable` (static).
- Version stamp: `goshisen 0.2.0` (package version injected via ldflags).
- Feature stamps verified in binary strings: `offload_policy=enforce:
  flow offloading disabled (verified)`, `offload observe`, flowtable hit
  metrics (`goshisen_flowtable_hits_packets`).
- APK payload: `/usr/sbin/goshisen`, `/etc/init.d/goshisen`,
  `/etc/init.d/goshisen-boot`, `/etc/config/goshisen`,
  `/etc/goshisen/policy.yaml`, `/lib/goshisen/goshisen-lifecycle.sh`,
  `lib/apk/packages/goshisen.list`.

### Reproducibility

Two clean SDK builds (x86_64) of the same pin/recipe produced
**byte-identical** APKs (`aa5352bf…` both times) and binaries
(`ed8087c2…`). The aarch64 APK (`ab3d8ca6…`) was built with the same
recipe and SDK version once; its binary is a static aarch64 ELF
(`b8d19dfc…`). Byte-identity across target arches is not claimed (each
arch builds its own binary).

## 6. Validation matrix (live lab, QEMU TCG)

Environment: OpenWrt 25.12.6 (`x86_64` generic ext4; `armsr/armv8`
ext4-combined-efi), kernel 6.12.112, serial console, LAN ns + WAN
marker (10.0.2.2:8088). All installs via `apk` (package manager), not
manual binary placement.

| # | Check | x86_64 | aarch64 |
| --- | --- | --- | --- |
| 1 | Fresh install via `apk add` (pkg 0.2.0-r5, bin sha `ed8087c2…`) | PASS | BUILD-EVIDENCE |
| 2 | Payload files present + executable init scripts | PASS | BUILD-EVIDENCE |
| 3 | Service default-disabled (start refuses, no daemon) | PASS | BUILD-EVIDENCE |
| 4 | Enable/start; `--version` 0.2.0; queues 100+101 bound | PASS | BUILD-EVIDENCE |
| 5 | Rendered UCI config → YAML (boolean allow_bypass) | PASS | BUILD-EVIDENCE |
| 6 | DROP control blocks request (curl exit 28) + drop verdicts (7) | PASS | BUILD-EVIDENCE |
| 7 | ACCEPT control passes (HTTP 200) + established (13)/new (8) | PASS | BUILD-EVIDENCE |
| 8 | Offload `enforce` disables flowtable (FT 1→0, log `verified`) | PASS | BUILD-EVIDENCE |
| 9 | stop → 0 procs, hook retained (2 rules); start/restart re-bind | PASS | BUILD-EVIDENCE |
| 10 | Reload via SIGHUP → `policy published` (source=sighup) | PASS | SKIP |
| 11 | kill -9 → fail-closed; procd respawn (1 proc back) | PASS | SKIP |
| 12 | Reboot: guard S12 → service → table/queue after boot | PASS | SKIP |
| 13 | Upgrade r4→r5: config preserved (uci queue_num=222, enabled=1) | PASS | SKIP |
| 14 | Downgrade r5→r4 (released r4 apk `5020b861…`): guard disable first; binary r4 (`6055cc16…`), config preserved, daemon re-binds queue (222) | PASS | SKIP |
| 15 | Uninstall: binary/rc.d links/app nft table removed | PASS | BUILD-EVIDENCE |

SKIP = exercised on x86_64 lab only.
BUILD-EVIDENCE = aarch64 APK built and verified (static aarch64 ELF,
correct payload, version stamp); the live armsr QEMU runtime lab on the
instabox sandbox was **BLOCKED by environment**: the armsr combined-EFI
image boots to the UEFI setup menu over the serial console and the
menu navigation could not be automated reliably on this sandbox (5+
boot strategies tried: default boot, UEFI pflash, firmware menu
navigation). No fake PASS is claimed for arm64 runtime. The full
behavioral matrix (install/controls/offload/lifecycle/reboot/upgrade/
downgrade/uninstall) is verified on x86_64 with the identical recipe and
binary source pin.

**Downgrade caveat (r5→r4):** the r4 daemon is the v0.2.1-era binary
(pin `f5f211a`); if the machine was running r5, the runtime config
`/var/run/goshisen/config.yaml` may still be the r5-rendered one. The
true released r4 recipe re-renders the config on `start` (no
`allow_bypass` line), so a clean `restart` after downgrade works; if a
stale r5-rendered config is read before re-render, the r4 daemon fails
to parse `allow_bypass` (documented; `rm /var/run/goshisen/config.yaml`
before starting the r4 service is the clean path). The downgrade test
above used the released r4 package (`5020b861…`) and passed with the
clean restart.

Environment-applicable limits:

- QEMU TCG only (no hardware offload); flowtable hit counters are 0 —
  enforce-mode disable is the observable control (nft `flowtable` object
  gone, daemon log `verified`).
- aarch64 runtime: **BLOCKED (environment)** on this sandbox (UEFI
  serial issue, see matrix note). The aarch64 APK/binary are verified
  at build level; runtime behavioral matrix is x86_64 (same recipe/pin).
- IPv6 control in the dual-bridge lab (phase16 verified on r4) was not
  repeated here; the v6 queue (101) bind and the inet table are verified.

## 7. Trust / signing

- Release artifact signing (v0.2.2 CLI tarballs, production anchor
  `keys/goshisen-release.pub.pem`) is unrelated to APK trust (details in
  `docs/release/release-readiness-v0.2.2.md`).
- The OpenWrt package is installed in the lab with
  `apk add --allow-untrusted` (documented transition exception until
  native `usign` APK verification ships). **No production APK signing
  key exists in this repo** — APK production distribution signing is
  **BLOCKED** (reported separately; test-only install is not claimed as
  production signing).

## 8. Rollback plan

- If the r5 package misbehaves: reinstall the baseline r4 artifact
  (`goshisen-0.2.0-r4.apk`) as documented in `docs/openwrt.md`; the
  package prerm/`goshisen-boot disable` procedure applies.
- Repository rollback = revert this PR (clean squash revert); no history
  rewrite, no tag/release/asset changes.

## 9. PR / CI

- Branch: `feat/openwrt-pin-v0.2.2` (this PR).
- PR: https://github.com/88kbleirad/GoShiSen-Enforce/pull/33 (open; not merged).
- CI runs (post-push) tracked in evidence-index row 30.
- Approval gate: merge only after 6/6 CI jobs succeed (repo convention).

## 10. Evidence files

- `docs/release/openwrt-v022-package-validation.md` (this report).
- `docs/release/evidence-index.md` row 30.
- Lab evidence logs (instabox): `/data/prompt30-artifacts/` (`matrix.log`,
  `dg-test.log`, `controls.log`, `arm64.log`).
- SDK build logs: `/data/owrt-sdk` (`make` outputs), local
  `~/.cache/gsh-openwrt-sdk/`.
