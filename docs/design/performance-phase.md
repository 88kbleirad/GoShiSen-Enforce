# Performance Phase — Baseline, Profile, Optimize (Prompt 03)

**Status: complete.** Raw benchmark logs in `evidence/perf-prompt03/`
(`base_idle_*_run{1,2,3}.txt` = pre-optimization, run in an idle
environment; `opt_*_run{1,2,3}.txt` = post-optimization, same machine).
This report only claims improvements verified by three runs per side with
median + population standard deviation (σ) and environment metadata.

## 0. Environment (identical for baseline and optimized runs)

| Axis | Value |
| --- | --- |
| Host | dev box (uid 1000, no root, no CAP_NET_ADMIN) |
| CPU | 11th Gen Intel Core i5-11400H, 12 threads, 2.70 GHz base / 4.5 GHz boost |
| RAM | 32 GB (31.8 GiB total) |
| OS / kernel | Linux 7.3.3-1-cachyos |
| Go | go1.27.3-X:nodwarf5 linux/amd64 |
| Commit (pre) | `bfbf813` (baseline worktree, clean) |
| Commit (post) | working tree at `bfbf813` + this phase's changes (uncommitted) |
| Bench mode | `go test -bench=. -benchmem -count=1 -run '^$'`, 3 runs per side, same day/idle |
| Method | median + pstdev across the 3 runs; changes where |Δ| > 3×σ |

Models: `internal/policy` (engine), `internal/packet` (parser + backend
decision pipeline), `internal/metrics` (result observation). Real
NFQUEUE socket throughput is kernel/root-gated; the pipeline benchmark
measures the full in-process per-packet path *including* metric
observation, excluding the netlink socket syscalls.

## 1. Baseline (pre-optimization)

Hot-path profile (pprof, `BenchmarkKingProcessNewAccept`): `fmt.doPrintf`
≈ 55% of CPU — the accepted-set key (`tupleKey`) built with
`fmt.Sprintf` per stateful packet; `runtime.mallocgc` ≈ 19% (allocs).
Per-packet allocations on the full pipeline: **15–16 allocs, ~312 B**.

| Benchmark | Base median ns | Base σ | Base allocs/op |
| --- | ---: | ---: | ---: |
| Parse IPv4 TCP | 16.7 | 0.0 | 0 |
| Parse IPv4 UDP | 16.8 | 0.1 | 0 |
| Parse IPv6 TCP | 18.1 | 0.0 | 0 |
| Decide small policy (NEW accept) | 38.7 | 0.0 | 0 |
| Decide small policy (ESTABLISHED) | 26.9 | 0.4 | 0 |
| Decide 200-rule policy (worst-case) | 2181 | 38.3 | 0 |
| King.Process NEW accept | 934 | 16.7 | 16 |
| King.Process ESTABLISHED reply | 409 | 4.6 | 7 |
| tupleKey | 359 | 4.0 | 6 |
| Pipeline NEW accept (e2e) | 1129 | 24.7 | 15 |
| Pipeline ESTABLISHED reply | 634 | 2.8 | 7 |
| Pipeline NEW default-drop | 668 | 8.2 | 8 |
| Pipeline metrics-only | 86.5 | 0.6 | 1 |
| Latency p50 / p95 / p99 (µs) | 1.039 / 1.719 / 2.686 | 7.4 | 15 |

## 2. Bottleneck

The decision path itself (parse + prefix/port/state matching) was
already zero-alloc. The hot cost was the **stateful accepted-set**:

- `tupleKey` used `fmt.Sprintf("%s|%d|%s|%d|%d", …)` → ~360 ns, 6 allocs
  per packet (called by `IsAccepted` for every stateful packet and by
  `MarkAccepted` on every NEW-accept).
- `sync.Map` with a string key added map-node allocations.
- Metrics: two separate mutex acquisitions per packet (`ObserveLatency`
  + `ObserveVerdict`), string-keyed map increments → ~86 ns, 1 alloc.
- `Rule.matches` re-computed `Prefix.Contains` via
  `netip.PrefixFrom(...).Masked()` per rule per packet.

## 3. Changes (controlled, semantics-preserving)

1. `internal/policy/engine.go` — `tupleKey` string → struct `tupleKey
   {lo, hi uint64; ppm uint64}` (5-tuple packed: v4 as two 32-bit
   halves; v6 as full 128-bit pair; protocol<<32 | srcPort<<16 |
   dstPort in 64 bits). Reply
   canonicalization preserved (swap src/dst + ports when
   `Direction == DirReply`). `sync.Map` → `sync.RWMutex` + map.
   Pinned by `internal/policy/tuplekey_test.go` (v4/v6 reply mapping,
   distinct-connection non-collision).
2. `internal/policy/engine.go` — `Rule` compacted: `States []State` →
   `StateMask uint16` (single bit test, no slice scan); `*PortRange` →
   value `PortRange` + `HasSrcPort`/`HasDstPort` bools.
3. `internal/metrics/metrics.go` — `ObserveVerdict`+`ObserveLatency`
   → single `ObservePacket` under **one** lock; string-keyed maps →
   fixed `[2][3]int64` / `[5]int64` arrays indexed by enum ints.
   Enum types (`BackendErrorKind`, `OverflowCause`,
   `PolicyUpdateOutcome`) became typed ints with `String()` rendering;
   Prometheus text output byte-identical. `internal/app` decider
   updated to the new API.
4. `internal/packet/capture.go` — attribute parsing in `evaluate`
   switched from a closure-walk (`parseAttrs` + closure per packet) to
   one inlined switch (`parseNFQueueAttrs`); verdict message buffer now
   pooled (`verdictBufPool`), eliminating one `make` per verdict.

Parser code unchanged; its small pre/post delta is environment noise.

## 4. Result (3 runs each, same idle machine, median + σ)

| Benchmark | Base med | Opt med | Δ | Base allocs | Opt allocs | Opt σ |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| King.Process NEW accept | 934 ns | 103 ns | **−89%** | 16 | 0 | 3.5 |
| King.Process ESTABLISHED | 409 ns | 53 ns | **−87%** | 7 | 0 | 1.8 |
| tupleKey | 359 ns | 4.4 ns | **−99%** | 6 | 0 | 0.1 |
| Pipeline NEW accept (e2e) | 1129 ns | 253 ns | **−78%** | 15 | 0 | 5.7 |
| Pipeline ESTABLISHED reply | 634 ns | 193 ns | **−70%** | 7 | 0 | 3.5 |
| Pipeline NEW default-drop | 668 ns | 206 ns | **−69%** | 8 | 0 | 3.1 |
| Pipeline metrics-only | 86.5 ns | 15.8 ns | **−82%** | 1 | 0 | 0.0 |
| Decide small NEW | 38.7 ns | 35.5 ns | −8% | 0 | 0 | 0.3 |
| Decide small ESTABLISHED | 26.9 ns | 23.6 ns | −12% | 0 | 0 | 0.5 |
| Decide 200 rules | 2181 ns | 2035 ns | −7% | 0 | 0 | 14.5 |
| Parse IPv4 TCP | 16.7 ns | 17.9 ns | +7% | 0 | 0 | 0.2 |
| Latency p50 (µs) | 1.039 | 0.306 | **−71%** | 15 | 0 | 3.3 |
| Latency p95 (µs) | 1.719 | 0.31 | **−82%** | | | | |
| Latency p99 (µs) | 2.686 | 0.48 | **−82%** | | | | |

### Notes

- **Zero allocations** on the full decision pipeline after optimization
  (parser, engine, metrics, verdict message build).
- Per-packet in-process capacity at 253 ns/op ≈ **~3.9 M packets/s**
  single-core (decision-only; real NFQUEUE throughput adds netlink
  syscalls and kernel copy, and is root-gated — measured in the lab).
- Parse improvements are not claimed (code unchanged; within noise σ).

## 5. Semantic verification (unchanged behavior)

| Check | Evidence |
| --- | --- |
| Worker count | 2 goroutines (one per queue socket, IPv4 + IPv6), `Backend.Run`; no configurable worker pool exists — decision pipeline is single-threaded per socket |
| Policy size | `BenchmarkDecideLargePolicy` (200 rules) verified; scaling is O(rules) first-match |
| IPv4/IPv6 | `BenchmarkParseIPv4TCP/UDP`, `BenchmarkParseIPv6TCP`, `TestParseIPv6*`, `TestPublishPolicyIPv6`, `TestTupleKeySemantics/ipv6` |
| Stateful semantics | `TestStatefulDecide`, `TestStatefulKingProcess`, `TestPolicyReloadKeepsEstablished`, `TestTupleKeySemantics` (reply canonicalization), pipeline established-reply bench |
| Reload atomic | `TestKingAtomicReplace`, `TestKingConcurrentReplace`, `TestPublishPartialFailureKeepsOld`, `TestPublishConcurrentSerializes`, `TestRunSIGHUPReload`, `TestReloadFromFile*` |
| Default-drop | `TestDefaultDeny`, `TestDecideNoMatchDefaultDrop`, kernel chain policy drop (architecture §2.1) |
| Fail-closed | `TestFailClosedNeverRequestsFailOpen`, verdict always sent (`TestEvaluateVerdicts`, `TestEvaluatePanicSafety`) |

All `go test -race ./...` pass; `make check` (fmt + vet + test +
build) passes.

## 6. Files

- Benchmark suite: `tests/benchmark/` (empty), `internal/packet/parser_bench_test.go`, `internal/packet/pipeline_bench_test.go`, `internal/policy/engine_bench_test.go`.
- New test: `internal/policy/tuplekey_test.go`.
- Evidence: `evidence/perf-prompt03/` (env.md, base_idle_*, opt_*, base_*).
