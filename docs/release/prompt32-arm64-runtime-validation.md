# Prompt 32 — aarch64 runtime validation of OpenWrt r5 APK (PASS)

**Status:** complete — **PASS** (live aarch64 runtime). **Date:** 2026-11-14.
**Base:** main after PR #33 merge (`d2c2752`); package r5 (daemon v0.2.2,
pin `c5de6e8…`, `PKG_RELEASE=5`).

## 1. Environment

| Item | Value |
| --- | --- |
| Emulation | QEMU 10.0.13 TCG, `-machine virt,virtualization=on -cpu cortex-a57 -m 1024 -smp 2 -no-reboot` |
| Firmware | `-bios /usr/share/qemu-efi-aarch64/QEMU_EFI.fd` (unified UEFI — auto-boots, no interactive menu) |
| Image | OpenWrt 25.12.6 `armsr/armv8` generic ext4 combined EFI (raw sha256 `cedd764d…` (first boot) / `deef134f…` (fresh), gz sha256 `77a3802b…` verified per prompt17) |
| Kernel | 6.12.112 (arm64); `DISTRIB_ARCH='aarch64_generic'` |
| Topology | dual-tap: LAN ns `gsh_armsr_lan` (10.0.3.2/24) — tap `gsh-armsr0` — guest `br-lan` (10.0.3.1/24, eth0) ⇄ eth1 (10.0.2.15/24, gw 10.0.2.2) — tap `gsh-armsr1` — WAN ns `gsh_armsr_wan` (10.0.2.2/24, marker server :8090) |
| Guest NIC mapping | eth0 = LAN tap (br-lan), eth1 = WAN tap (static, no slirp) |
| APK | `goshisen-0.2.0-r5.apk` sha256 `ab3d8ca6fea2cec7a97069cd25623b56f44eb406ace7c34b2ef94f6ab2b8072d` (aarch64 build) |
| Install | `apk add --allow-untrusted` (documented transition exception; NOT production signing) |

**Key boot finding** (resolves the earlier BLOCKED): the armsr
combined-EFI image booted cleanly with `-bios QEMU_EFI.fd` (auto-boot).
The earlier AAVMF pflash route opened the UEFI menu; using the unified
`QEMU_EFI.fd` avoids interactive firmware. Also required: dual-tap
topology (no slirp user-net — its DHCP/ARP was unreliable under this
setup), with the guest NIC order determined empirically (eth0 = LAN tap,
eth1 = WAN tap).

## 2. Runtime evidence (live, on the aarch64 guest)

| Check | Result | Log evidence |
| --- | --- | --- |
| Smoke | `uname -m` → `aarch64`; kernel 6.12.112 | arm32.log [1] |
| Default-disabled | `/etc/init.d/goshisen start` refuses (`service is disabled (goshisen.main.enabled=0)`); `uci get ... enabled` = 0 | [4] |
| Enable/start | service starts; `ps` shows daemon; queues **100 + 101 bound**; hook has **2 queue rules**; config renders `allow_bypass` | [5] |
| Verdict counters | `goshisen_packets_total{action="drop",protocol="tcp"}` increments on DROP | [6]/R2 |
| DROP control | LAN client curl to disallowed port → `curl` exit 28 (blocked); drop verdicts=4 | R2 |
| ACCEPT control | LAN client curl to marker → **HTTP 200**; `state="established"` = 3092, `state="new"` = 5; `policy published version=3 source=sighup` | R3 |
| Reload failure | BOGUS policy → `policy reload rejected; keeping previous policy`; `policy_updates_total{outcome="failure"} 1`; version unchanged | [7] |
| Offload enforce | `offload_policy=enforce` restart → `offload_policy=enforce: flow offloading disabled (verified)` | R4 |
| Lifecycle | stop → hook retained (2 queue rules, fail-closed); start → queue 100 re-bound | R5 |
| Uninstall | binary removed, rc.d links 0, `table inet goshisen` removed | R6/[10] |

Full raw logs: `evidence/prompt32/arm64-runtime-log.txt` (439 lines;
both run DONE markers present).

## 3. Limits (unchanged)

- QEMU TCG emulation; no physical arm64 hardware claim. Flowtable hit
  counters are TCG-zero (recorded as-is; enforce-disable is the
  observable control).
- Production APK signing remains **BLOCKED** (no production key;
  `--allow-untrusted` transition exception used in lab only). This is a
  separate backlog item, not resolved by this task.
- No new tag/release; no package-feed publish; no production router.
- Host `ip_forward`/bridge FORWARD ACCEPT rules were lab-scoped and
  removed in teardown (no QEMU/netns left).
