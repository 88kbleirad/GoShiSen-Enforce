# Phase 9.2 — CI Validation Evidence (GitHub Hosted)

Branch: `phase7-baseline`
HEAD: `f1f09c1a5db024d33ea5d4c2730363bd900c7170` (tag-trigger CI commit; verified remote)
Prior verified HEAD: `e50602e2f66db3f90d46ce07b5936cbb1d67798a` (21/21 integration PASS)
Draft PR: [#1](https://github.com/88kbleirad/GoShiSen-Enforce/pull/1) — `test: harden fail-closed integration coverage for v0.1.0`
CI workflow runs: `42817678370` (PR @ `5bd1c57`) / `42817666527` (push @ `5bd1c57`); tag-trigger runs `42819392387` / `42819383699` @ `f1f09c1`
Date: 2026-11-10.

## Host regression (Go 1.25.0, local)
`go mod verify` PASS; `gofmt -l .` clean; `go vet ./...` PASS; `go test ./... -count=1` PASS; `go test -race ./... -count=1` PASS; `go build ./...` PASS; `git diff --check` clean.

## GitHub-hosted CI (ubuntu-24.04)
- Runner kernel: `7.0.0-1017-azure` (`CONFIG_NETFILTER_NETLINK_QUEUE=m`, `CONFIG_NFT_QUEUE=m`, `CONFIG_NF_CONNTRACK=m`, `CONFIG_NET_NS=y`).
- Go toolchain: `1.25.14` (setup-go cache hit).
- NFQUEUE + conntrack preflight: PASS (queue rule installed in throwaway netns; `nf_conntrack_count` non-zero).

### F1. Standard validation (build job) — PASS
Verify modules, formatting, vet, `go test -race ./...`, build, CLI smoke (help/version/missing-config nonzero) all green.

### F2. Privileged integration — 21/21 PASS, zero skips
| # | Test | Result | Duration |
|---|---|---|---|
| 1 | TestCrashKillFailClosedAndRestart | PASS | 12.79s |
| 2 | TestReadinessFalseUntilEnforced | PASS | 1.62s |
| 3 | TestLabAllowedTCP | PASS | 1.80s |
| 4 | TestLabAllowedUDP | PASS | 1.82s |
| 5 | TestLabDeniedTraffic | PASS | 9.58s |
| 6 | TestLabDefaultDrop | PASS | 10.52s |
| 7 | TestLabQueueNoListener | PASS | 9.82s |
| 8 | TestLabShutdownClean | PASS | 9.83s |
| 9 | TestHookCleanupScope | PASS | 1.60s |
| 10 | TestNATDnat | PASS | 2.55s |
| 11 | TestNATMasquerade | PASS | 2.50s |
| 12 | TestNATReturnTraffic | PASS | 2.51s |
| 13 | TestNATWithFiltering | PASS | 2.51s |
| 14 | TestNATReloadWhileConnections | PASS | 18.47s |
| 15 | TestQueueOverflowKernelDrop | PASS | 23.70s |
| 16 | TestStateTCPHandshakeAndReply | PASS | 1.83s |
| 17 | TestStateUnsolicitedInbound | PASS | 2.32s |
| 18 | TestStateInvalidConnection | PASS | 9.52s |
| 19 | TestStateConnectionTimeout | PASS | 2.82s |
| 20 | TestStateUDPTimeout | PASS | 1.81s |
| 21 | TestStateReloadWhileEstablished | PASS | 18.48s |

### F3. Mandatory skip detection — enforced
Workflow runs `go test -v`, streams output, and fails the job if any `--- SKIP` line appears. On the green run: 0 test-level skips. A green workflow without mandatory tests is therefore impossible by construction.

## The three previously-unverified scenarios (Phase 8.7 leftovers)
| Scenario | Result | Evidence |
|---|---|---|
| NAT-reload | PASS | TestNATReloadWhileConnections 18.47s: hold probe stays connected across SIGHUP policy reload; final exchange completes (HELD); NEW denied after reload. |
| Queue overflow | PASS | TestQueueOverflowKernelDrop 23.70s: SIGSTOP daemon, 2000-datagram UDP flood with queue_maxlen=4; kernel `queue_dropped` grows; overflow warning logged; allowed TCP still works and denied UDP still dropped after resume. |
| State timeout/hold | PASS | TestStateConnectionTimeout 2.82s + TestStateUDPTimeout 1.81s + TestStateReloadWhileEstablished 18.48s: liveness under hold, reconnect allowed, second UDP exchange on same tuple (ESTABLISHED) allowed, established flow survives reload while NEW is denied. |

## CI failure history (fixes)
1. Run `42810672781` – zero output, exit 1 at 10m: GitHub Actions terminates steps that emit no output ~10m; test output was captured in a shell var, never streamed. Fixed: stream via `tee` + explicit `-timeout 25m` (commit `aa14811`).
2. Run `42812300831` – `TestNATReloadWhileConnections` hung in `t.Cleanup` at `nat_test.go:376`: `<-holdDone` block on a one-shot channel already consumed by the test body. Fixed: bounded cleanup wait (commit `dc140c0`).
3. Run `42816127151` – three state tests failed: probe listeners (`listen-echo`, `listen-echo-report`, `listen` UDP) served exactly one exchange and exited, so reconnects/second exchanges got ECONNREFUSED or i/o timeout. Fixed: listeners loop until killed (commit `884e87b`).
4. `TestStateReloadWhileEstablished` asserted a NEW connection succeeds after reload to ESTABLISHED-only policy — wrong expectation; rewrote to hold the established flow across reload and assert NEW is dropped (commit `e50602e`).

## Verdict: GREEN
All mandatory CI jobs pass; 21/21 privileged scenarios pass; zero mandatory skips; fail-closed semantics preserved (no test weakened or skipped to green). Release gate for v0.1.0: **READY** pending release checklist §3–§5 and explicit merge/tag authorization.
