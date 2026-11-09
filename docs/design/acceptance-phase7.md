# Final Acceptance Report — GoShiSen-Enforce (Phase 7 roadmap)

Branch: `phase7-baseline`. Commits: `edb38b6` (0+1), `4097e05` (2), `7b9f478` (4), `ae9eee6` (5). Environment: unprivileged (uid 1000, no `nft`, no CAP_NET_ADMIN) — all privileged integration runs remain environment-gated and are **not** claimed as passed.

## Verification evidence

| Gate | Result |
| --- | --- |
| `gofmt -l .` | clean |
| `go mod verify` | pass (cold clone included) |
| `go vet ./...` | pass |
| `go test -race ./...` | pass — all packages (integration tests skip with explicit non-root reason) |
| `go build ./...` | pass (cold clone included) |
| `make check` stages | all pass (fmt/vet/test/build) |
| CLI smoke (version, config check, policy validate, policy simulate) | verified with real binary; `--state new` documented |
| OpenAPI vs routes | match (no API change in Phase 7) |
| Cold clone (git archive → fresh checkout) | `go mod verify`, gofmt, vet, race tests, build all pass |

## Phase status

| Phase | Status |
| --- | --- |
| 0 Baseline & toolchain | **done** — report in `docs/design/baseline-phase7.md`; CI Go 1.22→1.25 (2 jobs); doc floors → 1.25; no uncommitted files at baseline |
| 1 Queue overflow | **done (code+tests)** — kernel procfs stats parser, bounded `goshisen_queue_overflow_total{cause=kernel_drop|user_drop}`, 1 s watcher with rate-limited WARN, `queue_maxlen` config/env/flag, fail-open invariant unit test, root-gated `overflow_test.go` |
| 2 Crash/restart | **done (code+tests)** — readiness gated on hook+queue (cleared at shutdown; unit tested), SIGKILL fail-closed + restart recovery integration test, systemd unit + lab supervisor loop |
| 3 Privileged suite | **environment-gated** — prereqs checked (root/nft/netns/Go); lab script refuses cleanly as non-root; all integration tests skip with explicit reasons; CI has a privileged job |
| 4 Security review | **done** — 9/9 PASS; 2 low notes fixed (typed backend-error enum; sample config token removed). Regression coverage present |
| 5 Docs & release | **done (code/docs)** — architecture reconciled (conntrack mirror claims removed), user/developer guides, README, SECURITY.md, OpenAPI verified, release checklist created, cold-clone build passes |

## Remaining before release (required by checklist)

- Run `sudo scripts/lab.sh up && sudo make integration && sudo scripts/lab.sh down` on a privileged host; verify all 21 integration tests (S1–S12 + NAT + overflow + crash) pass and teardown is clean.
- Tag `v0.1.0` only after the privileged suite passes (per `docs/release-checklist.md`).

## Notable design decisions

- Overflow stats come from `/proc/net/netfilter/nfnetlink_queue` (namespace-scoped; the only kernel API exposing `queue_dropped`/`queue_user_dropped`) — not a netlink attribute (none exists).
- Fail-closed is structural: the backend never sets `NFQA_CFG_F_FAIL_OPEN`; the invariant is pinned by test.
- Readiness moved after queue bind; the hook installs first, so the firewall fails closed throughout startup.
- No mocks claim kernel behavior: parser tests use synthetic netlink bytes; watcher tests use synthetic proc lines; integration tests use real traffic in real namespaces.
