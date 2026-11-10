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

- Constants: `nfqnlCfgCmdUnbind=2` added; `nfqnlCfgCmdPfBind` corrected 2→3.
- sendconfigure()` now builds exactly the 4-message sequence via `configMessages()`: BIND → PARAMS → MAXLEN → MASK+FLAGS. No PF_BIND/PF_UNBIND/UNBIND is sent (PF_* are obsolete no-ops; the old byte was UNBIND).
- Added `TestConfigSequenceGolden` (hard-coded UAPI bytes), `TestConfigCommandValuesUAPI` (1/2/3 pins), `TestConfigSequenceNoUnbind` (rejects UNBIND/PF_BIND in the sequence).
- Removed temporary `debugWire` instrumentation and the diagnostic probe.

## C. Host validation (Go 1.25.0)

`go mod verify` PASS; `gofmt -l .` clean; `go vet ./...` PASS; `go test ./... -count=1` PASS; `go test -race ./... -count=1` PASS; `go build ./...` PASS; `git diff --check` clean.

## D. VM privileged integration

(Final run in progress — results appended.)

## E. Remaining blockers

- Final VM integration results.
- GitHub Actions CI (branch unpushed; no push authorization).
