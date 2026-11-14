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
- Working tree clean after commits `f953239` → `fd5454a` → `cafe48c` on
  `feat/e2e-reproducibility` (PR #29, open).

## 9. Push / CI evidence status (Prompt 22)

The three prompt-21 commits are **local only**; PR #29's head is still the
prompt-20 commit `36dc861`:

| Item | Value |
| --- | --- |
| PR | https://github.com/88kbleirad/GoShiSen-Enforce/pull/29 (open, mergeable=true) |
| PR head (remote) | `36dc861` (prompt 20; pre-fix) |
| Local HEAD | `cafe48c` (3 commits ahead: f953239, fd5454a, cafe48c) |
| Remote branch | `origin/feat/e2e-reproducibility` at `36dc861` |
| Working tree | clean |

**CI on the remote head `36dc861` (pre-fix, documented as baseline):**
- Run 43484497691 (pull_request): `build, vet, test` **failure** at step 7
  "Test"; `package`, `openwrt e2e`, `integration` all success.
- Run 43484486820 (push, same SHA): identical — build/test fails.
- This is exactly the prompt-21 defect: the unit tests hit the real host
  PATH (no qemu on the ubuntu build runner). The failing step is the `go
  test` suite, not the privileged jobs.

**Push/PR completion is BLOCKED by credentials** (verified 2026-11-14T01:35Z):
- `gh auth status` → 401 "Bad credentials" for the keyring token and the
  `GH_TOKEN` env token.
- `git credential fill` returns a `ghp_` token that also returns 401 on
  `api.github.com/user` and on the git wire protocol.
- `ssh -T git@github.com` → "Permission denied (publickey)"; no SSH key is
  registered on the account and `ssh-add -l` shows no agent keys.
- The instabox sandbox's `gh` token is likewise 401.

**User action required (one of):**
- Run `gh auth login` locally (browser/device flow) to restore the keyring
  token, or export a fresh `GH_TOKEN`, then:
  ```sh
  cd ~/GoShiSen-Enforce
  git push origin feat/e2e-reproducibility   # 3 commits
  gh pr checks 29 --watch                     # confirm all checks green
  ```
- Or register an SSH key: `ssh-keygen` (if needed), add
  `~/.ssh/id_ed25519.pub` at https://github.com/settings/keys, then push
  over SSH.

Until that completes, the CI evidence for the prompt-21 code is the local
and sandbox verification in sections 5–7; it has **not** been produced by
GitHub Actions yet.

## 10. Prompt 23 — CI regression root cause + re-verification

**Symptom:** PR #29's `build, vet, test` job fails at step "Test" on the
remote head `36dc861` (`TestPreflightReady` → MISSING_DEPENDENCY qemu;
`TestPreflightPortOccupied` → MISSING_DEPENDENCY qemu; `TestPreflightDiagnosticsStable`
→ stops at qemu). Runner: Ubuntu 26.04 without QEMU.

**Root cause (verified by direct comparison):**
- CI/PR #29 head is `36dc861` (prompt-20 code). It contains the *old*
  mock-based preflight (`E2E_MOCK_SUDO` / `E2E_MOCK_MISSING_TOOLS` /
  `E2E_MOCK_PORT_OCCUPIED`), which scans the **real host PATH** for tools.
- The prompt-21 hermetic fix is **local only**: commits `f953239`,
  `fd5454a`, `cafe48c`, `8ffccc8` on `feat/e2e-reproducibility` were never
  pushed (credentials were 401/invalid at push time).
- Not a workflow-checkout bug, not a fixture-parse bug, not a regression in
  the fix itself: the fix simply never reached the remote.

**Local re-verification at HEAD `8ffccc8` (all PASS):**
- `go test ./tests/package -run 'Preflight|Runner' -count=1`
- `go test ./tests/package -count=1`
- `go test -race ./...`
- `make check`
- `PATH=/usr/bin:/bin go test ./tests/package -count=1` (hermetic)
- `docker run golang:1.25` (no qemu/nft/ip/sudo): unit tests PASS
- `go test -tags preflight_integration -run '^TestPreflightReal$' ./tests/integration`
  → BLOCKED (no privilege) with explicit reason — valid outcome, not a
  unit-test failure.

**Action taken:** device-flow auth initiated but never completed on the
browser side; the user then supplied a fresh `GH_TOKEN` directly. With it,
the branch was pushed (2026-11-14):
- `git push origin feat/e2e-reproducibility` → `36dc861..7177f92`
- PR #29 head is now `7177f92` (mergeable), URL:
  https://github.com/88kbleirad/GoShiSen-Enforce/pull/29
- CI runs for HEAD `7177f92`: push run 43542804700, PR run 43542808652
  — both **completed, all jobs SUCCESS** (build/vet/test, package,
  openwrt e2e, integration). PR #29 check rollup: 8/8 SUCCESS.
  - The `openwrt-integration` job ran step "Real preflight gate (same
    PATH as the runner)" (step 6) and it passed before step 7 (the
    runner: provision → boot → e2e). Verified from the job step listing.
  - Check-run links (run 43542804700 / 43542808652):
    build/vet/test 132287001844 / 132287014286; integration
    132287001773 / 132287014340; openwrt e2e 132287001586 /
    132287014264; package 132287001737 / 132287014069.
  - Note: the earlier pre-fix runs on `36dc861` (43484497691 /
    43484486820) failed exactly at `build, vet, test` step "Test".
  - **Follow-up real-environment fix:** the original prompt-21 patch also
    hard-gated port 8088, which is *not* a lab port (lab binds 5555
    serial + 2222 SSH forward). On a shared host with an unrelated
    process on 8088 the real preflight wrongly reported RESOURCE. Fixed
    in commit `f8adc68`: the port gate checks only 5555/2222 (fixture
    keys and table case updated), and `TestPreflightReal` accepts
    RESOURCE as a valid environment-consistent outcome naming the
    occupied lab port. Re-verified on instabox (foreign 8088 ignored →
    READY; tagged integration PASS).

**Final state (2026-11-14, PR #29 head `b961c62`):**
- Push run 43543876296 + PR run 43543882131: **both completed, success**.
- Check rollup on the PR: 8/8 **SUCCESS** (build/vet/test ×2, package ×2,
  openwrt e2e ×2, integration ×2), MERGEABLE.
- `openwrt-integration` job step "Real preflight gate (same PATH as the
  runner)" ran and passed before the runner step on the final head.
- Working tree clean; local == remote at `b961c62`.

**Final state after final docs push (head `a01aeec`, 2026-11-14):**
- Push run 43544252146 + PR run 43544258606: **both completed, success**.
- PR #29 check rollup at head `a01aeec`: 8/8 **SUCCESS**, MERGEABLE.
- One transient infra flake observed earlier: PR run 43543591396 (head
  `f8adc68`) failed in `TestOpenWrtE2E` with "daemon not ready (no
  NFQUEUE bound)" — a QEMU TCG cold-boot timeout (daemon wait window
  300s) on the CI host; the identical code passed on the neighboring run
  (43543586726 push, 43543882131 PR) and on the final heads. No code
  change was made for it; it did not recur.
