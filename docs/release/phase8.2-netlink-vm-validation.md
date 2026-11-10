# Phase 8.2 — Netlink Protocol Verification & VM Integration

Date: 2026-11-10. Branch: `phase7-baseline`.

## Verdict: **PENDING VM integration results**

---

## A. Netlink Protocol Audit

| Component | Result | Evidence |
| --- | --- | --- |
| NFQUEUE constants | **PASS** | Two defects found and fixed: config attrs were shifted +3 (`nfqaCfgCmd=4` → UAPI 1, `nfqaCfgParams=5` → 2, `nfqaCfgQueueMaxlen=6` → 3) and `nfqaPayload=3` → UAPI 10 (`NFQA_PAYLOAD`; 3 is `NFQA_MARK`). Pinned by `TestNFQNLAttrNumbersMatchUAPI` against `nfnetlink_queue.h`. |
| Message framing | **PASS** | `nlmsghdr`(16) + `nfgenmsg`(4: family/version/res_id BE) + NLA attrs (len incl. header+4B alignment, type BE). Golden `TestConfigMessageGolden`, `TestAttrEncoding`, `TestNFGenmsgMarshal`. |
| Verdict encoding | **PASS** | `NFQNL_MSG_VERDICT`((3<<8)|1), `NFQA_VERDICT_HDR`(2) with `{be32 verdict, be32 id}`; queue in nfgenmsg res_id. Golden `TestVerdictMessageGolden`. |
| Conntrack attributes | **PASS** | `NFQA_CT`=11, `NFQA_CT_INFO`=12, nested `CTA_PROTOINFO`(4)→`CTA_PROTOINFO_TCP`(2)→`CTA_PROTOINFO_TCP_STATE`(1) — verified against `nfnetlink_conntrack.h`; golden `TestCTAttrsGolden`, `TestApplyCTInfo`. |
| Endianness and alignment | **PASS** | Lengths are native (host x86-64 LE), payload fields big-endian per UAPI; 4-byte attribute alignment (NLA_ALIGN). Golden tests cover both. |
| Kernel compatibility | **PENDING** | Requires the live NFQUEUE-capable VM (run 7). The prior runs proved the daemon builds and the lab comes up; the bind failures were caused by the attribute-type defects above. |

## B. Host Regression

Toolchain: **Go 1.25.0** (declared floor; `GOTOOLCHAIN=go1.25.0`).

| Check | Result |
| --- | --- |
| `go mod verify` | PASS |
| `gofmt -l .` | clean |
| `go vet ./...` | PASS |
| `go test ./... -count=1` | PASS (all packages) |
| `go test -race ./... -count=1` | PASS |
| `go build ./...` | PASS |
| `git diff --check` | clean |

## C. VM Integration

(Run 7 in progress — see `docs/release/phase8.2-netlink-vm-validation.md` update once complete.)

## D. Fixes Applied

- `internal/packet/capture.go`: correct `nfqaCfgCmd/Params/QueueMaxlen`, `nfqaPayload` to UAPI values.
- `internal/packet/netlink_test.go` (new): byte-level golden tests + UAPI constant pins + fail-open flag invariant.
- Earlier (Phase 8.1): hook-subcommand parse fix, probe-binary lifecycle, in-ns waitForPort, log-polled daemon readiness, probe timeouts (see commit `8f797bd`).

## E. Remaining Blockers

- VM integration results (run 7).
- CI (GitHub) verification still requires push authorization.
