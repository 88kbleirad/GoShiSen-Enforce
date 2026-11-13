# Security Policy

## Project status

GoShiSen-Enforce is a lab firewall under active development (Phase 7 — hardening/release). It implements policy enforcement over NFQUEUE with kernel-side conntrack state and nftables hooks, a localhost REST management API, and bounded-cardinality Prometheus metrics. It is **not yet officially released**; treat it as not-yet-suitable for protecting real production networks until the first tagged release. Tests requiring root (privileged integration suite) are gated and never falsely reported as passing.

## Reporting a vulnerability

Report suspected vulnerabilities privately to the maintainer, **not** via a public issue:

- Open a [private security advisory](https://github.com/88kbleirad/GoShiSen-Enforce/security/advisories/new) on GitHub, or
- contact the maintainer listed in `LICENSE` / the repository owner.

Include: affected version or commit, a description, reproduction steps, and the impact you believe it has. We aim to acknowledge reports within a few days.

## Scope notes for reviewers

- The daemon runs as **root** (or with `CAP_NET_ADMIN`/`CAP_NET_RAW`) inside a dedicated network namespace; it modifies only the app-owned nftables table `inet goshisen` in the current namespace and never touches the host firewall.
- Fail-closed behavior is a core property: a stopped, crashed, or overloaded engine (queue overflow) must result in dropped traffic, never bypassed traffic. The backend never sets `NFQA_CFG_F_FAIL_OPEN`.
- The API binds to loopback by default; administrative mutations require a bearer token that is never accepted via argv, never logged, and redacted in `config show`.
- Metric labels are bounded enums; no IPs, ports, or flow IDs appear as labels.
- Reports about the design (not only the code) are welcome against `docs/design/architecture.md`.

## Threat model (router deployment)

This section states what the daemon enforces and where residual gaps are,
based on the OpenWrt lab validation (25.12.6, QEMU).

### Enforced

- **LAN→WAN IPv4/IPv6 TCP/UDP forwarded traffic** is queued (IPv4
  queue `100`, IPv6 queue `101`) and decided by policy before it is
  forwarded. A DROP verdict drops the packet; ACCEPT lets firewall4
  continue.
- **Fail-closed in every state**: daemon stopped, crashed, crash-
  looping, queue overflow, malformed packet, or missing conntrack
  attributes all result in DROP (never ACCEPT). `NFQA_CFG_F_FAIL_OPEN`
  is never set; the nft queue rule never uses `bypass`.
- **Router-local traffic** (SSH to the router, LuCI, DNS, DHCP) never
  traverses the forward hook, so management stays reachable even when
  forwarding is fully blocked.

### What an attacker on the LAN can do

- Send traffic toward WAN that the policy does not explicitly allow: it
  is dropped (default action DROP) — including IPv6, which is queued
  like IPv4.
- Saturate the queue (queue overflow): the kernel drops the excess and
  the daemon records `goshisen_queue_overflow_total`; enforcement keeps
  failing closed.
- Send malformed/truncated packets: they are parsed defensively (never
  panic; fuzzed), recorded as parse errors, and dropped.

### What an attacker on the WAN can do

- Send unsolicited inbound TCP/UDP: firewall4's established/related
  rules and the daemon's policy drop them (no hidden ACCEPT).
- Attempt to evade the policy via IPv6: IPv6 is queued on a dedicated
  queue; no family bypass exists.

### Residual gaps

- **Flow offloading**: software/hardware flow offloading (fw4
  `flow_offloading` or `flow_offloading_hw`) makes established flows
  skip nftables entirely. The daemon detects offloading and emits a
  high-severity WARN plus `goshisen_flow_offload_active 1`, re-checked
  every 10 s, but never disables it. `offload_policy` lets the operator
  make the gap non-silent: `refuse` aborts startup before the hook is
  installed (fail-closed by absence, system untouched), `readiness`
  keeps enforcing but keeps `/readyz` at 503 while offloading is
  active. `warn` (default) only reports. Disable offloading on routers
  that rely on GoShiSen enforcement.
- **Stateful IPv6**: on kernels whose `nfnetlink_queue` does not
  deliver conntrack attributes for IPv6, IPv6 packets arrive as INVALID
  and are dropped (fail-closed). Stateless IPv6 rules still work.
- **Boot window**: before the daemon starts (START=90, after firewall),
  only firewall4's own policy applies. During startup the hook is
  installed before the queue binds, so that sub-window is fail-closed.
- **SIGTERM / service stop**: the hook is intentionally left in place
  (fail-closed), so a stopped service blocks forwarding until the hook
  is removed (`goshisen hook cleanup`) or the service restarts. This is
  by design; see `docs/openwrt.md#recovery` for the exact recovery
  commands.
- **uninstall**: `apk del goshisen` runs `hook cleanup` (prerm), so
  removal does not leave an orphaned queue rule that blocks forwarding.

### Security invariants (pinned by tests)

- `NFQA_CFG_F_FAIL_OPEN` never requested.
- Parser never panics on arbitrary input (`FuzzParse`, `FuzzParseAttrs`).
- The hook touches only the app-owned `inet goshisen` table; firewall4
  and other tables are never listed, flushed, or modified.
- Metric label cardinality bounded; no addresses or flow IDs as labels.

## Supported versions

None yet: no releases have been made. Once the first tagged release exists, this section will list supported versions.
