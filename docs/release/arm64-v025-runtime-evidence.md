# Arm64 runtime verification — v0.2.5 release payload (PASS)

**Date:** 2026-11-17. **Status:** PASS — real aarch64 runtime execution of
the v0.2.5 release payload, including policy controls, SIGHUP reload,
queue binds, and offload enforce. Closes the "arm64 runtime unverified
for v0.2.5" gap (prior arm64 evidence covered v0.2.1/v0.2.2 only).

## 0. What was (and was not) run — plain scope

- **Ran: the public `goshisen-linux-arm64.tar.gz` release asset.**
  Binary sha256 `715ec1c9425965032b2668f1ea78e968203e5b4b6ea5c0ef90a109688de6d8a0`;
  tarball sha256 `cf400ac852b8d5a7135c9f642d65058a775d58b2e5b48b2828dc6a97d3556a26`
  **matches the release's `checksums.txt`** (verified 2026-11-17 against
  the published asset set). This is the PUBLIC v0.2.5 arm64 asset, not a
  local build.
- **Did NOT run: the aarch64 apk.** The published `goshisen-0.2.5-r1.apk`
  is an x86-64 package (payload extracted and `file`-checked: "ELF 64-bit
  LSB executable, x86-64"); no aarch64 apk exists in the v0.2.5 release.
- **Emulation is QEMU aarch64 TCG, not hardware.** No physical arm64
  hardware was used. Flowtable hit counters under TCG are zero-hit, so
  `offload_policy=enforce` disable (the observable control) was verified,
  but **this run proves nothing about real offload behavior on hardware**.
- **Issue #7 (other architectures / hardware runtime) remains OPEN.** This
  run does not resolve it; hardware verification is still required.

## 1. Environment

| Item | Value |
| --- | --- |
| Runtime | **aarch64** (`uname -m` → `aarch64` inside VM) |
| Emulation | QEMU TCG (`qemu-system-aarch64` 10.0.13, `-machine virt,virtualization=on -cpu cortex-a57 -m 1024 -smp 2`, `-bios /usr/share/qemu-efi-aarch64/QEMU_EFI.fd` — auto-boot) |
| Image | OpenWrt 25.12.6 `armsr/armv8` generic ext4 combined EFI (`/data/armsr-lab/openwrt-image.img.gz`) |
| Kernel | 6.12.112 (arm64) |
| Payload | `goshisen-linux-arm64.tar.gz` (v0.2.5 asset), installed to `/usr/sbin/goshisen`, sha256 `715ec1c9425965032b2668f1ea78e968203e5b4b6ea5c0ef90a109688de6d8a0` |
| Kmods | kmod-nfnetlink-queue / kmod-nft-queue / kmod-nf-conntrack-netlink 6.12.112-r1 (armsr target) |
| Topology | dual-tap: LAN ns `gsh_armsr_lan` (10.0.3.2/24, gw 10.0.3.1) → host bridge → guest br-lan (10.0.3.1/24, eth0) → forward → eth1 WAN (10.0.2.15/24, gw 10.0.2.2) → WAN ns `gsh_armsr_wan` (marker 10.0.2.2:8090) |
| Daemon config | `interface: br-lan`, `policy_file: /etc/goshisen/policy.yaml`, `queue_maxlen: 1024`, `log_level: info` |

Note: image's stock UCI had `wan=dhcp` and `lan=192.168.1.1/24`; the lab
set `wan` static `10.0.2.15/24 gw 10.0.2.2` and `lan` `10.0.3.1/24`
(lab-only, not a product change).

## 2. Evidence

| Check | Result |
| --- | --- |
| Smoke | `/usr/sbin/goshisen --version` → `goshisen v0.2.5`; sha256 `715ec1c9...` |
| Daemon start | log: `policy loaded rules=0 default_action=DROP` → `nftables hook installed` → `NFQUEUE bound queue=0` |
| Queue binds | `/proc/net/netfilter/nfnetlink_queue`: queue 0 peer `10318`, queue 1 peer `2319440751` (both non-zero) |
| DROP control | deny policy (rules=0, default DROP); LAN client curl to `10.0.2.2:8090` → exit 28 (timeout); `goshisen_packets_total{action="drop",protocol="tcp"}` increments (5), `by_state{state="new"}` 5 |
| ACCEPT control | allow policy (rules=2: NEW per-service + ESTABLISHED/RELATED); LAN client curl → **HTTP 200**; `goshisen_packets_total{action="accept",protocol="tcp"}` 10, `by_state{state="established"}` 9 |
| SIGHUP reload | `policy reload requested (SIGHUP)` → `policy published version=1 source=sighup rules=2 default_action=DROP` (deny→allow); later version=2 rules=0 (allow→deny) |
| Offload enforce | `flow_offloading=1` + `offload_policy: enforce` restart → log `offload_policy=enforce: flow offloading disabled (verified)`; `nft list ruleset | grep -c flowtable` → 0 after |
| Post-enforce queues | re-bound (peer 10318 / 2319440751) after clean restart (old daemon killed via `kill -9 $(pidof goshisen)`; busybox has no `pkill`) |
| Post-enforce DROP | deny policy reloaded → curl exit 28 (blocked) |

## 3. Limits

- QEMU TCG emulation; no physical arm64 hardware claim. Flowtable hit
  counters are TCG-zero (enforce-disable is the observable control).
- Guest network config was adjusted for the lab topology (stock image
  `wan=dhcp`); not a product change.
- Lab teardown: QEMU process killed, netns `gsh_armsr_lan`/`gsh_armsr_wan`
  removed, host iptables FORWARD rules removed (see teardown section in
  the run log).
