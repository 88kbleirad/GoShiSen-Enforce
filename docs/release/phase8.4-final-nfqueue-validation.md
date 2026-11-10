# Phase 8.4 — Final NFQUEUE Fix, Regression & Release Gate Validation

Date: 2026-11-10. Branch: `phase7-baseline`.

## Verdict: **PENDING privileged integration results** (final VM run in progress)

---

## A. Root Cause Confirmation

| Item | Value |
| --- | --- |
| Original failure | All privileged tests failed at `nfqueue: bind queue 0: netlink receive: no such device` (ENODEV); `/proc/net/netfilter/nfnetlink_queue` showed queue 0 after BIND but empty after the second init message. |
| Original command bytes | The second init message sent command byte **2** (payload `02 00 00 02`). |
| Linux UAPI mapping | `NFQNL_CFG_CMD_NONE=0`, `BIND=1`, **`UNBIND=2`**, **`PF_BIND=3`**, `PF_UNBIND=4`. |
| Defect | `nfqnlCfgCmdPfBind` was defined as **2** (UNBIND), not 3 (PF_BIND). The "PF_BIND" step therefore sent UNBIND, which destroyed the queue instance created by BIND. |
| Failure mechanism | BIND creates queue → UNBIND destroys it → PARAMS/MAXLEN/FLAGS all hit `!queue → -ENODEV`. |
| Confirmation | Standalone probe (byte-level) in Debian 13 VM: with the erroneous second message, BIND succeeds (queue listed) then queue vanishes and PARAMS fails ENODEV. Without it, BIND→PARAMS→MAXLEN→FLAGS all ACK successfully (queue listed, copy_mode=2). |

## B. Final implementation change

Two independent defects were found and fixed by privileged execution:

1. **Config command byte** (bind phase): `nfqnlCfgCmdPfBind` was 2 (= UAPI UNBIND) instead of 3 (= PF_BIND); the init sequence sent UNBIND and destroyed the queue. Fixed: corrected constants (`nfqnlCfgCmdUnbind=2`, `nfqnlCfgCmdPfBind=3`), `configure()` now uses `configMessages()` and sends only BIND → PARAMS → MAXLEN → MASK+FLAGS.
2. **Receive-path framing** (packet phase): the kernel frames NFQUEUE packet messages as `nlmsghdr + nfgenmsg(4) + attrs`, but `handlePacket` passed `m.Data` (nfgenmsg + attrs) to `evaluate`, which parsed attributes from offset 0 — reading the nfgenmsg as garbage attribute headers, so `NFQA_PACKET_HDR`/`NFQA_PAYLOAD` were never found and **every packet was dropped**. Fixed: new `packetAttrs()` helper strips the 4-byte nfgenmsg before attribute parsing.

Earlier fixes in this thread (same root causes class): native-endian `struct nlattr` header encoding, `NLM_F_ACK` request flag, UAPI attribute type numbers, `__be16` pf encoding.

### Regression tests added

- `internal/packet/netlink_test.go`: message type/nfgenmsg/attr golden bytes (native endian), config BIND/PARAMS/verdict goldens, nested conntrack attrs, UAPI constant pins, config-sequence golden (4 messages, hard-coded bytes, no UNBIND/PF_BIND, no FAIL_OPEN).
- `internal/packet/capture_test.go`: `TestHandlePacketKernelFraming` (real kernel framing through `packetAttrs`+`evaluate`).
- `internal/packet/qstats_test.go`, `internal/metrics`, `internal/config`, `internal/app`, `internal/cli`: Phase 7 additions (unchanged).

### Debugging code removed

Temporary `debugWire` instrumentation and the `tests/bindprobe` diagnostic program were deleted; production logging is back to default (no wire dumps).

## C. Host validation (Go 1.25.0)

`go mod verify` PASS; `gofmt -l .` clean; `go vet ./...` PASS; `go test ./... -count=1` PASS; `go test -race ./... -count=1` PASS; `go build ./...` PASS; `git diff --check` clean.

## D. VM privileged integration

(Final run in progress — results appended.)

## E. Remaining blockers

- Final VM integration results.
- GitHub Actions CI (branch unpushed; no push authorization).
