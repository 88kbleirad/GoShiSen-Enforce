# Phase 10.4 — NFQUEUE End-to-End Enforcement Validation (executed)

Date: 2026-11-11. Status: executed on the OpenWrt QEMU lab (pinned
25.12.6 image, kernel 6.12.112).

## Summary

End-to-end LAN→WAN enforcement by the GoShiSen-Enforce daemon is
demonstrated on the lab VM: queue 100 bound, hook at `priority filter +
1`, positive and negative HTTP controls with full counter identity, and
fail-closed behavior across crash, overflow, shutdown, malformed
packets, and no-consumer states. Primary acceptance criterion **PASS**.

## A. Environment

- OpenWrt 25.12.6 r33311-981c978eff, x86/64, kernel 6.12.112.
- QEMU 10.0.13 (TCG), 512 MB, 2 vCPU; disk `/tmp/owrt.img` built by
  `scripts/openwrt-image-prep.sh --download` (image sha256
  `f8538784…` verified against release manifest; kmods checksum-verified).
- Deployed daemon: `v0.2.0-openwrt` (CGO-disabled static, sha256
  `b3a18e6…`). **Note:** the repository's `bin/goshisen` is a glibc
  dynamic build (`interpreter /lib64/ld-linux-x86-64.so.2`) and cannot
  run on musl OpenWrt (`not found`); the lab deployments and CI use the
  static build.
- NFQUEUE kmods loaded: `nfnetlink_queue`, `nft_queue`,
  `nf_conntrack_netlink` (free `nft_queue`/`nfnetlink_queue` refcounts).

## B. NFQUEUE

- `cat /proc/net/netfilter/nfnetlink_queue`:
  `100  <daemon-pid> 0 2 65531 0 0 <seq> 1`
  — queue 100 present, bound to the daemon PID (`peer_portid`),
  copy_mode=2 (`NFQNL_COPY_PACKET`), queue_dropped=0,
  queue_user_dropped=0 (except during the overflow test), id_sequence
  advances with queued packets.
- Daemon logs confirm the full bind sequence: `NFQUEUE bound queue=100
  maxlen=1024` after `nftables hook installed`.
- Queue config: BIND → COPY(0xffff, PACKET) → MAXLEN(1024) →
  CONNTRACK flags (`NFQA_CFG_F_CONNTRACK` mask only; never FAIL_OPEN).
  CT glue present in this kmod build (stateful ACCEPT works).

## C. nftables

- fw4 forward chain: `type filter hook forward priority filter; policy
  drop` — `ct state { established, related } accept`, `forward_lan` →
  `accept_to_wan` (LAN→WAN accepted, counter observed), `forward_wan`,
  `handle_reject`; srcnat: `meta nfproto ipv4 masquerade` on eth0.
- GoShiSen table: `table ip goshisen` chain forward
  `type filter hook forward priority filter + 1; policy accept;`
  rules: `ct state … counter`, `meta l4proto { tcp, udp } counter queue
  to 100`.
- No fw4 rules are touched by the daemon; `nft list tables` shows only
  `inet fw4` and `ip goshisen`. NAT preserved across fw4 reload and
  daemon restart. No flow-offload rules configured
  (`nft_flow_offload` module refcount 0; no `flow offload` statements
  in the ruleset).

## D. Enforcement

Positive and negative controls run by `scripts/openwrt-e2e.sh`
(Go wrapper `tests/openwrt_integration/`):

| Control | Policy | Client result | Daemon verdicts | Hook counter | Kernel queue drops |
| --- | --- | --- | --- | --- | --- |
| DROP | deny-lab-http.yaml (1 rule) | curl exit 28, no HTTP | +7 drop | +7 | 0 |
| ACCEPT | allow-lab-http.yaml (2 rules) | HTTP 200 + `GSH-ALLOW-PROOF` | +13 accept (new +1, established +12) | +13 | 0 |

Identity held: hook delta == sum of verdict deltas; `queue_dropped`
and `queue_user_dropped` stayed 0. `goshisen_backend_errors_total`
absent (zero parse/send errors).

## E. Fail-closed

- **Malformed packets**: crafted IPv4 with total_len=20 and no L4
  header, checksum-valid, sent from the LAN client. fw4's own
  `accept_to_wan … ct state invalid drop` counter grew +10 (the kernel
  conntrack rejects the truncated L4 before priority 1) — a pre-hook
  guard, not a bypass. The daemon's own malformed path is pinned by
  unit tests (`TestEvaluateVerdicts` "parse error drops" → DROP +
  ParseErrors; `TestParseNeverPanicsOnRandomInput`; parser rejects
  truncated TCP/UDP). No parse errors observed in the lab queue.
- **Queue overflow**: daemon SIGSTOP'd, UDP flood (20100 datagrams)
  → `queue_user_dropped` 0→280, `id_sequence` →421, queue_total 116
  in-flight; daemon resumed, logged
  `kernel queue dropped packets before userspace (fail-closed)
  user_dropped=280`, metric `goshisen_queue_overflow_total{cause="user_drop"} 280`.
  Traffic remained enforced after resume (HTTP 200 under allow policy,
  exit 28 under default DROP).
- **Unexpected termination**: SIGKILL of daemon → hook retained in
  kernel, queue consumer gone; curl exit 28, server logged no request
  (fail-closed). No queue stats line while unbound.
- **Restart/recovery**: daemon restarted → hook reinstalled (no
  duplicate tables), queue rebound to new PID, policy recovered from
  the config file, HTTP 200 flows again.
- **Intentional shutdown**: `SIGTERM` → log `shutdown signal received,
  stopping queue`, `queue stopped; hook left in place (fail-closed)`;
  hook remains installed, traffic dropped while daemon stopped.
- **Rule cleanup**: `goshisen hook cleanup` removes only `table ip
  goshisen`; fw4 intact; daemon untouched.

## F. Regression

- `go test ./...` (unit + package + openwrt wrapper): PASS, 0 skips
  for the OpenWrt e2e.
- `go vet ./...`: PASS.
- `go test ./tests/package/ -run TestOpenWrt*`: 4 PASS (new init/UCI
  surface tests).
- Privileged host suite (`tests/integration`, 21 tests): environment-
  gated — host kernel 6.18.60-instabox has no `nft` tool and no
  nf_tables modules; suite skipped with explicit reason (BLOCKED in
  this sandbox; it passed in CI on the phase 10.3 head). The OpenWrt
  equivalents of every scenario (DROP/ACCEPT, default-drop, no-
  listener, shutdown cleanup, hook-cleanup scope, overflow, crash/
  restart) were exercised on the VM in this session.

## G. Phase 10.5–10.11 progress

- **10.5 procd service**: packaged init script fixed (see below),
  validated end-to-end under procd (enable via UCI, start, reload via
  SIGHUP, stop; daemon state and enforcement confirmed in each state).
- **10.6 UCI**: `/etc/config/goshisen` validated; default interface
  corrected to a kernel netdev (`br-lan`).
- **10.7 package build**: built with the official OpenWrt SDK
  (25.12.6, gcc-14.3.0, x86-64): `package/feeds/goshisen/compile`
  produces `goshisen-0.2.0-r1.apk` (3.26 MB). The Makefile pin was
  updated to the current head (the earlier pin predated the nft
  queue-syntax fix, and the pushed repo does not yet contain the new
  commits, so the SDK build used a verified local git-archive with
  `PKG_HASH`). Dependencies `golang/host` and `nftables` resolved via
  the standard feeds. `.apk` payload verified by extraction: fixed
  init script (config_load, procd_send_signal, service_enabled gate),
  UCI default `br-lan`, packaged fail-closed policy, static daemon
  binary.
- **10.8 security regression**: covered by the unit tests above plus
  the privileged suite (environment-gated here).
- **10.9 integration tests**: OpenWrt e2e (Go wrapper) PASS in this
  session; CI job exists and was green on phase 10.3 head.
- **10.10 performance/resource**: daemon VmRSS ≈10 MB, 8 threads on
  512 MB VM; enforcement-path HTTP latency avg 8.9 ms (n=20, min 4.2
  ms, max 18.4 ms); verdict-latency histogram 1215 packets all ≤10 ms
  (713 ≤1 ms).
- **10.11 LuCI proposal**: `docs/design/luci-proposal.md` written
  (architecture only — no LuCI code in this repo).

## Fixes made during this phase

1. `openwrt/files/etc/init.d/goshisen`: added `config_load goshisen`
   before `config_get` (rc.common does not load UCI sections
   automatically; without it the rendered config was empty and the
   daemon exited). Also fixed `reload_service` to use
   `procd_send_signal goshisen "*" SIGHUP` (the previous
   `procd_kill` deletes the procd instance).
2. `openwrt/files/etc/config/goshisen`: default `interface` changed
   from the fw4 zone `lan` to the kernel netdev `br-lan` (host
   validation rejects zone names).
3. `openwrt/Makefile`: `PKG_SOURCE_VERSION` updated from `1fa98bf`
   (pre-queue-syntax-fix) to the current head.
4. `tests/package/openwrt_init_test.go`: new tests pinning the three
   packaging fixes, `docs/design/luci-proposal.md` added.

## Rollback notes

Lab-only changes: `/usr/sbin/goshisen` replaced on the VM (original
glibc build was non-functional on musl; static build matches the CI
artifact), `interface` UCI override to `br-lan`, host HTTP servers
stopped. The default packaged fail-closed policy was restored after
testing (`version: 1, default_action: DROP, rules: []`). All lab
changes are confined to the disposable QEMU VM and its host TAP
bridge; no host firewall rules were touched.
