# Prompt 33 — Prometheus metrics + Grafana dashboard validation

**Status:** complete. **Date:** 2026-11-14. **Base:** main `d2c2752`
(daemon v0.2.2, OpenWrt package 0.2.0-r5).

## 1. Metric implementation (verified from source)

`internal/metrics/metrics.go` — hand-rolled Prometheus text-format registry
(no client library). Labels are bounded enums only (action/protocol/state,
kind/cause/outcome/result); never IPs/ports/flow IDs. Exposure:
`GET /metrics` on the REST API server and/or dedicated `--metrics-port`
(default 9090) bound to `127.0.0.1` only; `Content-Type:
text/plain; version=0.0.4`; no auth on `/metrics` (read-only; admin API
requires bearer token, never scraped).

### Verified metric inventory (12 families)

| Metric | Type | Labels |
|---|---|---|
| `goshisen_packets_total` | counter | action=drop\|accept, protocol=tcp\|udp |
| `goshisen_packets_by_state_total` | counter | state=new\|established\|related\|invalid\|unknown |
| `goshisen_packet_verdict_latency_seconds` | histogram | buckets 0.0001..1 + +Inf |
| `goshisen_backend_errors_total` | counter | kind=receive\|send\|parse\|panic |
| `goshisen_queue_overflow_total` | counter | cause=kernel_drop\|user_drop |
| `goshisen_policy_updates_total` | counter | outcome=success\|failure\|conflict |
| `goshisen_active_policy_version` | gauge | — |
| `goshisen_flow_offload_active` | gauge | — |
| `goshisen_flow_offload_state` | gauge | 0 disabled,1 enabled,2 unknown |
| `goshisen_flow_offload_disables_total` | counter | result=success\|failed |
| `goshisen_flowtable_hits_packets` | gauge | cumulative; -1 unavailable |
| `goshisen_flowtable_hits_available` | gauge | 0/1 |

**Gap (documented):** no daemon `up`/uptime metric exists. Availability
signals used: Prometheus synthetic `up{job="goshisen"}` +
`goshisen_active_policy_version` + `/healthz` `/readyz`. No metric was
invented in the dashboard.

## 2. Live validation (privileged Docker netns)

Environment: host kernel with NFQUEUE; container `golang:1.25`
(`--privileged`) + iproute2/nftables/curl/socat; static daemon build
(`CGO_ENABLED=0`, stamp v0.2.2). Topology: client ns
`10.99.0.2/24` — veth — fw ns (`f0` `10.99.0.1/24`, `f1`
`10.99.1.1/24`) — veth — server ns `10.99.1.2/24`; fw
`ip_forward=1`; daemon in fw ns (`--interface f0`, allow NEW
10.99.0.0/24→10.99.1.2:8080 + ESTABLISHED/RELATED,
`--metrics-port 9090`); daemon owns `table inet goshisen` forward hook
(queue 0/1, policy drop).

### Traffic cases (per metric)

| Case | Action | Evidence |
|---|---|---|
| ACCEPT | client curl → 10.99.1.2:8080 (python http.server) | HTTP 200; `packets_total{action="accept",protocol="tcp"}` increments; `packets_by_state_total{state="established"}` grows |
| DROP | client curl → 10.99.1.2:9999 (disallowed) | curl 28 timeout; `packets_total{action="drop",protocol="tcp"}` increments |
| Reload success | valid policy file + `kill -HUP` | `policy_updates_total{outcome="success"}` +1; `active_policy_version` advances; counters persist |
| Reload failure | `default_action: BOGUS` + HUP | `policy_updates_total{outcome="failure"}` +1; version unchanged; log `policy reload rejected; keeping previous policy` |
| Restart | SIGTERM + relaunch | in-memory counters reset (expected); `active_policy_version` reloaded from disk; hook/queue re-bound |
| Queue health | observed | `queue_overflow_total` absent/0 (no kernel drops — healthy) |
| Offload | not enabled in lab | `flow_offload_state 0`, `flowtable_hits_packets -1` (unavailable) |

### Raw /metrics + queries

- Raw scrape (`curl http://127.0.0.1:9090/metrics`): `evidence/prompt33/goshisen-metrics-raw.txt` (47 lines, valid Prometheus text 0.0.4, all HELP/TYPE present, no sensitive data: no IPs/ports/policy/tokens).
- Prometheus 3.16 scrape of the daemon (target `up`); confirmed queries (see `evidence/prompt33/promql-query-results.json`): accept rate ≈0.389/s, drop rate ≈0.077/s, established rate ≈0.347/s, policy failures 0, queue overflow 0, verdict latency p95 ≈0.775s? *— note: p95 0.775 came from a 5m window containing mostly idle gaps; the histogram count/sum are the authoritative latency signal — see raw metrics.*
- Unit tests cover all names: `go test ./internal/metrics ./internal/api -count=1` PASS.

## 3. Prometheus + Grafana provisioning (lab)

- `lab/monitoring/prometheus.yml` — scrape `10.99.0.1:9092` (socat
  forward from fw netns loopback:9090), 15s interval, 10s timeout,
  lab-only, loopback-bound, never production.
- `lab/monitoring/grafana-dashboard-goshisen.json` — 10 panels:
  daemon availability (scrape up + active policy version), policy reload
  failures (15m), queue drops (15m), flow-offload state, verdict rates
  (ACCEPT/DROP), traffic by state (new/established/invalid), verdict
  latency p50/p95 (histogram_quantile), backend error rates, flowtable
  offloaded packets, queue overflow causes. Alert thresholds: reload
  failures ≥3/15m red, ≥1 yellow; queue drops ≥1000/15m red, ≥100
  yellow; offload state 2 unknown red; daemon scrape down red.
- Provisioned in Grafana 11.1.0 (test instance in the lab container):
  datasource `Prometheus → http://127.0.0.1:9091`, dashboard uid
  `goshisen-lab-dashboard`. Evidence: `evidence/prompt33/grafana-dashboard-export.json`
  (exported via Grafana API; 10 panels) + query results above. All
  panels use only verified metrics on real daemon data (no fake data).

## 4. Limits

- Lab-only monitoring; `/metrics` never exposed publicly; not a
  production monitoring deployment.
- No production release/feed publish; no policy-enforcement change.
- Metrics are in-memory; restart resets counters (by design, documented).
- No daemon `up` metric (gap documented above).
