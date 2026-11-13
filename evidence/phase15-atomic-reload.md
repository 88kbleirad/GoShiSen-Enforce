# Prompt 01 — Atomic Policy Reload (Phase 15) Evidence

Date: 2026-11-13 (UTC+7)
Base: v0.2.0 baseline (Prompt 00) — HEAD `bfbf813`, `make check` PASS, 21/21 integration SKIP non-root.

## What changed

Unified, durable, atomic policy publish pipeline:

| Layer | Change |
|---|---|
| `internal/store` | `NextVersion()` (shared max+1); `OpenReadOnly()` (no dir creation) |
| `internal/api` | Shared mutation lock (`Options.Mu`); outcome metrics on apply/activate (success/failure/conflict); superseded private `nextVersion` removed |
| `internal/app` | `Daemon.reloadMu` (shared with API); `publish(snap, data, source)` — compile → next version → WritePolicy → SetActive → king.Replace, in that order under one lock; `reloadFromFile()` (single read); startup version gauge reflects recovered store version |
| `internal/metrics` | `goshisen_policy_updates_total{outcome="success"\|"failure"\|"conflict"}` (bounded enum) |
| `internal/cli` + `cmd/goshisen` | `policy dry-run --policy-file F --policy-dir D` — validates and previews the exact publish (next version, rules delta, default-action change) read-only; missing store is an error, never created |

Summary of the guarantee: a policy is never visible half-applied. Validation happens first; durable writes (policy file, then active pointer — both fsync'd) happen before the in-memory snapshot swap; any failure keeps the previous policy in both the store and the live ruleset. A daemon killed mid-publish recovers the previous active version (torn versions never activate).

## Tests added (7 new in app, 3 new in api/cmd)

- App (`internal/app/atomic_reload_test.go`):
  - `TestReloadFromFileMalformedKeepsOld` — malformed YAML + semantically invalid policy: king and store unchanged, `failure` metric incremented.
  - `TestReloadFromFileValidPublishes` — valid reload → new durable version + king swap + `success` metric.
  - `TestPublishPartialFailureKeepsOld` — read-only store (write fails): publish aborts before king swap; store+king keep old policy.
  - `TestPublishConcurrentSerializes` — 10 concurrent publishes → versions 2..11, store/king agree at the end.
  - `TestPublishPolicyIPv6` — IPv6 + IPv4 rules publish and decide correctly in both families.
  - `TestRunSIGHUPReload` — end-to-end: daemon running, rewrite policy file, SIGHUP the process; store active=1, king enforces new policy (UDP ACCEPT / TCP DROP).
  - `TestPublishKillFailClosedSimulation` (api pkg) — simulated mid-publish kill (v2 written, not activated): recovery gets v1; torn v2 listed but never active.
- API (`internal/api/server_test.go`): `TestAPISharedMutexWithDaemonPublish`, `TestAPIPolicyUpdateMetrics` (success/failure/conflict counters), `TestPublishKillFailClosedSimulation`.
- Cmd (`cmd/goshisen/main_test.go`): `TestRunPolicyDryRunReadOnly` (output contains `would publish version 2`, store untouched), `TestRunPolicyDryRunMissingStore` (fails without creating dir).

## Verification run

```
make check                              → PASS (exit 0) — fmt, vet, test ./..., static build
go test -race -count=2 ./internal/app/ ./internal/api/ ./internal/metrics/ → PASS (11.8s)
go test -race -count=1 ./...            → PASS (all packages, 7.07s)
```

Test count: 40 tests in app+api+cmd (23 in app incl. 6 new; 14 in api incl. 3 new; 2 in cmd). Integration suite unchanged (21/21 SKIP non-root).

## Dry-run smoke (built binary)

```
$ goshisen policy dry-run --policy-file /tmp/dr-policy.yaml --policy-dir /tmp/dr-store
policy /tmp/dr-policy.yaml: valid, 1 rule(s), default action DROP
dry-run: would publish version 1 (active: )
  rules: -1 -> 1 (first policy)
  default action: none -> DROP
$ ls /tmp/dr-store   → empty (nothing written)
$ goshisen policy dry-run --policy-file /tmp/dr-bad.yaml --policy-dir /tmp/dr-store
goshisen: policy: rule 1 (bad): state "BOGUS" must be new, established, related, or invalid
```

## Files changed (13 + 3 new; +472/−43)

M: README.md, cmd/goshisen/main.go, docs/design/architecture.md, docs/design/mvp-scope.md,
docs/developer-guide.md, docs/user-guide.md, internal/api/server.go, internal/api/server_test.go,
internal/app/app.go, internal/cli/cli.go, internal/cli/policycmd.go, internal/metrics/metrics.go,
internal/store/store.go
A: cmd/goshisen/main_test.go, internal/app/atomic_reload_test.go, evidence/ (this report)

## Known limits (unchanged from baseline)

- Root-gated integration suite (real NFQUEUE/NAT traffic) still cannot run on this box; the new reload semantics are exercised at the unit/doppelganger level. The CI privileged job remains the gate for real-traffic reload.
- The store is the single source of truth for the active version; a daemon that never receives SIGHUP and starts from a file-only policy does not create a store version until the first reload/apply.
