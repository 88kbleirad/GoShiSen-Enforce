# Arm64 Runtime Verification — OpenWrt armsr QEMU Lab Evidence (Prompt 17)

**Date:** 2026-11-14 (UTC); lab run 2026-11-13T16:33–17:05Z
**Status:** PASS — real aarch64 runtime execution of the goshisen daemon,
including policy controls, atomic reload, restart, graceful shutdown, and
offload enforce/observe.

---

## 1. Environment metadata

| Item | Value |
| --- | --- |
| Runtime | **aarch64** (verified inside VM: `uname -m` → `aarch64`) |
| Emulation | QEMU TCG (`qemu-system-aarch64` 10.0.13, `-machine virt -cpu cortex-a57`) |
| HW | none — TCG emulation (no hardware offload claim) |
| Image | OpenWrt 25.12.6 `armsr/armv8` generic ext4 combined EFI (sha256-verified against official `sha256sums`: `77a3802b...`) |
| Kernel | 6.12.112 (arm64) |
| NFQUEUE | `kmod-nfnetlink-queue` + `kmod-nft-queue` (6.12.112-r1, from arm64 target kmods) installed; `/proc/net/netfilter/nfnetlink_queue` present |
| Toolchain | Go 1.27.1 (host) → `GOOS=linux GOARCH=arm64 CGO_ENABLED=0` cross-build |
| Binary/static | `ELF 64-bit LSB executable, ARM aarch64, statically linked, stripped` |
| Artifact verified | v0.2.1 `goshisen-linux-arm64.tar.gz` sha256 `2d8ca943...` matches evidence-index; release checksums OK (release predates signing so checksum+provenance record is the trust path) |
| Topology | client ns 10.0.3.2 → host bridge → VM br-lan (10.0.3.1, eth1 tap) → VM forward → eth0 slirp WAN (10.0.2.15) → host marker 10.0.2.2:8088 |

## 2. Smoke (aarch64)

- `/usr/sbin/goshisen --version` → `goshisen v0.2.1` (release binary) and `goshisen dev` (current-head binary).
- `--help` works; service `running`; `{"status":"ready"}`; queues 100+101 bound; log:
  `policy loaded rules=2 default_action=DROP` → `nftables hook installed` → `NFQUEUE bound queue=100`.

## 3. Policy controls (traffic through the queue)

| Test | Setup | Result |
| --- | --- | --- |
| ACCEPT | policy allow `10.0.3.0/24 → 10.0.2.2:8088` NEW; client curl ×4 | `goshisen_packets_total{action="accept",protocol="tcp"} 4`; `by_state{state="new"} 4` |
| DROP | default DROP, no rule; client curl ×5 | `goshisen_packets_total{action="drop",protocol="tcp"} 10` |
| Queue | nft chain counters advance in lockstep (4/4 packets → queue 100) | PASS |

(Reply path via slirp did not complete — NAT return-path limitation of the lab
topology, not a daemon defect; the daemon verdicts are the evidence.)

## 4. Reload / restart

| Test | Result |
| --- | --- |
| Valid reload (DROP→ACCEPT) | `policy published version=3 source=sighup rules=0 default_action=ACCEPT` |
| Invalid reload (default_action BOGUS) | `policy reload rejected; keeping previous policy error="policy: default_action: action \"BOGUS\" must be ACCEPT or DROP"`; **`active_policy_version` stayed 3** |
| Restart with valid policy | service `running`, queue re-bound (new pid) — no torn/partial state |
| Restart with invalid on-disk policy | fails closed (startup validation) — expected, documented |
| Graceful stop | `shutdown signal received, stopping queue` → `queue stopped; hook left in place (fail-closed)`; hook table retained in `nft` |

## 5. Offload enforce/observe (aarch64)

- `flow_offloading=1` → flowtable `inet/fw4/ft` (hook ingress, devices br-lan, counter).
- `offload observe` → `state=enabled confidence=high flowtable inet/fw4/ft packets=0 bytes=0` (TCG zero-hit — recorded, not inferred as disabled).
- `offload_policy=enforce` restart → log `offload_policy=enforce: flow offloading disabled (verified)`; post state `state=disabled confidence=high`, flowtable grep count 0.

## 6. CI/regression

- New `tests/package/arm64_build_test.go`: cross-compile arm64 + static aarch64 ELF assertion (always runs); qemu-user smoke (`--version`, `offload status`) when binfmt available.
- New CI job `arm64` (ubuntu-latest): cross-build, `file` asserts, `go test -run TestArm64`, optional qemu smoke. No secrets, deterministic.

## 7. Limits (explicit)

- QEMU TCG: no hardware offload; zero-hit flowtable counters are environment-limited (same as x86_64 lab).
- No physical arm64 hardware available — evidence is **QEMU emulation**, clearly separated from a hardware claim.
- Release v0.2.1 artifact predates signing; trust via release checksums + evidence-index record.

## 8. Findings during validation

- armsr default OpenWrt has no lan→wan forwarding rule and drops forward at fw4; lab added `forward=ACCEPT` default + forwarding UCI (lab-only, not product change).
- The `allow_bypass` YAML field (prompt 15) is absent from the v0.2.1 release binary; current-head init script renders it — release binary startup requires the older config. The lab used the current-head binary for feature paths and the release binary for start/version/help proof.
