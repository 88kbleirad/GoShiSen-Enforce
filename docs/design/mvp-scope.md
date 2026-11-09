# GoShiSen-Enforce — MVP Scope

Status: **Phase 6 — NAT + packaging complete (privileged integration still not runnable in this environment: non-root).** Companion to `docs/design/architecture.md`; defines what the lab firewall MVP is, phase-by-phase acceptance criteria, and what is explicitly out of scope.

---

## 1. MVP goal

A Linux lab firewall that:

- filters **forwarded IPv4 TCP/UDP** traffic between two network namespaces,
- evaluates packets in **Go** via **NFQUEUE** (queue 0) with a **first-match YAML policy**,
- uses **kernel conntrack** for connection state (NEW/ESTABLISHED/RELATED/INVALID) and drops untracked/unsupported traffic,
- keeps **nftables** minimal (forward hook + queue rule, chain policy drop) and reserves its table for future NAT,
- is **fail-closed** by construction (kernel drops when no engine is available),
- runs entirely inside an isolated, reproducible netns lab with reliable setup/teardown.

Non-goals of the MVP are listed in §6; they are *design-reserved* (architecture document names the extension points) but not implemented.

---

## 2. Component inventory (what the MVP ships)

| ID | Component | Acceptance (minimum) |
| --- | --- | --- |
| C1 | nftables ruleset provisioning | **Done (Phase 3):** daemon creates `table ip goshisen`, `chain forward` (policy drop), queue rule via `nft` in the router namespace only. |
| C2 | NFQUEUE bind | **Done (Phase 3):** direct nfnetlink_queue backend; binds queue 0, sets maxlen, reads packets, writes verdicts; binds after policy load (fail-closed ordering). |
| C3 | IPv4/TCP/UDP parser | **Done (Phase 3):** parses without panicking; rejects truncated/unknown; fragments and unsupported protocols per architecture; fuzzed. |
| C4 | Policy engine | **Done (Phase 2):** YAML load + strict validation, first-match evaluation, default DROP, immutable snapshots, atomic replace, offline validate/simulate. State/direction semantics land in Phase 4. |
| C5 | Conntrack integration | **Done (Phase 4):** kernel attach metadata per packet (`NFQA_CT`/`NFQA_CT_INFO`); state/direction mapped to the engine; accepted-set in `King`; TCP state from kernel protoinfo. No userspace conntrack mirror/event subscription (not needed: decision uses per-packet CT metadata). |
| C6 | Lab harness | **Done (Phase 3):** `scripts/lab.sh` up/down/status; unique `gsh_` resource names; root-gated; host firewall untouched. |
| C7 | Metrics/logs | Prometheus counters (accepted/dropped per action+protocol) and queue overflow stats; audit log lines for `log: true` rules. |
| C8 | Reload | Atomic snapshot replacement is implemented (`policy.King`); daemon loads policy at startup. `SIGHUP` wiring lands in Phase 5. Failed reload keeps old policy; connections survive reload. |

---

## 3. Acceptance domains (cross-cutting)

Every implementation phase's acceptance must also satisfy:

- **Fail-closed:** killing the daemon stops traffic (subsequent packets are dropped); restarting restores service without manual nftables intervention.
- **No bypass:** non-TCP/UDP and IPv6 forwarded packets never reach the engine (kernel chain policy drop).
- **Determinism:** identical policy + identical packet stream → identical verdicts across runs.
- **Cleanup:** after `scripts/lab.sh` teardown, `ip netns list` and `ip link show` show no leftover lab artifacts; a second run works without manual intervention.
- **Build:** `go build ./...`, `go vet ./...`, and `go test ./...` pass from a clean checkout.

---

## 4. End-to-end scenarios (functional acceptance)

Each scenario is run against the lab topology (`fw`/`client`/`server`, §7 of architecture.md); a scenario passes when the named probe produces the named result.

| # | Scenario | Policy under test (concept) | Probe | Expected |
| --- | --- | --- | --- | --- |
| S1 | Default DROP | empty ruleset | client→server TCP/UDP | all blocked |
| S2 | Explicit allow + state | `allow-new` for dst_port 80 TCP, `allow-established` for ESTABLISHED/RELATED | client→server TCP 80, then reply | handshake completes; replies pass |
| S3 | Reply without accept | `allow-established` present, **no** NEW-allow | server→client unsolicited TCP SYN | dropped (no accepted connection) |
| S4 | First-match | two rules overlapping on dst_port 8080 (DROP first, ACCEPT second) | client→server TCP 8080 | dropped (first match wins). Covered offline by `policy simulate`. |
| S5 | CIDR + port semantics | allow only `10.0.0.0/24`→server:22 | (a) in-subnet source, (b) other source | (a) allowed, (b) dropped |
| S6 | Protocol filtering | allow only UDP 53 | TCP 53 vs UDP 53 | TCP dropped, UDP allowed |
| S7 | INVALID | state:INVALID DROP rule | malformed/untracked packet (e.g. late fragment) | dropped |
| S8 | IPv6/non-TCP-UDP | any policy | IPv6 TCP, ICMP, GRE probes | dropped (never queued) |
| S9 | Reload | allow TCP 22 → reload to allow TCP 2222 | probe 22 after reload | dropped; new policy governs; traffic continues for existing established connections |
| S10 | Crash/restart | allow TCP 80 | kill daemon mid-connection, probe; restart, probe | probe fails while down; after restart, new connects work |
| S11 | Queue overflow | flood beyond maxlen | overflow counter increments; no policy bypass (dropped packets stay dropped) |
| S12 | Invalid policy | bad YAML / bad port / bad CIDR | daemon refuses to start (or keeps old policy on reload) with a clear error |

---

## 5. Phase acceptance criteria

### Phase 1 — Foundation (COMPLETE)
- [x] Valid Go module; `go build ./...`, `go vet ./...`, `go test ./...` pass
- [x] CLI: `--help`, `--version`, `--config/-c`, `--interface`, `--policy-file`, `--log-level`, `--log-json`; unknown flags/commands error with exit 2
- [x] Configuration precedence defaults < file < env (GOSHISEN_*) < flags; per-field source recorded and shown by `config show`
- [x] Strict YAML file decoding (unknown keys rejected); clear errors name field, value, and fix
- [x] Structured logging (`log/slog`, text or JSON) honoring `log_level`
- [x] Signal handling: SIGINT/SIGTERM → graceful shutdown, exit 0
- [x] No system modifications: only read-only validation (policy file readable, interface present/up)
- [x] Makefile gate (`make check` = fmt + vet + test + build) and CI workflow
- [x] Sample config `configs/goshisen.yaml`; docs (README, user/developer guides) match repository

### Phase 2 — Policy engine (COMPLETE)
- [x] YAML policy compile with strict decoding (unknown fields rejected; reserved `state`/`in_iface`/`out_iface` rejected with targeted messages)
- [x] IPv4 CIDR (`any`, `a.b.c.d/n`, bare host) and port matchers (single, inclusive `lo:hi`), TCP/UDP/any protocol
- [x] ACCEPT/DROP actions; unique rule IDs (duplicates rejected)
- [x] First-match evaluation and default DROP; ambiguous port/protocol combinations rejected
- [x] Immutable `Snapshot` (defensive copies) and atomic `King` replacement (concurrent-safe, `-race` tested)
- [x] Backend-independent `policy.Packet` metadata; engine runs without root or network
- [x] Table-driven tests: CIDR boundaries, port boundaries, overlapping rules, rule ordering, default deny, invalid configuration, reload/atomic swap
- [x] Offline simulation: `goshisen policy validate` and `goshisen policy simulate` (verdict + matching rule ID; no system changes)

### Phase 3 — Real packet enforcement (COMPLETE; lab verification pending)
- [x] NFQUEUE backend (`internal/packet/capture.go`) speaking nfnetlink_queue over `github.com/mdlayher/netlink` (direct; no cgo; `go-nfnetlink` does not exist as a module)
- [x] Every queued packet receives a verdict (parse failure, missing decider, or engine panic → DROP; panic recovery tested)
- [x] IPv4/TCP/UDP parser: never panics on truncated/malformed/unsupported input; fuzzed (`FuzzParse`, 3M execs clean); fragment and unsupported-protocol behavior per architecture §6.2–§6.3
- [x] nftables hook (`internal/hook`): rule `ip goshisen` + forward chain (policy drop) + queue rule; created only in the router namespace; host firewall never touched
- [x] Isolated lab `scripts/lab.sh`: unique `gsh_` namespaces/veths, up/down/status, root-gated, deterministic cleanup
- [x] Integration tests (real traffic, no mocks): allowed TCP, allowed UDP, denied traffic, default DROP, queue-with-no-listener, clean shutdown — all six present; **gated on privileges** (`checkPrereqs`), skipped here with explicit reasons because this machine runs as non-root
- [x] Daemon wiring: policy loads before the hook is installed; fail-closed at every step; graceful SIGINT/SIGTERM shutdown with counter summary
- [ ] Acceptance: run `sudo make integration` on a privileged host/runner to execute the six scenarios (cannot be satisfied in this session — no root)

### Phase 4 — Stateful filtering (COMPLETE; lab verification pending)
- [x] Verified the backend exposes required metadata: kernel `NFQA_CFG_F_CONNTRACK` flag confirmed in headers; `NFQA_CT` + `NFQA_CT_INFO` attached per packet; `IP_CT_IS_REPLY` gives explicit direction; `CTA_PROTOINFO_TCP_STATE` gives kernel TCP state (no Go conntrack reimplementation)
- [x] Backend configured to request CONNTRACK attributes; `applyCTInfo` maps kernel ctinfo → engine state/direction; untracked → INVALID
- [x] `state` matcher live in policy compile (NEW/ESTABLISHED/RELATED/INVALID; invalid values rejected)
- [x] Stateful semantics: NEW-accept records the connection in `King`'s accepted-set; ESTABLISHED/RELATED require acceptance (five-tuple alone never proves establishment); direction explicit
- [x] Policy reloads (SIGHUP) atomic; failed reload keeps old policy; accepted connections survive reload (engine + integration coverage)
- [x] Integration tests added (all root-gated, skipped here with reasons): TCP handshake+echo, unsolicited inbound, invalid connection state, connection timeout/hold, UDP timeout/reuse, reload-while-established
- [x] ICMP RELATED limitation documented (hook queues TCP/UDP only; ICMP errors never reach the engine)
- [ ] Acceptance: run `sudo make integration` on a privileged host/runner to execute the state scenarios (cannot be satisfied in this session — no root)

### Phase 5 — REST API + metrics (COMPLETE)
- [x] REST API `internal/api`: binds 127.0.0.1 by default; bearer-token auth for admin ops (config/env only, never argv); bodies ≤16 KiB (413); server timeouts; structured JSON errors
- [x] View active policy (`GET /v1/policy`); validate without applying (`POST /v1/policy/validate`); apply by version with `base_version` compare-and-swap (incompatible concurrent writes → 409, never silent overwrite); list versions; activate stored version (rollback)
- [x] Invalid policy → 422 before any state change; active policy preserved (unit-tested)
- [x] Durable policy storage `internal/store`: immutable `<version>.yaml` + atomic `active.json` pointer (temp+fsync+rename+dir-fsync); restart recovery; versioned rollback
- [x] Audit logging for admin actions + versions; tokens never logged (redacted in `config show` too)
- [x] Metrics `internal/metrics`: bounded-cardinality labels (action/protocol/state enums, error kind), active-policy-version gauge, verdict-latency histogram; standalone `metrics_port` + `GET /metrics` on API; no IP/port/flow-ID labels
- [x] Liveness `GET /healthz`; readiness `GET /readyz` (reflects enforcement state)
- [x] `api/openapi.yaml` matches the implementation
- [x] Tests: auth (missing/valid + audit), conflict, invalid-preserves-active, concurrent apply (exactly one winner), restart recovery, activate/rollback, body limit, readiness, metrics, store atomicity; all `-race` clean
- [x] Config: `api_host`, `api_port`, `api_token`, `metrics_port`, `policy_dir` (+ env, file; flags except token)
- [ ] Privileged integration: API + metrics exercised inside the lab (root-gated; not runnable in this environment)

### Phase 6 — NAT + packaging (COMPLETE; lab verification pending)
- [x] NAT purely kernel-side: nftables `dstnat` (DNAT) + `srcnat` (MASQUERADE) chains in the app-owned table; no Go address/checksum rewriting
- [x] Atomic application: one `nft -f` transaction per install (`add table` + `delete table` + full redefinition in one batch); a failed apply changes nothing; `Cleanup` (and the `goshisen hook cleanup` command) deletes only the app table; no flushing outside scope
- [x] Address semantics documented: engine sees post-DNAT dst (prerouting precedes FORWARD) and pre-MASQUERADE src (postrouting follows FORWARD); return traffic by conntrack; proven by integration tests
- [x] Config: `nat.enabled`, `nat.masquerade.{enabled,out_iface}`, `nat.dnat[]` (name/protocol/pub_ip/pub_port/target_ip/target_port) with validation
- [x] Integration tests added (all root-gated, skipped here with reasons): DNAT, MASQUERADE, return traffic, NAT+filtering (post-DNAT view), reload-while-NAT-connections (hold probe survives SIGHUP)
- [x] Dockerfile: multi-stage (Go build + static nftables build → distroless base), minimal runtime image, explicit `--cap-add=NET_ADMIN,NET_RAW`, no privileged mode, health-check guidance (healthz/readyz), graceful SIGTERM shutdown (STOPSIGNAL TERM), durable policy volume, restart/recovery guidance
- [x] No install scripts (uninstall/rollback reliability bar unmet; documented); no Kubernetes manifests (CNI relationship undefined; documented)
- [ ] Acceptance: run `sudo make integration` on a privileged host/runner to execute the NAT scenarios (cannot be satisfied in this session — no root)

### Phase 7 — Hardening/release
- [ ] Robustness: queue overflow handling + counters (S11); crash/restart hardening (S10; fail-closed + supervisor restart); lab teardown reliability under `kill -9` during traffic (D4)
- [ ] Dependency pins in go.mod, `go mod verify` clean, `go vet`/`go test ./...` green (D5).
- [ ] Docs closure: user guide (config, run, lab), developer guide (build, test, contribute), SECURITY.md, README status accurate.
- [ ] Final acceptance: full scenario suite + lab cleanup + a cold-clone build.

---

## 6. Explicitly out of MVP scope (reserved)

| Feature | Why not now | Design hook (where it will land) |
| --- | --- | --- |
| IPv6 filtering | MVP is IPv4 TCP/UDP | nftables `ip6` table or family inet; parser IPv6 headers |
| ICMP/other L4 protocols | not in target traffic | queue rule matchers; parser extension; engine protocol enum |
| DNAT/MASQUERADE | **implemented in Phase 6** (kernel-side nftables) | see §8.2 architecture.md for pre/post-NAT semantics |
| Port forwarding beyond DNAT forms above | DNAT covers the MVP need | additional `dnat` rule shapes |
| REJECT verdicts | MVP drops only | nftables `reject` rule (kernel-side), not NFQUEUE |
| REST API / dynamic rule management | **implemented in Phase 5** | `internal/api` |
| eBPF/XDP offload | different machinery entirely | n/a |
| Multi-queue / queue-balance, GSO offload | performance tuning beyond lab needs | NFQUEUE `queue-balance`, `NFQA_CFG_F_GSO` |
| TLS/encryption of control plane | management is local netns | out of scope |
| High availability, clustering, state replication | lab firewall only | n/a |
| Kubernetes deployment manifests | relationship with the cluster CNI is undefined (a pod-CNI firewall would conflict with or duplicate the CNI's own NAT/filter rules); must be defined before manifests exist | document the CNI interaction model first |
| Install/uninstall scripts | would need reliable uninstall + rollback semantics to be worth shipping; not yet met | ship only with verified teardown |
| Web UI, Prometheus alerting, dashboards | monitoring beyond counters | not planned; no web tree exists in the repository |
| QoS/rate limiting/traffic shaping | not required by policy model | nftables meters/limits |
| Logging/audit pipeline beyond `log: true` line | minimal | `internal/metrics` expansion |

---

## 7. Dependency pins / compatibility risk (summary)

See `docs/design/architecture.md` §8 for the full matrix. MVP pins:

- `github.com/mdlayher/netlink` (pinned v1.11.2) — the NFQUEUE backend's base; `go-nfnetlink` does not exist and is abandoned.
- `gopkg.in/yaml.v3` — strict policy decode.
- Hand-rolled IPv4/TCP/UDP parser (no gopacket), fuzzed.
- Hook provisioning via the `nft` binary (host tool), not a library.
- Kernel modules `nf_tables`, `nfnetlink_queue`, `nf_conntrack`, `nf_defrag_ipv4`; refusal to start with clear diagnostic if absent.
- Go ≥ 1.22, Linux ≥ 5.6.

---

## 8. Definition of done (Phases 1–7 totality)

- Working firewall in the lab as specified in §4 (all scenarios S1–S12 documented above),
- architecture.md + this document accurate, README reflecting true status at every phase boundary,
- tests for every acceptance criterion listed above are part of `go test ./...` or the lab script (no unverifiable acceptance),
- no known TODOs in the committed code; all reserved features are documented as out-of-scope here.
