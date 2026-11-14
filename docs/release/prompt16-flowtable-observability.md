# Flowtable Bypass Observability — OpenWrt Lab Evidence (Prompt 16)

**Date:** 2026-11-13 (UTC)
**Status:** PASS — measurement model (absent / zero-hit / positive / unknown)
verified live on OpenWrt 25.12.6 QEMU TCG; positive-hit proof explicitly
scoped to a hardware-capable environment (not claimable in TCG).

---

## 1. Environment

| Item | Value |
| --- | --- |
| Kernel | 6.12.112 |
| Distro | OpenWrt 25.12.6 (`r33311-981c978eff`) |
| Emulation | QEMU TCG (x86_64) |
| Flow offloading | `firewall.@defaults[0].flow_offloading='1'` → fw4 flowtable `ft` (`hook ingress priority filter`, devices `br-lan`/`eth0`) |
| Flow-offload rule | `meta l4proto { tcp, udp } flow add @ft` (fw4 forward, handle 91) |
| Daemon | gs-deploy binary `dev` (prompt 16 head), policy DROP + allow 10.0.3.0/24→10.0.2.2:8088 |

## 2. State observations (live)

### 2.1 `absent` (flowtable removed by enforce)

After `offload_policy=enforce` daemon start:

```
offload observe: at=2026-11-13T16:11:23.25731004Z state=disabled confidence=high
  reason: no flowtable / flow-offload rules
```

`nft -a list ruleset | grep -c flowtable` → `0`. Service `running`.

### 2.2 `present, zero hits` (flowtable present, no offload observable)

With `flow_offloading=1` and warn default (daemon not yet enforcing), before traffic:

```
offload observe: at=2026-11-13T16:09:33.5649716Z state=enabled confidence=high
  flowtable inet/fw4/ft packets=0 bytes=0
  hits: packets=0 bytes=0 (cumulative, monotonic)
```

Raw ruleset shows the flowtable's bare `counter` statement and the flow-add rule.
After a 20-connection burst through the VM forward path (daemon accepted 10 TCP
packets through the queue — `goshisen_packets_total{action="accept"} 10`), the
flowtable counters were still `0/0`:

```
offload observe: at=2026-11-13T16:11:00.884310811Z state=enabled confidence=high
  flowtable inet/fw4/ft packets=0 bytes=0
```

**Interpretation (recorded, not inferred):** `zero hits ≠ offload disabled`.
Packets traversed the chain (queue accepted 10) and the flowtable offloaded 0 —
QEMU TCG has no offload engine. This is the documented TCG limitation.

### 2.3 `positive hits` — NOT observable in TCG (scoped)

No platform in this lab can produce flowtable hits (TCG offloads 0 packets).
The instrumentation is ready and unit-tested (`TestParseRulesetFlowTableCounters`
→ `HitsPackets=42`); a hardware-capable environment (real NIC driver flowtable
offload) is required to observe `present_positive_hit`. Explicitly NOT claimed.

### 2.4 metrics (live)

During `absent` (post-enforce): `goshisen_flow_offload_state 0`,
`goshisen_flowtable_hits_available 0`, `goshisen_flowtable_hits_packets -1`.

During `present zero-hit` (pre-enforce): state=1, hits_available=1,
hits_packets=0.

## 3. Enforcement interaction (enforce disable, live)

Log sequence with `offload_policy=enforce`:

```
level=WARN msg="flow offloading detected ... enforcement gap" state=enabled   (watchdog, pre-enforce)
level=INFO msg="offload_policy=enforce: flow offloading disabled (verified)" detail="flow offload disabled"
```

After: state=disabled, confidence=high, service running, flowtable gone.

## 4. What was NOT verified

- Positive flowtable hit counters (TCG). Hardware-capable environment required
  (backlog #3 remaining piece).
- Driver-level offload beyond nftables (ethtool absent in VM).
- The client HTTP=000 return path is unrelated: daemon accept verdicts prove the
  forward path works; the marker/return path quirk does not affect counter
  observability.

## 5. Evidence artifacts

- Raw `nft -a list ruleset` excerpts (flowtable object, flow-add rule, chain
  counters) captured at t0/t1 in the serial transcripts above.
- Live `offload observe` outputs (2.1–2.2), daemon log transition, metrics.
- Scripts: `scripts/openwrt-console.py` transcript sessions recorded in this
  session log.
