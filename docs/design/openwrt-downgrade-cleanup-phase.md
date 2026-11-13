# OpenWrt Package Downgrade Cleanup — Design (Prompt 18, backlog #5)

**Status:** proposed (implementation follows).
**Applies to:** OpenWrt package `goshisen` lifecycle (`prerm`/`postinst`),
boot-guard rc.d links, downgrade r4→r2 safety. Release v0.2.1/assets/signing key
untouched.

---

## 1. Audit (owner / path / action)

| Path | Owner | Created by | Action on uninstall/downgrade |
| --- | --- | --- | --- |
| `/etc/init.d/goshisen` | r2+r4 (package file) | install | removed by apk (package file) |
| `/etc/init.d/goshisen-boot` | **r4 only** | install | removed by apk on r4→r2 (not in r2) |
| `/etc/rc.d/S90goshisen` | package (runtime) | postinst `goshisen enable` (rc.common) | must be removed by prerm (r2 prerm only `stop`s — **gap**) |
| `/etc/rc.d/K10goshisen` | package (runtime) | same | must be removed by prerm |
| `/etc/rc.d/S12goshisen-boot` | package (runtime) | r4 postinst `goshisen-boot enable` | **must be removed by r4 prerm** — currently guarded by `[ -x /etc/init.d/goshisen-boot ]` which can be false during downgrade → **dangling** |
| `/etc/rc.d/K10goshisen-boot` | package (runtime) | same | same |
| `/etc/config/goshisen`, `/etc/goshisen/policy.yaml`, `/usr/sbin/goshisen` | package | install | removed by apk |

Root cause: r4's `prerm` removes the rc.d links **only if** `/etc/init.d/goshisen-boot` still exists (`-x` guard). During r4→r2 downgrade apk may remove that init file before running `pre-deinstall`, so the rc.d symlinks remain and point at a nonexistent target (dangling). r2's `prerm` also never runs `disable` for `/etc/init.d/goshisen`, so `S90goshisen`/`K10goshisen` can dangle too after an r2 uninstall.

## 2. Cleanup contract

- **Package-owned rc.d links are deterministic** (name derives from init script `START=`/`STOP=`): `S12goshisen-boot`, `K10goshisen-boot`, `S90goshisen`, `K10goshisen`.
- On **any** removal of the package (uninstall or downgrade to r2), `prerm` MUST remove all four links **unconditionally** in `/etc/rc.d/` — never guarded by the existence of the target init script, never following symlinks (`unlink`, not `rm -rf`), never touching any other rc.d entry.
- The init **scripts** are removed by apk itself (they are package files); the links are runtime state and must be cleaned by the script.
- `unlink` failure on a missing link is success (idempotent); failure on a *non-symlink* entry with the same name (e.g. a real file left by another package) is an error → abort with a clear message (no path ownership assumption).
- `hook cleanup` (nftables table removal) stays as today, guarded by `-x` on the binary (only when the daemon binary exists).

## 3. Ordering (fail-safe)

`prerm` order: (1) disable/stop services that exist, (2) **unconditionally unlink rc.d links**, (3) hook cleanup. This ensures even if the init files are already gone, the rc.d state is consistent by the time the package file removal completes. The boot guard is disabled before any binary swap by apk's pre-deinstall running before the new package's files are installed.

## 4. Missing/broken metadata

- If the rc.d link exists but `readlink` fails (broken), `unlink` still removes it.
- If a link points elsewhere (not `../init.d/...`), it is not ours to remove → verify target prefix before unlinking (only `../init.d/goshisen*`).
- If `prerm` cannot verify a path (permissions), fail with an explicit message; do NOT return success (no false success).

## 5. Empirical apk behavior (verified live, OpenWrt 25.12.6 QEMU)

- **apk v3 does NOT run the old package's pre-deinstall on downgrade.** A clean
  `apk add --no-network --allow-untrusted <r2.apk>` over r4 leaves all four rc.d
  links in place while `/etc/init.d/goshisen-boot` is removed → S12/K10
  `goshisen-boot` dangle. The r4 prerm fix therefore cannot fire on downgrade;
  it fires on **uninstall** (verified: `apk del` after fixed-r4 install removes
  all four links, `NO-LINKS`).
- **Pre-downgrade `goshisen-boot disable` is the verified fail-safe**: it
  removes S12/K10 while the target init script still exists; after the downgrade
  no boot-guard links remain (only S90/K10 goshisen, which r2 owns).
- **Post-downgrade rescue**: the `disable` command and the lifecycle lib are both
  gone (r2 does not ship them). The deterministic rescue is
  `unlink /etc/rc.d/S12goshisen-boot /etc/rc.d/K10goshisen-boot`
  (fixed paths, no follow, no recursive deletion). This is documented.

## 6. Tests

Shell-level (repo `tests/package/`): install→links present; upgrade r2→r4→links present; downgrade r4→r2→links gone; uninstall→links gone; repeated cleanup idempotent; missing target; malformed symlink; target-prefix mismatch (foreign rc.d untouched); path traversal guard (no `../../`).

Live OpenWrt QEMU (r2/r4 apks): baseline r2, upgrade, reboot/boot guard, downgrade, reboot, version check, uninstall, inventory evidence — verified as above.

## 7. QEMU/hardware limit

Boot/reboot ordering is observed under QEMU TCG; hardware boot timing is not claimed (same limitation as prior prompts).

## 8. Rollback

PR revert restores prior prerm behavior; cleanup is additive and guarded to package-owned paths. No tags/releases touched.
