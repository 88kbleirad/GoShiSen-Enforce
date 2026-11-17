# Audit — self-review 2026-11-17

Branch `audit/self-review-20261117` off `origin/main` (`d887cb9`). Scope:
logic bugs, unhandled errors, race conditions, unsafe input handling,
weak/bypassable checks in the signed-tag gate and verify-release-assets,
missing failure-path tests, and docs that contradict the code.

## Findings

| ID | Severity | Location | Problem | Fix commit | Test | Status |
| --- | --- | --- | --- | --- | --- | --- |
| F-001 | high | `internal/policy/parser.go:148`, `internal/config/config.go:608` | `fmt.Sscanf(s, "%d", &n)` stops at the first non-digit, so `"80evil"`, `"12.5"`, `"1e3"` silently parse as 80, 12, 1. A policy `src_port: 80evil` compiles and accepts port 80; `GOSHISEN_API_PORT=8080x` binds 8080 instead of failing at startup. The CLI's `--source-port`/`--dest-port` (`strconv.Atoi` in `internal/cli/policycmd.go`) already rejected these, confirming the defect. | `8a786f2` (policy), `da3eaf7` (config) | `TestCompileRejectsNonIntegerPorts`, `TestLoadRejectsTrailingGarbagePortEnv` (both failed before, pass after) | fixed |
| F-002 | medium | `internal/packet/capture.go:446` | `runSocket`'s non-timeout `Receive` error branch reported the backend error as `"send"` and incremented `SendErrors`; the documented `kind=receive` metric (`docs/release/metrics-grafana-validation.md`) was unreachable. Also, a persistent socket error (e.g. netlink socket closed) spins the loop until context cancellation instead of surfacing the error. | `5067b90` | `TestRunSocketReceiveErrorKind` (failed before, passes after) | fixed (label + count) |

## Not fixed and why

- **F-003 (suspected): `runSocket` never exits on a persistent receive error.** A closed netlink socket yields EBADF on every `Receive`; the loop continues calling `b.noteError` at up to 2/s until `ctx` is cancelled (shutdown). This is not a fail-open defect — the daemon keeps enforcing with the existing queue and the fail-closed hook stays installed — but a wedged socket is not surfaced and the process cannot be restarted by the watchdog. Fixing it changes the run-loop control flow (a likely permanent receive error should terminate the loop so `Run` returns and the daemon restarts); left as suspected because it is intertwined with the queue bind/lifecycle semantics and the guardrails restrict release-critical changes without human review.
- **F-004 (suspected): checksums verification in `scripts/verify-release-assets.sh` only filters `goshisen-linux-(amd64|arm64).tar.gz` lines.** Verified fail-closed for those two patterns, but any *other* CLI asset (e.g. an `sbom.spdx.json` or an extra tarball) is not covered by `checksums.txt`; the full-set verification is delegated to `verify-signatures.sh` (signature + provenance + sha256), which is the intended design. Documented, not changed.
- **Signed-tag gate semantics unchanged by design (guardrail).** No changes to `scripts/release-pin-gate.sh` step 6, `verify-signatures.sh`, `apk-verify.sh`, or `verify-release-assets.sh`. The gate is opt-in (`GATE_TAG_SIGN_CHECK=1`), fails closed on unsigned/forged keys, and does not block unsigned historic tags when off — verified against v0.2.3–v0.2.5 in the prior follow-up.
- **`internal/hook` `TestInstallProgramIsAtomic` fails on this host** (`Could not process rule … queue to 0` at nft syntax check). Reproduced on `origin/main` with no local changes: host kernel lacks `NFT_QUEUE` (module absent; `nft -c` fails identically for both `queue to N` and `queue num N`). Environmental, passes in CI (ubuntu-latest). No code change.

## Test plan (actual output)

```
# Before fixes (failing tests)
$ go test ./internal/policy/ -run TestCompileRejectsNonIntegerPorts -v
    parser_test.go:26: Compile accepted "src_port: 80evil"; want validation error
    ... (6 subtests fail)
FAIL  github.com/88kbleirad/GoShiSen-Enforce/internal/policy
$ go test ./internal/config/ -run TestLoadRejectsTrailingGarbagePortEnv -v
    config_test.go:483: Load accepted GOSHISEN_API_PORT="8080x"; want validation error
FAIL  github.com/88kbleirad/GoShiSen-Enforce/internal/config
$ go test ./internal/packet/ -run TestRunSocketReceiveErrorKind -v
    capture_test.go:372: receive-side failure reported as "send"; want "receive"
FAIL  github.com/88kbleirad/GoShiSen-Enforce/internal/packet

# After fixes
$ go test ./internal/policy/ ./internal/config/ ./internal/packet/ -count=1
ok   github.com/88kbleirad/GoShiSen-Enforce/internal/policy
ok   github.com/88kbleirad/GoShiSen-Enforce/internal/config
ok   github.com/88kbleirad/GoShiSen-Enforce/internal/packet

# Full suite (race)
$ go test -race -count=1 ./...
ok  ... internal/metrics, internal/packet, internal/policy, tests/integration,
    tests/openwrt_integration, tests/package (all pass)
FAIL  internal/hook — TestInstallProgramIsAtomic only; environmental
    (host kernel lacks NFT_QUEUE; reproduced on origin/main unchanged)

# Formatting / vet / build
$ gofmt -l .   # (empty)
$ go vet ./...  # clean
$ go build ./... # clean
```
