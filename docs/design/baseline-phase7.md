# Baseline Report — GoShiSen-Enforce (Phase 0, branch `phase7-baseline`)

Date: 2026-11-09. Repository state at commit `24bac6b` (branch `phase7-baseline`, clean working tree — no uncommitted changes, no staged files, no untracked files).

## 1. Current implemented features

| Area | Package | Status |
| --- | --- | --- |
| Policy engine (YAML, strict, first-match, atomic snapshots) | `internal/policy` | complete, unit-tested |
| NFQUEUE backend (direct nfnetlink_queue over mdlayher/netlink) | `internal/packet` | complete, unit-tested, fuzzed |
| IPv4/TCP/UDP parser (no panic; fragments/protocol policy) | `internal/packet` | complete, fuzz-clean |
| nftables hook (app-owned table `ip goshisen`, atomic install, cleanup) | `internal/hook` | complete |
| Durable policy store (versioned files, atomic active pointer) | `internal/store` | complete |
| REST API (localhost bind, bearer auth, version CAS, audit, health/readiness) | `internal/api` | complete |
| Metrics (bounded-cardinality Prometheus text format) | `internal/metrics` | complete |
| Daemon lifecycle (policy-before-hook, graceful signals, SIGHUP reload) | `internal/app` | complete |
| NAT (DNAT + MASQUERADE, kernel-side, atomic transaction) | `internal/hook` | complete (Phase 6) |
| Docker packaging (multi-stage, distroless, no privileged) | `deployments/docker/Dockerfile` | complete |
| Lab harness (isolated netns + veth, robust teardown) | `scripts/lab.sh` | complete |
| Integration suite (real traffic, root-gated, 17 scenarios) | `tests/integration` | present; **not runnable as non-root** |

Baseline gate (run on Go 1.27.1): `go mod verify` ✓, gofmt clean ✓, `go vet ./...` ✓, `go test -race ./...` ✓ (all packages pass; integration tests skip with explicit reason), `go build ./...` ✓.

## 2. Missing or incomplete acceptance criteria

- Phase 3/4/6 privileged integration scenarios: never executed in this environment (non-root). Code exists; `checkPrereqs` gates on euid 0 + tools + netns creation.
- Phase 7 (hardening/release): all items open at baseline — queue overflow observability, crash/restart hardening, supervisor config, release closure.
- Architecture doc (`docs/design/architecture.md`) still describes a userspace conntrack-mirror design that the implementation does not have (implementation reads per-packet CT metadata directly from NFQUEUE). Doc/impl mismatch to reconcile (Phase 5).
- User guide header still said "Phase 1 (foundation)" at baseline; README said Phase 6. Docs to synchronize.

## 3. Toolchain inconsistencies

| Where | Before (baseline) | Required |
| --- | --- | --- |
| `go.mod` | `go 1.25.0` | — |
| Dependencies (netlink v1.11.2, socket v0.6.0, x/sys, x/net, x/sync) | all declare `go 1.25.0` floors | Go ≥ 1.25 |
| CI `.github/workflows/ci.yml` (+build and integration jobs) | `go-version: "1.22"` | **must be ≥ 1.25** — job cannot compile the module |
| `deployments/docker/Dockerfile` | `golang:1.25` | ✓ consistent |
| `docs/design/architecture.md` §8.1 | "Go ≥ 1.22" | stale → Go ≥ 1.25 |
| `docs/design/mvp-scope.md` §7 | "Go ≥ 1.22" | stale → Go ≥ 1.25 |
| `docs/developer-guide.md` | "Go ≥ 1.22 (developed on Go 1.27)" | stale → Go ≥ 1.25 |

Resolution applied: CI `go-version` → `"1.25"` (both jobs); doc floors → `Go ≥ 1.25`. `go.mod` stays `1.25.0`; Dockerfile unchanged. Installed local toolchain is Go 1.27.1 (≥ floor, OK).

## 4. Tests requiring root / CAP_NET_ADMIN / nftables / netns

All under `tests/integration/` (skip with explicit reason when non-root via `checkPrereqs`):

- `e2e_test.go`: TestLabAllowedTCP, TestLabAllowedUDP, TestLabDeniedTraffic, TestLabDefaultDrop, TestLabQueueNoListener, TestLabShutdownClean, TestHookCleanupScope.
- `state_test.go`: TestStateTCPHandshakeAndReply, TestStateUnsolicitedInbound, TestStateInvalidConnection, TestStateConnectionTimeout, TestStateUDPTimeout, TestStateReloadWhileEstablished.
- `nat_test.go`: TestNATDnat, TestNATMasquerade, TestNATReturnTraffic, TestNATWithFiltering, TestNATReloadWhileConnections.
- `overflow_test.go` (new, Phase 7): TestQueueOverflowKernelDrop.

## 5. Uncommitted files / risky deletions

- At baseline: **none** — working tree clean on `main`; no uncommitted files, no risky deletions.
- Lab resources are namespace-contained (`gsh_*`); `scripts/lab.sh down` deletes only lab namespaces/veths and never touches host firewall.

## 6. Smallest safe changes proposed (Phase 0)

1. CI: `go-version: "1.22"` → `"1.25"` in both jobs (done).
2. Docs: Go floor 1.22 → 1.25 in architecture.md, mvp-scope.md, developer-guide.md (done).
3. No other toolchain changes required: go.mod, Dockerfile already consistent; `go mod verify` clean; gofmt/vet/test-race/build all pass.

## 7. Verification of the applied changes

- Full gate after edits: gofmt ✓, `go mod verify` ✓, `go vet ./...` ✓, `go test -race ./...` ✓, `go build ./...` ✓.
- Integration suite skips cleanly (root-gated) — not falsely reported as passing.
