# Phase 9 — CI Validation Evidence

Branch: `phase7-baseline` (HEAD `40fa5b3`, 33 commits ahead of `origin/main`).
Date: 2026-11-10.

## Push / CI status

**BLOCKED at push**: the environment has no GitHub authentication (no `GH_TOKEN`, no `gh auth login`, no credential helper, no SSH key). `git push --dry-run` fails with "could not read Username ... terminal prompts disabled". The branch is ready to push; CI has **not executed** (UNVERIFIED). No workflow run IDs exist.

Required to proceed (user action):

```bash
# Option A: provide a token (repo scope), then:
gh auth login --with-token < token.txt
git push -u origin phase7-baseline
gh pr create --draft --base main --head phase7-baseline \
  --title "test: harden fail-closed integration coverage for v0.1.0" \
  --body "Positive controls added to deny/fail-closed scenarios; NFQUEUE+conntrack preflight; see docs/release/phase8.7-positive-controls-ci.md"

# Option B: user runs the push + PR themselves with the same commands.
```

## Local evidence (all verified)

### Host regression (Go 1.25.0)
`go mod verify` PASS; `gofmt -l .` clean; `go vet ./...` PASS; `go test ./... -count=1` PASS (9 packages); `go test -race ./... -count=1` PASS (9 packages); `go build ./...` PASS; `git diff --check` clean.

### Fuzz + golden tests (added this phase)
- `FuzzParseAttrs`: 10s run, 765k execs, no panic (guards the previously-panicking attribute parser).
- `TestCTInfoUAPIGolden`: hard-coded UAPI values for `NFQA_CT_INFO` (4-byte BE) and the 0-based `ip_conntrack_info` enum; PASS.
- `TestNFQNLAttrNumbersMatchUAPI`, `TestConfigCommandValuesUAPI`: PASS.

### Privileged VM (Debian 13, kernel 6.12.116, QEMU TCG) — controls run
| Test | Result |
|---|---|
| TestLabDeniedTraffic (with positive control) | PASS 15.71s |
| TestLabDefaultDrop (allow-phase control + default-drop) | PASS 16.29s |
| TestLabQueueNoListener (no-listener deny + daemon allow control) | PASS 13.09s |
| TestStateUnsolicitedInbound (control NEW allow, reload, deny) | PASS 5.44s |
| TestStateInvalidConnection (control allow + unmatched-port deny) | PASS 13.17s |

### Prior full-suite VM result (suite6, same branch before controls)
13/16 PASS: Crash/Restart, Readiness, LabAllowedTCP, LabAllowedUDP, LabDeniedTraffic, LabDefaultDrop, LabQueueNoListener, LabShutdownClean, HookCleanupScope, NATDnat, NATMasquerade, NATReturnTraffic, NATWithFiltering. Remaining 3 (TestNATReloadWhileConnections, TestQueueOverflowKernelDrop, state timeout/hold tests) were TCG-timeout-bound; their harness fixes are committed but not re-verified under TCG (component fixes verified: fresh-listener, daemon-readiness poll, 20s probe deadlines).

### `TestHookCleanupScope` "queue rule not found" (old main run)
Root cause was the CLI subcommand parsing bug (`hook cleanup` ran `hook status` → Verify). Fixed in `8f797bd` with regression `TestParseHookSubcommand`; `TestHookCleanupScope` PASSes in suite6 (8.26s). Not a product defect.

## CI workflow (prepared)
`.github/workflows/ci.yml`: `ubuntu-24.04`; installs `nftables iproute2 procps kmod`; preflight prints kernel config (`CONFIG_NETFILTER_NETLINK_QUEUE`, `CONFIG_NFT_QUEUE`, conntrack), loads `nfnetlink_queue`/`nf_conntrack`/`nf_conntrack_netlink`, installs a queue rule in a throwaway netns, reads `nf_conntrack_count`; suite rejects any SKIP (fails job). No secrets; `permissions: contents: read`.

## Scenario inventory (16 privileged + 5 control-extended)
TestCrashKillFailClosedAndRestart, TestReadinessFalseUntilEnforced, TestLabAllowedTCP, TestLabAllowedUDP, TestLabDeniedTraffic, TestLabDefaultDrop, TestLabQueueNoListener, TestLabShutdownClean, TestHookCleanupScope, TestNATDnat, TestNATMasquerade, TestNATReturnTraffic, TestNATWithFiltering, TestNATReloadWhileConnections, TestQueueOverflowKernelDrop, TestStateTCPHandshakeAndReply, TestStateUnsolicitedInbound, TestStateInvalidConnection, TestStateConnectionTimeout, TestStateUDPTimeout, TestStateReloadWhileEstablished.

## Verdict: **BLOCKED**
CI unexecuted (no auth to push). Local + VM evidence is strong: host regression green, controls all pass, 13/16 full-suite scenarios pass. Forcing a "READY" claim without CI is not permitted.
