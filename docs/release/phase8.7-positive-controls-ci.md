# Phase 8.7 — Positive Controls, Regression & Draft PR Preparation

Branch: `phase7-baseline` (HEAD `33ed36f`). Date: 2026-11-10.

## Test changes (committed)

Positive controls added to every deny/fail-closed scenario so a deny
assertion cannot pass because the daemon or probe was broken:

| Test | Negative control | Positive control |
| --- | --- | --- |
| `TestLabDeniedTraffic` | 9090 (no rule) → DROP | 8080 allowed first (same daemon/run) |
| `TestLabDefaultDrop` | empty ruleset → DROP | allow-phase control first, then restart with empty rules |
| `TestLabQueueNoListener` | lab `gsh_lab` queue hook, no listener → kernel DROP | start daemon on the same queue 0 with an allow policy → same path succeeds |
| `TestStateUnsolicitedInbound` | reload to no-NEW policy → unsolicited server→client DROP | client→server NEW control succeeds before reload |
| `TestStateInvalidConnection` | unmatched port → DROP | valid NEW to allowed port succeeds (fail-open guard) |

## No-listener control design (TestLabQueueNoListener)

The lab installs a forward chain in `table ip gsh_lab` with
`meta l4proto { tcp, udp } queue num 0` and `policy drop`; no daemon is
started. The negative phase confirms the client→server TCP connect is
blocked (kernel drops the queued packet with no listener). The positive
phase starts the daemon, which binds the **same queue 0**; with an
explicit allow policy the same client→server path (10.99.0.2 →
10.99.1.2:8080) succeeds. Same namespaces, addresses, port, and queue;
only listener availability + policy differ — proving the block was the
missing listener (kernel fail-closed), not a broken route/path.

## Evidence

### Host regression (Go 1.25.0) — PASS
`go mod verify` PASS; `gofmt -l .` clean; `go vet ./...` PASS;
`go test ./... -count=1` PASS (9 packages); `go test -race ./... -count=1`
PASS (9 packages); `go build ./...` PASS; `git diff --check` clean.

### Privileged VM (Debian 13, kernel 6.12.116, QEMU TCG)
Control run (all with positive controls) — 5/5 PASS, exit 0:
- `TestLabDeniedTraffic` 15.71s
- `TestLabDefaultDrop` 16.29s
- `TestLabQueueNoListener` 13.09s
- `TestStateUnsolicitedInbound` 5.44s
- `TestStateInvalidConnection` 13.17s

Full-suite run (suite6, pre-controls): 13/16 PASS (crash/restart,
readiness, TCP/UDP allow, deny, default-drop, no-listener, shutdown,
hook cleanup, and all four NAT scenarios). The 3 remaining
(NAT-reload, overflow, state timeout/hold) are TCG-timeout-bound; their
harness fixes are committed but not re-verified under TCG.

Environment limitation: QEMU TCG makes the long hold/timeout scenarios
impractically slow; the fixes (fresh listener per probe, daemon
readiness polling, 20s probe deadlines, single-consume listener
handling) are committed.

### Cleanup
`scripts/lab.sh down` verified clean after each run; no `gsh_*` netns,
veth, or nft state left; host/VM root netns untouched.

### GitHub Actions
Workflow prepared (`ubuntu-24.04`, tooling install, NFQUEUE+conntrack
preflight with a queue-rule probe in a netns, skip rejection).
**UNVERIFIED** — push requires GitHub authentication which is not
available in this environment (no `GH_TOKEN`, no `gh auth login`, no
credential helper, no SSH key).

## Draft PR status
**Not created** — blocked on GitHub authentication. Branch
`phase7-baseline` is 34 commits ahead of `origin/main`, ready to push.
Exact commands in `docs/release/phase9-ci-validation.md`.

## Remaining release blockers
1. GitHub auth → push branch → Draft PR.
2. GitHub CI must run green with **zero skips** (includes the three
   TCG-timeout-bound scenarios, which must pass on the fast runner).
3. Then tag `v0.1.0` (requires separate approval).
