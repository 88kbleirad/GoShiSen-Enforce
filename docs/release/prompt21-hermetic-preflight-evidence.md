# Hermetic Preflight Tests + Real Preflight Integration Gate — Evidence (Prompt 21)

**Date:** 2026-11-14 (UTC)

**Status:** PASS — the preflight state machine is fully decoupled from the
host: unit tests run against a hermetic fixture (empty PATH, no host tools,
no /proc reads), and a separate integration target exercises the *real*
preflight on a provisioned runner.

## 1. Problem (from Prompt 20 CI)

After prompt 20, `TestRunnerPreflightBlockedNoSudo` was fixed, but CI still
failed on the `build` job (ubuntu-latest, **no qemu in PATH**):

- `TestPreflightReady` → rc=3, `MISSING_DEPENDENCY qemu-system-x86_64`
  (expected READY).
- `TestPreflightPortOccupied` → rc=3, `MISSING_DEPENDENCY qemu-system-x86_64`
  (expected RESOURCE).
- `TestPreflightDiagnosticsStable` → stopped at qemu, never reached nft.

Root cause: the dependency gate scanned the *real host PATH* (`[ -x ... ]`)
inside unit tests. The old `E2E_MOCK_MISSING_TOOLS` mock could only mark
tools *absent*; it could never make the suite independent of what the host
actually installed. So the CI build runner's missing qemu leaked into every
test.

## 2. Fix — environment abstraction

`scripts/e2e-runner.sh` now implements a pure environment view:

- **`real_env`** (default): PATH tool lookup (searching the full system
  PATH, the same one CI passes via `sudo -E env PATH=$PATH`), uid/sudo
  probe, kernel-module loadability probe (`/sys/module` **or**
  `modprobe --dry-run --quiet` — exit 0 for loaded *and* loadable modules,
  no privileges needed), `/proc/net/netfilter/nfnetlink_queue` node check,
  and port probes (`/dev/tcp`, bash builtin — no external binaries).
- **`fixture_env`**: `E2E_PREFLIGHT_FIXTURE=<file>` loads the fixture into
  a bash associative array using **only builtins** (`read`/`case`/`[[ ]]`).
  Nothing outside the file is read and no external binary is executed; a
  fixture-driven run with an empty PATH proves it. Unknown/missing keys are
  a `CONFIG_ERROR` (a typo can never silently produce a wrong state).
  `--list-requirements` prints the single source of truth.

Gate order is unchanged and fixed (the prompt mandates it):
`CONFIG → PRIVILEGE → DEPENDENCY → RESOURCE → READY`.

Also: the environment manifest is now written **before** the preflight, so
a blocked run still leaves `environment-manifest.txt` behind (in fixture
mode the manifest is skipped — a unit test must not execute host probes).

## 3. Unit tests (tests/package/e2e_reproducibility_test.go)

All hermetic: a fixture file is the environment. Verified on a host and on
a qemu-less container with qemu/nft/iptables/sudo missing from PATH.

| Test | Fixture | Result | Exit |
| --- | --- | --- | --- |
| TestPreflightReady | all present | READY | 0 |
| TestPreflightPortOccupied | all present + port 5555 occupied | RESOURCE names 5555 | 3 |
| TestPreflightDiagnosticsStable | all present, nft absent | exactly one MISSING_DEPENDENCY naming nft + install remediation | 3 |
| TestRunnerPreflightBlockedNoSudo | no sudo + qemu absent | BLOCKED (privilege before tool) | 3 |
| TestPreflightMissingToolAfterPrivilege | qemu absent | MISSING_DEPENDENCY names qemu | 3 |
| TestPreflightConfigError | `--daemon ""` (malformed args) | CONFIG_ERROR | 3 |
| TestPreflightStateMachineTable | 15 sub-cases | every state × exit code + multi-fault precedence (config > privilege > dependency > resource) | - |

The table covers: ready, blocked, blocked-with-missing-tool, missing qemu,
missing nft, missing module nf_conntrack, resource 5555, resource 8088,
config error, precedence config-first, precedence blocked-before-tool,
precedence tool-before-resource, precedence module-before-resource,
precedence tool-before-module, precedence resource-last. Each asserts the
exact terminal state (exactly one state line) and that no later-gate state
appears.

## 4. Real preflight integration target (separate)

`tests/integration/preflight_test.go` (**build tag `preflight_integration`**)
runs the real preflight with **no fixture**:

- asserts `--list-requirements` prints the contract;
- runs `e2e-runner.sh --artifacts … E2E_PREFLIGHT_ONLY=1` and requires:
  - READY + rc 0 on a fully provisioned runner;
  - MISSING_DEPENDENCY + rc 3, naming the exact tool/module, with an
    install/modprobe remediation otherwise;
  - BLOCKED → explicit-reason skip (consistent with `checkPrereqs`; the
    privileged CI jobs reject skips, so it cannot silently pass there).

Makefile target: `make preflight-integration` (root-gated). Wired into the
privileged `openwrt-integration` CI job as a step **before** the runner,
with the same `PATH` the runner will use (`sudo -E env PATH=$PATH`).

This is deliberately a *separate* target: the unprivileged `build` job
keeps running the hermetic unit tests only, so CI image changes (qemu
removed/added) surface as **accurate diagnostics**, never as masked
unit-test passes.

## 5. Verification run (local workstation, CachyOS, no passwordless sudo)

- `go test ./tests/package -run 'Preflight|Runner' -count=1` — PASS.
- `go test ./tests/package -count=1` — PASS.
- `go test -race ./...` — PASS (all packages).
- `make check` — PASS (fmt, vet, test, static build).
- Hermetic proof: `env PATH=/usr/bin:/bin go test ./tests/package -run 'Preflight|Runner'`
  — PASS (PATH has no qemu/nft/iptables/sudo; fixtures replace them).
- Real preflight on this host (non-root) → BLOCKED + manifest written.

## 6. Verification run (docker golang:1.25 — no qemu/nft/ip/sudo)

- `go test ./tests/package -run 'Preflight|Runner' -count=1` — PASS (hermetic).
- Real preflight before provisioning → `MISSING_DEPENDENCY qemu-system-x86_64`
  + manifest, rc 3 (correct detection).
- After `apt-get install iproute2 nftables iptables qemu-system-x86 sudo kmod`
  → real preflight **READY**, rc 0.

## 7. Verification run (instabox sandbox — passwordless sudo, qemu present)

- Stale lab (ports 5555/8088 occupied, netns/links left over) → real
  preflight reported `RESOURCE: port 5555 is already in use` + manifest,
  rc 3 (correct detection of the *real* host).
- After cleaning stale lab resources → real preflight **READY**, rc 0.
- `go test -tags preflight_integration ./tests/integration/ -run '^TestPreflightReal$'`
  → PASS.

## 8. Safety

- No changes to tag v0.2.1, release assets, signing keys, or daemon runtime.
- No fake skips: the integration target's BLOCKED skip has an explicit
  reason and CI rejects skips; the unit tests never skip.
- No assertion was weakened: MISSING_DEPENDENCY is never accepted where
  READY/RESOURCE is required.
- Working tree clean after commit `f953239` on
  `feat/e2e-reproducibility` (PR #29, open).
