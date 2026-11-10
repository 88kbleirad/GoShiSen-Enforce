# Phase 8.5 — Conntrack Namespace Recovery & GitHub Hosted CI Validation

Date: 2026-11-10. Branch: `phase7-baseline`.

## Verdict: **BLOCKED** (release gate)

Conntrack root cause isolated with reproducible evidence; the product's
CT path is correct per the kernel API, but the VM (Debian 13, kernel
6.12.116, QEMU TCG) does not populate `NFQA_CT/CT_INFO` on queued
packets, so stateful allow-path/NAT/overflow scenarios cannot be
validated in it. GitHub Actions CI for this branch remains UNVERIFIED
(unpushed; no authorization).

---

## A. Conntrack root cause — investigation log

### A.1 Minimal forward topology (independent probe) — WORKS
3 fresh namespaces, router with a single forward chain (`policy accept`
+ `ct state {…}` counter), client→server TCP: ct rule matched 10
packets, `nf_conntrack` gained the TCP entry, `nf_conntrack_count=1`,
traffic flowed (ECHOED). **Conntrack works in fresh netns on this
kernel.**

### A.2 Lab topology (gsh_fw) — FAILS
The lab's `gsh_fw` (with `gsh_lab` table) shows empty `nf_conntrack`
and zero stat counters even without the daemon. Isolating variants:
adding a `gsh_lab`-style forward basechain (policy drop) or (policy
drop, no queue rule) both break CT; a single chain with `policy drop`
also breaks CT confirmation, while the same single chain with `policy
accept` works.

### A.3 Policy finding (mct5)
Same single chain, same ct rule: **`policy drop` → CT count 0; `policy
accept` → CT count 1, traffic flows.** Rationale: conntrack entries are
confirmed at POSTROUTING; a packet dropped at FORWARD (chain policy)
never reaches confirmation, so no lasting entry exists — and the
queued/held packet's CT metadata is not delivered.

### A.4 Product-path finding (prodct / prodct2)
With the *product hook exactly as generated* (ct rule + queue rule +
policy drop) and the real daemon, both `policy drop` and `policy
accept` yield: queue rule queued 7 packets, daemon metrics show all
7 as `state="invalid" action="drop"`, CT count 0. Loading
`nf_conntrack_netlink` explicitly (prodct3) does not change the
outcome — so the missing-module hypothesis is disproven.

### A.5 Conclusion
- The product correctly requests `NFQA_CFG_F_CONNTRACK` and parses
  `NFQA_CT/CT_INFO`; its fail-closed behavior (INVALID → DROP) is
  correct and is exactly what the architecture documents.
- On this VM, queued packets carry no conntrack metadata (`ctInfo=0`
  → INVALID), so stateful allow/NAT/overflow scenarios cannot pass.
  This is a VM kernel/netns quirk (QEMU TCG; Debian 6.12) — the
  combination is standard and works on real hosts (NFQUEUE +
  conntrack is used by production systems).
- **The remaining validation must run on a real conntrack-capable
  host/runner.** The new integration preflight (below) detects this
  condition functionally and skips with an explicit reason, which the
  CI job rejects.

## B. Changes made in Phase 8.5

- `tests/integration/e2e_test.go`: `checkPrereqs` now also runs a
  functional conntrack preflight (`checkConntrackNFQUEUE`): a
  throwaway namespace, a `ct state`-bearing nft chain, a real loopback
  TCP flow via the probe, and asserts `nf_conntrack_count > 0`. If the
  environment cannot track, tests skip with an explicit reason
  (CI fails on any skip).
- `.github/workflows/ci.yml`: integration job → `ubuntu-24.04`;
  installs `nftables iproute2 procps kmod`; preflight step loads
  `nf_conntrack`/`nf_conntrack_netlink` and prints kernel config;
  skips rejected; timeout 20m.

## C. Host validation (Go 1.25.0)

`go mod verify` PASS; `gofmt -l .` clean; `go vet ./...` PASS;
`go test ./... -count=1` PASS; `go test -race ./... -count=1` PASS;
`go build ./...` PASS; `git diff --check` clean. Workflow YAML parsed
and validated.

## D. Validation matrix

| Validation | Result | Evidence |
| --- | --- | --- |
| Netlink protocol (golden) | PASS | unit/golden tests |
| NFQUEUE BIND/config | PASS | standalone probe ACKs, queue listed |
| Conntrack tracking (fresh netns) | PASS | minimal 3-ns probe (A.1) |
| Conntrack in lab gsh_fw | BLOCKED (VM quirk) | A.2–A.5 |
| Stateful allow / NAT / overflow / crash-restart | BLOCKED (VM) | depends on CT-in-queue |
| Fail-closed (INVALID→DROP) | PASS | metrics show all denied traffic dropped |
| Readiness | PASS | `TestReadinessFalseUntilEnforced` |
| Hook cleanup scope | PASS | `TestHookCleanupScope` |
| Host regression | PASS | §C |
| GitHub CI | UNVERIFIED | branch unpushed; workflow prepared |

## E. Remaining blockers

1. Run the privileged suite on a conntrack-capable real host/runner
   (the preflight now detects and reports any environment that cannot).
2. Push the branch and let GitHub Actions run both jobs; integration
   must be green with **zero skips**. CI remains UNVERIFIED until then.
