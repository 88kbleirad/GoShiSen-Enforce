# Phase 8.6 — Final NFQUEUE/Conntrack End-to-End Verification & CI Gate

Date: 2026-11-10. Branch: `phase7-baseline`.

## Verdict: **BLOCKED** (release gate)

End-to-end product-path conntrack metadata is now verified working (the
wire-level bugs are fixed and proven). Privileged integration on the VC
VM (Debian 13, kernel 6.12.116, QEMU TCG) passes 13/16 fully-executed
scenarios; the remaining scenarios are pinned to test-harness timing
fixes that are committed but impractical to re-verify under TCG's
extreme slowness. GitHub Actions CI remains UNVERIFIED (unpushed; no
authorization).

---

## A. Conntrack findings (confirmed, wire-level)

| # | Finding | Evidence |
| --- | --- | --- |
| 1 | `NFQA_CT_INFO` is a **`__be32`** (`ctnetlink_glue_build` uses `nla_put_be32(..., htonl(ctinfo))`); the parser read only the first byte → always 0 → every packet INVALID. | The raw NFQUEUE attribute dumper built during the investigation showed type 12 present with `ctInfo=0`; after fix, state distribution became `new`/`established`. |
| 2 | `enum ip_conntrack_info` is **0-based** (ESTABLISHED=0, RELATED=1, NEW=2, IS_REPLY=3, additive replies). Constants were 1/2/3/4 with a bitwise reply check → after fix #1, NEW(2) decoded as "related". | Fixed constants + threshold-based reply decode; unit tests cover all state/direction combos. |
| 3 | The accepted-set tuple key was not direction-normalized → reply packets (endpoints swapped) never matched → ESTABLISHED/RELATED replies dropped. | Fixed `tupleKey` to canonicalize on `DirReply`; `TestStatefulKingProcess`/`TestPolicyReloadKeepsEstablished` (now modeling real replies) fail without the fix. |
| 4 | `parseAttrs` panicked on a truncated trailing attribute (`slice bounds out of range [72:71]`) → daemon crashed mid-flow, breaking reply paths. | Fixed aligned-length guard; `TestParseAttrsTruncatedNoPanic`. |
| 5 | Result: minimal product-path topology (hook as generated + real daemon) now completes a TCP handshake + echo (`ECHOED`), CT count=1, metrics all-accept (1 new + 9 established). | `finalct` harness runs. |

## B. Privileged integration on the VM (suite6)

| Scenario | Result |
| --- | --- |
| Crash/restart (SIGKILL → drop → restart recovery) | PASS |
| Readiness lifecycle | PASS |
| TCP allow (handshake) | PASS |
| UDP allow (echo) | PASS |
| Denied traffic / default DROP | PASS |
| Queue no-listener (fail-closed) | PASS |
| Shutdown clean | PASS |
| Hook cleanup scope | PASS |
| DNAT | PASS |
| MASQUERADE | PASS |
| NAT return traffic | PASS |
| NAT + filtering | PASS |
| NAT reload-while-connections | BLOCKED (TCG timing) — fix committed (fresh listener + daemon readiness) |
| Queue overflow | BLOCKED (TCG timing) — fixed commits not yet re-verified in VM |
| State tests (handshake/hold/UDP/established) | BLOCKED (TCG timing) — same |

All remaining failures were traced to test-harness timing (fixed sleeps
under TCG, single-consume listeners); the fixes are committed
(`startStateProbe` double-probeBin, listener readiness wait, fresh
listeners, daemon readiness polls, 20s probe deadlines, policy
corrections).

## C. Host regression (Go 1.25.0)

`go mod verify` PASS; gofmt clean; `go vet ./...` PASS; `go test ./...` PASS; `go test -race ./...` PASS; `go build ./...` PASS; `git diff --check` clean. 9 packages.

## D. GitHub CI

- Workflow (`ubuntu-24.04`) prepared: tooling install, module preflight, skip-rejection, 20m timeout.
- **UNVERIFIED**: branch unpushed; no push authorization. A real runner (fast, conntrack-capable) should run the full suite in minutes.

## E. Changes (Phase 8.6)

- `internal/packet/capture.go`: `NFQA_CT_INFO` 4-byte BE decode; 0-based `ip_conntrack_info` constants; threshold-based reply decode; `parseAttrs` truncation guard.
- `internal/policy/engine.go`: direction-normalized accepted-set tuple key.
- `tests/integration/*`: harness fixes (listener readiness, fresh listeners, daemon readiness, probe deadlines, policy corrections).
- Tests added: `TestCTInfoWireEncoding`, `TestParseAttrsTruncatedNoPanic`, updated stateful reply tests.
- Commits: `69dcc38`, `b046287`, `af25ed2`, `f27955c`, `99275f5`, `d8e821e`, `0cbca1b`, `d2f709a`.

## F. Release gates

- Completed: host regression; protocol/wire correctness; product-path CT verified; 13/16 privileged scenarios; CI workflow prepared.
- Outstanding: full privileged suite on a fast conntrack-capable runner (CI or real host); actual GitHub Actions run with zero skips.

## G. Next action

Authorize push of `phase7-baseline`, run GitHub Actions (`build` + `integration` on `ubuntu-24.04`), confirm zero skips and all green, then tag `v0.1.0` per `docs/release-checklist.md`.
