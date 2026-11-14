# Offload Detect + Disable — Design (Prompt 15, backlog #2)

**Status:** proposed (implementation follows in this branch).
**Applies to:** the daemon's flow-offload handling, CLI `offload` subcommand,
config `offload_policy`, OpenWrt init script, docs. Release v0.2.1 assets are
untouched.

---

## 1. Policy-enforcement contract

GoShiSen-Enforce enforces policy at NFQUEUE time on the router's forward path.
A packet the kernel handles **without** traversing the forward chain and queue
never receives a verdict — that is an enforcement gap.

| Mechanism | Bypasses NFQUEUE? | Observable from Linux? | Handling |
| --- | --- | --- | --- |
| nftables flowtable + `flow offload`/`flow add @` rule | **yes** (established flows skip the chain) | yes — `nft list flowtables`, `nft -a list ruleset` | detect → explicit disable (verified) → fail closed while present |
| flowtable with `offload`/hardware offload flag | **yes** (same fast-path, driver offloads) | yes — flowtable flags (`offload`) | same as above; driver-level bypass beyond nft is *unknown* (below) |
| Driver/platform L4 flow acceleration (ethool features, HW classifier) | possibly (vendor-specific) | not generally via netlink | inventory only; documented as **unknown** limit; cannot be disabled generically |
| bridge/netfilter (`net.bridge.bridge-nf-call-iptables`) | no (packet still crosses our forward chain; may double-evaluate) | yes — sysctl | inventory only; not a bypass |
| Software offload on loopback / veth | no (no flowtable) | n/a | n/a |

**Contract:** packets that *must* be policy-checked are: all forwarded TCP/UDP
with state (NEW/ESTABLISHED/RELATED). When an offload can bypass that, the
daemon must be able to say **exactly** which state it is in: disabled, enabled,
failed (disable attempted but still enabled), or unknown. Enforcement must not
silently continue in a bypass-possible state.

## 2. Detection states

`FlowOffloadReport` grows a state + detail:

- `disabled` — no flowtable objects and no flow-offload rules anywhere in the
  ruleset. Enforcement gap absent.
- `enabled` — at least one flowtable with a hook, or a rule performing
  `flow offload`/`flow add @`. Detail lists where (table/chain/flowtable).
- `unknown` — detection itself failed (nft missing, list error). The daemon
  treats unknown as **not safe to enforce** (`enforce` policy fails closed).
  (Detection errors are logged; the state is not guessed.)
- `failed` — disable was attempted and re-detection still reports `enabled`
  (disable did not take effect). Used by the disable path and by `enforce`
  mode's fail-closed branch.

Inventory commands recorded per environment (evidence): `nft list flowtables`,
`nft -a list ruleset` (grep `flow`), `uci show firewall | grep -E 'flow|offload'`,
`ethtool -k <if>`, `sysctl -a | grep -E 'bridge-nf|flow'`, plus kernel/target/
driver/interface/timestamp. **QEMU TCG absence of a flowtable is NOT evidence
hardware offload is off** — TCG has no offload engine; absence only proves the
ruleset has none.

## 3. Disable path (explicit, idempotent, verified)

Two cooperating actions:

1. **Operator-visible, persistent (OpenWrt):** set UCI
   `firewall.@defaults[0].flow_offloading='0'` and reload firewall4
   (`/etc/init.d/firewall restart`). This is the durable fix; fw4 will not
   recreate the flowtable. Documented, with rollback (set `1` + reload).
2. **Daemon/CLI immediate, verified (`goshisen offload disable`):** performs a
   transactional `nft -f` program that removes only flow-offload objects:
   - `nft delete flowtable <fam> <table> <flowtable>` for each flowtable found;
   - `nft delete rule <fam> <table> <chain> handle <N>` for each rule whose
     statement is `flow offload`/`flow add @` (handles from `nft -a list
     ruleset`).
   Nothing else is touched (no table/chain deletion, no input/management
   surface). Idempotent: with nothing found, no-op success. After the delete,
   re-detect; if still `enabled` → report `failed` and exit non-zero.

Ordering: detection runs at daemon start **after** hook install decision but
**before** readiness; the 10 s watchdog re-runs detection, so an offload
enabled by a later firewall reload is caught and (in `enforce` mode)
re-attempted / failed-closed.

## 4. Behavior matrix (config `offload_policy`)

| Policy | offload `disabled` | offload `enabled` | offload `unknown` |
| --- | --- | --- | --- |
| `warn` (default) | run, ready | run, warn + gauge, ready | run, warn |
| `refuse` | run, ready | **startup fails before hook install** | run (warn) — unknown is not a proven gap |
| `readiness` | run, ready | run, enforce, gauge, **never ready** until disabled | run, ready (warn) |
| `enforce` (new) | run, ready | **attempt disable; if verified → run+ready, log; if disable fails (`failed`) → startup fails (fail-closed)** | **fails closed** (cannot prove safety) |

- `enforce` is the new fail-closed mode: it actively closes the gap and refuses
  to run when it cannot.
- **Diagnostic/development escape hatch:** explicit config `allow_bypass: true`
  (or env `GOSHISEN_ALLOW_BYPASS=1`) downgrades `enforce` to `warn` semantics
  with a loud one-time warning. It is opt-in, never default, and is for
  debugging only. No silent fallback: without it, enforce never continues past a
  gap.

## 5. Metrics & logging

- Gauge `goshisen_flow_offload_state` (0 disabled, 1 enabled, 2 unknown;
  replaces the boolean `..._active` for richer state, counter derived).
- Counter `goshisen_flow_offload_disables_total` + `..._failed_total` (disable
  outcome, no labels — bounded cardinality).
- Log lines: state transitions (`offload state=enabled detail=...`), every
  disable action (`offload disable: removed flowtable ...`), disable failure
  (`offload disable FAILED: ...`), enforce fail-closed startup error.

## 6. Rollback

- Feature rollback (PR revert): enforcement falls back to prior warn/refuse/
  readiness behavior; no state kept in the store.
- Disable rollback: re-enable via UCI (`flow_offloading='1'` + firewall
  reload); immediate in-kernel deletes are replaced by the next fw4 reload's
  re-creation.
- No tags/releases are touched by this task; v0.2.1 assets unchanged.

## 7. Hardware/TCG limits (explicit)

- QEMU TCG offloads 0 packets; a flowtable present in the ruleset is still
  detected and disabled, but **no packet bypass is observable in the lab**.
- Driver/platform flow acceleration beyond nftables cannot be observed or
  disabled generically — documented as `unknown` for hardware we cannot
  inspect; `enforce` fails closed only on nft-observable offload. This is the
  stated limitation (backlog #3 remains).
- ethtool inventory is recorded as evidence, not as a check.
