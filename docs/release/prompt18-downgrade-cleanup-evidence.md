# OpenWrt Downgrade Cleanup — Live Lab Evidence (Prompt 18, backlog #5)

**Date:** 2026-11-14 (UTC); lab run 2026-11-13T17:16–17:45Z
**Status:** PASS — uninstall cleanup verified; downgrade behavior mapped
empirically; pre-downgrade procedure verified; rescue documented.

---

## 1. Environment

| Item | Value |
| --- | --- |
| Image | OpenWrt 25.12.6 x86/64 (lab image, r4 preinstalled) |
| QEMU | TCG (`qemu-system-x86_64`, lab harness) |
| Packages | `goshisen-0.2.0-r2.apk` (original release), `goshisen-0.2.0-r4.apk` (rebuilt from this branch with the cleanup fix) |
| Interaction | serial console (direct socket; the bundled `openwrt-console.py` truncates output after long `apk` ops — use direct socket with adequate drain) |

## 2. Package lifecycle inventory (manifest-level)

| r2 | r4 |
| --- | --- |
| no `goshisen-boot` init | ships `goshisen-boot` init |
| post-install: `goshisen enable` | post-install: `goshisen enable` + `goshisen-boot enable` |
| pre-deinstall: hook cleanup + `goshisen stop` | pre-deinstall: hook cleanup + stop/disable both + **unconditional rc.d link removal (fix)** |
| no `/lib/goshisen/` | ships `/lib/goshisen/goshisen-lifecycle.sh` |

rc.d links are runtime-created by `rc.common enable` (S12/K10 `goshisen-boot`,
S90/K10 `goshisen`); none are packaged paths.

## 3. Verified behaviors (live)

| Scenario | Result |
| --- | --- |
| Install fixed r4 | 4 rc.d links present; `/lib/goshisen/goshisen-lifecycle.sh` present |
| **Uninstall (apk del)** | hook removed (`table inet goshisen removed`); **all four rc.d links removed (NO-LINKS)**; init scripts + binary gone |
| **Downgrade r4→r2 (clean, --no-network)** | r2 active (`0.2.0-r2`); `/etc/init.d/goshisen-boot` removed by apk; **all four rc.d links remain → S12/K10goshisen-boot DANGLING** |
| apk script ordering | **apk v3 does NOT run the old package's pre-deinstall on downgrade** (links not cleaned by r4's prerm) |
| Pre-downgrade `goshisen-boot disable` | removes S12/K10 while the script exists; after downgrade **no boot-guard links remain** (only S90/K10 goshisen) |
| Post-downgrade rescue | `unlink /etc/rc.d/S12goshisen-boot /etc/rc.d/K10goshisen-boot` (fixed paths) |
| Re-run cleanup | idempotent (absent links are success) |

## 4. Conclusions

- The **uninstall path is fully fixed**: r4's prerm unconditionally removes
  package-owned rc.d links (unlink, target-prefix checked, no recursive
  deletion), verified live.
- The **downgrade path cannot be automated from the removed package's prerm**
  because apk v3 skips the old package's pre-deinstall on downgrade. The
  supported, verified procedure is to disable the boot guard **before**
  downgrading; a fixed-path `unlink` rescue is documented for post-hoc cleanup.
- The lib + script are not present after downgrade (r2 does not ship them), so
  the rescue is a deterministic two-`unlink` command, not a shipped tool.

## 5. Unit/shell tests (repo)

`tests/package/openwrt_downgrade_cleanup_test.go` — 7 lifecycle tests (owned
links removed; idempotent absent; foreign file/symlink left; broken link
removed; traversal target refused; malformed refused) + Makefile surface test
(lib installed, prerm calls cleanup, unlink, no recursive deletion) + docs
procedure assertions.

## 6. Limits

QEMU TCG only; hardware boot timing not claimed. The r2 package is a historical
artifact and cannot be rebuilt with cleanup; behavior for the r4→r2 pair is
therefore documented (disable-before-downgrade), while forward releases inherit
the prerm/uninstall fix and the lifecycle lib.
