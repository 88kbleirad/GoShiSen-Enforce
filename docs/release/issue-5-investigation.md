# Issue #5 — Stateful IPv6 investigation (2026-11-15)

Status: **investigation complete, recommendation issued; no production
code change in this PR** (docs + captured-packet fixture test only).

## 1. Verdict in one sentence

On the verified kernel, **NFQA_CT_INFO is delivered for IPv6 queue 101
and stateful IPv6 ACCEPT completes end-to-end** when the policy uses the
canonical bidirectional `ESTABLISHED/RELATED` rule; the earlier issue #5
claim ("kernel does not deliver conntrack attributes for IPv6, so
stateful IPv6 cannot complete") is **not reproducible on this kernel**
and was, in the original phase-12 test, a **policy-rule-direction
artifact** (direction-specific established rule that dropped the
original-direction established ACK/data).

## 2. Environment (this investigation)

| Item | Value |
| --- | --- |
| VM | OpenWrt 25.12.6 x86/64, kernel **6.12.112** (`uname -r`) |
| QEMU | 10.0.13 TCG, dual-tap LAN/WAN namespaces |
| Package | `goshisen-0.2.3-r1.apk` (production-signed; anchor verified, `apk verify` OK) |
| Daemon binary | sha256 `57aef2cf558d80ed8d51c9e7f746021e101cde7a9427fc6432eaab69735b89c0`, `--version` → `goshisen 0.2.3` |
| Modules | `nf_conntrack` (with `nf_defrag_ipv4`, `nf_defrag_ipv6`), `nfnetlink_queue`, `nf_conntrack_netlink`, `nf_tables` — all loaded |
| Hook | `inet goshisen` forward, `meta nfproto ipv6 ... queue to 101` |
| Topology | LAN ns `fdcd:ea48:12a4::2/64` → guest → WAN ns `fd00::2/64` (marker :8089 v6, :8088 v4) |

## 3. Raw kernel evidence (nfq-probe on queue 101)

A minimal NFQUEUE probe (same netlink config sequence as the daemon,
including `NFQA_CFG_F_CONNTRACK`) bound queue 101 (AF_INET6) and printed
each packet's attributes. Captured v6 TCP request/response:

```
pkt id=1 ctInfo=2 ctAttr=- payload=v6 proto=6 fdcd:ea48:12a4::2:46160->fd00::2:8089
pkt id=2 ctInfo=3 ctAttr=- payload=v6 proto=6 fd00::2:8089->fdcd:ea48:12a4::2:46160
pkt id=3 ctInfo=0 ctAttr=- payload=v6 proto=6 fdcd:ea48:12a4::2:46160->fd00::2:8089
...
```

- **ctInfo=2** = `IP_CT_NEW`, original direction (SYN)
- **ctInfo=3** = `IP_CT_IS_REPLY` (base 0 = ESTABLISHED), reply (SYN-ACK/data)
- **ctInfo=0** = `IP_CT_ESTABLISHED`, original direction (ACK/data)
- **ctAttr absent** (`NFQA_CT` not delivered) for **both IPv4 and IPv6** —
  the daemon already handles this (TCPState -1, state from ctInfo only).

When the probe accepted every packet, the full v6 TCP flow completed
(**HTTP 200**, marker body), proving the kernel and NFQUEUE deliver
enough conntrack metadata for stateful IPv6.

## 4. Answers to the 3b questions (with evidence)

| Question | Answer | Evidence |
| --- | --- | --- |
| Are NFQA_CT / NFQA_CT_INFO delivered for IPv6 queue 101? | **CT_INFO yes; CT no** (same for IPv4). | probe log: ctInfo present on every packet, ctAttr absent on all |
| Is NFQA_CFG_F_CONNTRACK set on that socket? | yes — same 4-message config sequence as daemon (`BIND`, copy, maxlen, `F_CONNTRACK` flags) | probe source; daemon `configMessages` |
| Are nf_conntrack + IPv6 conntrack loaded? | yes — `nf_conntrack` with `nf_defrag_ipv6` | `lsmod` on guest |
| Is the IPv6 tuple key direction-normalized correctly? | yes — engine `newTupleKey` swaps src/dst on `DirReply`; captured packets replay correctly | `TestStatefulV6CanonicalPolicyFromCapturedPackets` (PASS) |
| Is the packet INVALID before the queue? | no — every packet carries valid ctInfo (2/3/0) | probe log |
| Does the daemon's verdict/mark cause INVALID later? | no — INVALID is only assigned when `NFQA_CT_INFO` is absent; here it is present | `applyCTInfo`; probe |
| Stateful v6 completes with the real daemon? | **yes, HTTP 200 + marker body** (canonical policy) | live curl + conntrack TIME_WAIT, 5+5 packets |

## 5. What actually failed before (root cause of the phase-12 report)

The phase-12 stateful policy used **direction-specific** established
rules (e.g. reply-only `src=fd00::/64 dst=fdcd::/64`). The client's
established **original-direction** ACK/data (ctInfo=0) matched no rule
and hit `default_action: DROP`. The daemon accepted the SYN and the
SYN-ACK reply, but the handshake never completed because the client ACK
was dropped. Conntrack stayed `SYN_SENT [UNREPLIED]`-adjacent
(ESTABLISHED with 1 reply) — the same signature the phase-12 report
attributed to "no IPv6 CT attributes".

The canonical pattern (`allow-established` with `state:
[ESTABLISHED, RELATED]`, no src/dst — matching any direction for
accepted connections) resolves it. This is documented in
`docs/design/architecture.md` §4 as the recommended pattern.

## 6. Kernel matrix

| Kernel | Stateful v6 tested | Result |
| --- | --- | --- |
| OpenWrt 25.12.6 x86/64 (6.12.112) | yes (this investigation) | **PASS** — completes end-to-end |
| OpenWrt 25.12.6 armsr/armv8 (6.12.112 arm64) | no | **UNVERIFIED** (prompt-32 ran DROP/ACCEPT controls, not stateful v6) |
| Other kernels | no | **UNVERIFIED** |

The original issue #5 premise (kernel CT-attribute delivery gap for
IPv6) remains **unverified on any kernel**; the only verified kernel
delivers CT_INFO fine. The arm64 kernel and any older/different kernels
are untested.

## 7. Options (cost/risk/fail-closed)

| Option | Fail-closed? | Packaged default change? | Tests that prove it | User impact |
| --- | --- | --- | --- | --- |
| **(1) Canonical-policy documentation + fixture test (chosen)** | yes — unchanged invariant | no — docs + regression test only | captured-packet unit test; live v6 HTTP 200 | none; closes the misdiagnosis; users get a working stateful-v6 example |
| (2) Kernel matrix / CI v6 e2e job | yes | no | CI openwrt e2e adds stateful-v6 ACCEPT step | CI cost + a small e2e addition |
| (3) Refuse IPv6 stateful mode at startup | yes (fail-closed) | yes — config flag | config-reject unit tests | breaks users who correctly use stateful v6 on CT-capable kernels |
| (4) Engine-side stateful tracking (ignore ctInfo for v6) | **risk** — could accept spoofed/unverified flows | yes | extensive engine tests | higher complexity, bypass risk; NOT recommended |

## 8. Recommendation

**(1) + (2):** keep the daemon as-is (it behaves correctly when the
policy follows the documented pattern), add the captured-packet
regression fixture (this PR), document the finding, and add a stateful
IPv6 ACCEPT step to the OpenWrt e2e CI job (v0.2.4 backlog) to prevent
regression and to cover the arm64 kernel in CI when a runner is
available. No production code change required.

## 9. Limits / unverified

- Only one kernel verified (6.12.112 x86/64); arm64 and others untested.
- The probe accepted all packets; the daemon's accepted-set semantics
  were verified via the live HTTP 200 run and the unit fixture.
- `NFQA_CT` (full tuple blob, incl. TCP state) is absent on both
  families — the daemon's `TCPState` is -1 for all captured packets;
  this is pre-existing behavior, not a regression.
- Production behavior on kernels that genuinely lack IPv6 CT-INFO
  delivery (older nfnetlink_queue builds) remains fail-closed (INVALID →
  DROP) and is untested here.
