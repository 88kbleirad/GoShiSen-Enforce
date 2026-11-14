# Flowtable Bypass Observability — Design (Prompt 16, backlog #3)

**Status:** proposed (implementation follows in this branch).
**Applies to:** flowtable hit measurement, `offload observe` command, metrics,
tests, docs. Release v0.2.1/assets/signing key untouched.

---

## 1. Measurement contract

Goal: provide observable evidence of flowtable bypass — packets that skipped
NFQUEUE — not just presence of a flowtable.

- **Observable:** a flowtable object with a `counter` statement (nftables
  counters the flowtable's offloaded packets). nft renders
  `counter packets N bytes M` inside the flowtable block. This is the only
  kernel-level counter that directly counts offloaded flows.
- **Secondary observation:** the flow-add rule/chain counters *before* vs
  *after* the `flow add @ft` statement. Packets that hit the flowtable add to
  its counter and may not appear in downstream chain/rule counters.
- **Read-only:** all measurement is `nft -a list ruleset` (parse-only). No
  ruleset/route/interface/connection state is ever modified.

## 2. State model (observability, distinct from detection state)

| State | Meaning | Confidence |
| --- | --- | --- |
| `absent` | No flowtable (detection disabled) | high |
| `present_zero_hit` | Flowtable exists, counter at 0 | high (counter present) |
| `present_positive_hit` | Flowtable exists, counter > 0 → **bypass observed** | high |
| `unknown` | Detection/nft failed, or counter unavailable (no `counter` statement) | limited/unknown |

**Rule:** zero hits ≠ offload disabled. Zero hits only mean no packets have been
offloaded since the counter was created (or traffic was insufficient, or the
platform's flowtable doesn't offload — e.g. QEMU TCG). Never infer "offload
disabled" from zero counters. Report `unknown`/`limited` with a reason when the
counter is unavailable instead of guessing.

## 3. Counter reset / monotonicity

- Flowtable counters are cumulative and monotonic within a flowtable lifetime.
- A flowtable rebuild (fw4 reload, disable+re-enable) resets counters to 0.
- `DeltaHits(after, before)`: if after ≥ before → delta; if after < before
  (reset/wraparound) → report the *after* value (re-based), never a negative
  delta. This is explicit and documented.
- Counter wrap (64-bit) is not a practical concern but the same reset logic
  covers it.

## 4. Instrumentation surface

- `goshisen offload observe` (read-only):
  - timestamp (UTC RFC3339Nano),
  - enforcement state (disabled/enabled/failed/unknown),
  - per-flowtable: family/table/name, cumulative packets/bytes,
  - aggregated hits + `HitsAvailable`,
  - confidence (high/limited/unknown) + reason.
- Metrics (Prometheus):
  - `goshisen_flowtable_hits_packets` (gauge, cumulative; -1 when unavailable),
  - `goshisen_flowtable_hits_available` (0/1),
  - existing `goshisen_flow_offload_state`.
- The daemon watchdog and startup update these metrics from the same parser;
  no new privileged operations.

## 5. Platform limits (explicit)

- **QEMU TCG:** flowtable exists (fw4) but offloads 0 packets — hit counters
  stay 0. This is recorded as zero-hit, NOT as "offload disabled". Bypass is
  not observable in TCG.
- **Hardware-capable:** flowtables with offload flatten (some drivers) still
  expose `counter`; positive hits are then direct bypass evidence. If the driver
  bypasses nftables entirely, counters stay 0 — reported as zero-hit/unknown,
  never as proof of anything.
- **Missing tools:** no nft, permission denied, `ethtool` absent → `unknown`
  with reason.

## 6. Rollback

PR revert keeps the prior detect/disable behavior; metrics/CLI additions are
additive and can be removed without touching enforcement semantics. No store
changes. No tags/releases touched.

## 7. Safety

Measurement is read-only by construction (parse `nft -a list ruleset`). The
disable path stays exactly as delivered in Prompt 15 — this prompt adds no
mutation. Zero/limited counters never auto-trigger policy changes.
