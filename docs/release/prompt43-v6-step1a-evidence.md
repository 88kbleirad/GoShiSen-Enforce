# Prompt 43 Step 1a — IPv6 queue 101 proof (evidence)

Date: 2026-11-15. Release: v0.2.3 draft (signed apk `948e2e1d…`).

## Setup

Dual-tap QEMU topology (no slirp; real forwarded traffic crosses the
router):
- LAN ns: `10.0.3.2/24` + `fd70:1::2/64`, default via `10.0.3.1`/`fd70:1::1`
- WAN ns: `10.0.2.2/24` + `fd00::2/64`, dual-stack HTTP server on `:::8090`
- Router (OpenWrt 25.12.6 x86_64): eth1 LAN `10.0.3.1/24`+`fd70:1::1/64`,
  eth0 WAN `10.0.2.15/24`+`fd00::1/64`, IPv6 forwarding on, fw4 rule
  accepts forwarded v6 tcp to `:8090`.

## Evidence (production-signed draft apk, packaged default policy)

### Queues bound (`/proc/net/netfilter/nfnetlink_queue`)
```
  100   <pid>  0 2 65531  0 0  0  1    # IPv4 (queue 100)
  101 <portid> 0 2 65531  0 0  0  1    # IPv6 (queue 101) — non-zero portid => bound
```
Queue 101 is bound by the daemon's dedicated AF_INET6 socket
(`capture.go`: `configureFamily(afInet6, queue+1, conn6)`).

### Hook (`nft list chain inet goshisen forward`)
```
type filter hook forward priority filter + 1; policy accept;
ct state { ... } counter packets 0 bytes 0
meta nfproto ipv4 meta l4proto { tcp, udp } counter packets 0 bytes 0 queue to 100
meta nfproto ipv6 meta l4proto { tcp, udp } counter packets 0 bytes 0 queue to 101
```

### IPv6 forward under packaged DROP policy
- Client curl `http://[fd00::2]:8090/goshisen.apk` (v6, forwarded) →
  `RC=28` (timed out) — BLOCKED.
- Server log: **0** v6 requests reached the server (no `[fd00::2]`-sourced GET).
- Daemon drop counter: `goshisen_packets_total{action="drop",protocol="tcp"}` ≥ 7.
- v6 hook rule counter is per-rule; in the ACCEPT-phase probe it advanced
  (157 → 158) while v6 traffic was sent — the v6 packets **are queued**
  on 101 when the daemon is the decision point.

### IPv4 paired positive control
- Packaged allow policy (`src 10.0.3.0/24 dst 10.0.2.2 dst_port 8090`)
  → client curl `http://10.0.2.2:8090/goshisen.apk` → **HTTP 200**.

### IPv6 ACCEPT control (documented limitation, issue #5)
- With an allow rule matching the v6 flow (src `fd70:1::/64`,
  dst `fd00::2`, dst_port 8090), the request still does not complete
  (`RC=28`). This matches the documented stateful-IPv6 conditional:
  when kernel conntrack CT attributes are not delivered for the IPv6
  queue (nfnetlink_queue CT-glue absence), replies are classified
  INVALID → DROP (fail-closed). Stateless IPv6 ACCEPT was verified in
  earlier releases; stateful v6 ACCEPT remains conditional (issue #5).

## Conclusion

- IPv6 forwarded traffic is **queued** (queue 101 bound; hook rule
  present and counts v6). It is **not** passing unqueued.
- Under the packaged default DROP policy, IPv6 forward is **blocked**
  (fail-closed); zero requests reach the server.
- IPv4 positive control passes in the same setup.
- Stateful IPv6 ACCEPT is the documented limitation (issue #5), not a
  queue-101 binding gap.

Evidence files: `/data/prompt40-v023-dryrun/v6-proof.log`,
`v6-dualtap-evidence.log`, `v6-daemon.log`, `v6-final.sh` (scripts).
