# Typed Preflight State Machine — Evidence (Prompt 20)

**Date:** 2026-11-14 (UTC)
**Status:** PASS — preflight is deterministic and injectable; the
`BlockedNoSudo` CI failure is fixed; BLOCKED / MISSING_DEPENDENCY /
CONFIG_ERROR / RESOURCE / READY are distinguished by test.

---

## 1. Problem (as reported)

CI `build, vet, test` failed:

```
TestRunnerPreflightBlockedNoSudo
expected BLOCKED message:
[e2e-runner] ERROR: required tool 'qemu-system-x86_64' missing ... 3
```

Root cause: the preflight checked the tool set before the privilege gate, and
the test fixture could not control the host's real tools (the CI build runner
has no qemu). The result depended on the host.

## 2. Fix (scripts/e2e-runner.sh)

Preflight is now a typed, deterministic state machine with a stable output
schema and injectable gates:

```
[e2e-runner] CONFIG_ERROR: <reason>
[e2e-runner] BLOCKED: <reason>
[e2e-runner] MISSING_DEPENDENCY: <reason>
[e2e-runner] RESOURCE: <reason>
[e2e-runner] READY: privileged e2e preflight passed
```

Gate order (fixed): CONFIG (args/input) → PRIVILEGE (root, else passwordless
sudo) → DEPENDENCY (curl/wget/python3/qemu/nft/iptables/ip/sudo) → RESOURCE
(ports; host NFQUEUE absence is a note) → READY.
Exit 3 covers the environment class (caller-compatible); the token line is
authoritative.

Test injection (no host-tool dependency): `E2E_MOCK_SUDO=no|yes`,
`E2E_MOCK_MISSING_TOOLS`, `E2E_MOCK_PORT_OCCUPIED`, `E2E_MOCK_CONFIG`,
`E2E_PREFLIGHT_ONLY=1`.

## 3. Regression tests (tests/package/e2e_reproducibility_test.go)

| Test | Mock | Expected |
| --- | --- | --- |
| TestPreflightNoSudoBlockedBeforeMissingTool | MOCK_SUDO=no | BLOCKED, exit 3, no MISSING_DEPENDENCY |
| TestPreflightMissingToolAfterPrivilege | MOCK_SUDO=yes + missing qemu | MISSING_DEPENDENCY names qemu |
| TestPreflightReady | MOCK_SUDO=yes + PREFLIGHT_ONLY | READY, exit 0 |
| TestPreflightPortOccupied | MOCK_PORT_OCCUPIED=8088 | RESOURCE names port |
| TestPreflightConfigError | MOCK_CONFIG=bad | CONFIG_ERROR |
| TestPreflightDiagnosticsStable | missing nft | exactly one MISSING_DEPENDENCY line naming nft |

Also kept: manifest text/JSON, pins, lab cleanup-safety, script syntax.

## 4. Validation

- `go test ./tests/package -run 'Preflight|EnvManifest|ManifestCaptures|LabScript' -count=1` — PASS.
- `go test ./... -count=1` — PASS; `make check` — PASS.
- Simulated CI build runner (PATH without qemu):
  - no-sudo → `BLOCKED` (exit 3);
  - sudo-ok → `READY` (exit 0).
- **Privileged OpenWrt e2e (instabox)**: `READY: privileged e2e preflight passed`
  → full e2e PASS (DROP/ACCEPT counter identity + gate tests) → cleanup rc=0.

## 5. Migration/exit-code note

Exit 3 remains the environment class; callers (CI job, docs) keep working. The
state token is the authoritative classifier, added without breaking existing
exit-code expectations.
