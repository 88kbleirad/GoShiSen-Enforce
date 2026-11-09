# Phase 8 — Privileged Validation Report (Release Gate)

Date: 2026-11-09. Branch: `phase7-baseline` (+ commit `cbd4aee`).

## Verdict: **BLOCKED** — the privileged integration gate could not execute on this host.

The blocker is environmental and fundamental, not a code defect. See §A.5 and §D.

---

## A. Environment

| Item | Value |
| --- | --- |
| OS / kernel | Linux 6.18.60-instabox (x86_64) |
| Go | go1.27.1 linux/amd64 (module floor 1.25) |
| nft | nftables v1.1.3 (`/usr/sbin/nft`, root PATH) |
| Root | available (`sudo -n` works; uid 0) |
| Network namespaces / veth | available (`unshare -Urn` verified; lab creates/removes `gsh_*` cleanly) |
| Existing firewall state | host has pre-existing Docker/nft tables (`ip nat`, `ip filter`, `ip6 …`) — untouched by us |
| CI access | remote is public; GitHub API readable (run metadata); job logs 403 (admin-only); `gh` not authenticated |

### A.5 Critical limitation — NFQUEUE is not present in this kernel

- `zcat /proc/config.gz | grep NETFILTER_NETLINK_QUEUE` → **`# CONFIG_NETFILTER_NETLINK_QUEUE is not set`**.
- `CONFIG_NF_TABLES=y`, `CONFIG_NF_CONNTRACK=y` (these exist), but the NFQUEUE subsystem is compiled out.
- No module tree: `/lib/modules/` does not exist (custom kernel), so the module cannot be loaded after the fact.
- Direct proof: `nft add rule ip gsh_preflight c queue num 9` fails with `Error: Could not process rule: No such file or directory ... queue num 0`.
- `scripts/lab.sh up` fails at the same `queue num 0` rule (after successfully creating the three namespaces and veth pairs); `scripts/lab.sh down` then cleans the lab fully (verified: no `gsh_*` netns/veth remain).

Because the product's enforcement path is NFQUEUE, **no privileged integration test can honestly run here**. Running them would skip (or, per the new CI guard, fail) — neither satisfies the release gate.

## B. Commands executed

```bash
git status --short --branch          # clean, phase7-baseline
git log -5 --oneline                  # 5 expected commits present
git diff --check                      # clean
go version / go env GOOS GOARCH       # 1.27.1 linux/amd64
go mod verify                         # pass
zcat /proc/config.gz | grep ...       # NFQUEUE absent (evidence above)
sudo scripts/lab.sh up                # fails at nft queue rule (no NFQUEUE)
sudo scripts/lab.sh down              # cleanup ok
go vet ./...                          # pass
go test ./...                         # pass (integration skips, explicit reason)
go test -race ./...                   # pass (run below)
go build ./...                        # pass
gofmt -l .                            # empty
python3 yaml validate ci.yml           # valid; integration job now guards skips
```

## C. Test results

| Validation | Result | Evidence |
| --- | --- | --- |
| Baseline regression (mod verify/fmt/vet/test -race/build) | **PASS** | full local runs green; integration tests skip with explicit reasons |
| Privileged integration (Phase 3 suite) | **BLOCKED** | NFQUEUE compiled out of kernel; lab up fails at queue rule |
| NFQUEUE overflow (D1) | **BLOCKED** | depends on NFQUEUE |
| Fail-closed behavior (D2) | **PARTIAL** | unit invariant `TestFailClosedNeverRequestsFailOpen` passes locally; runtime proof blocked |
| Crash/restart (D3) | **BLOCKED** | depends on live queue |
| Readiness lifecycle (D4) | **PARTIAL** | unit tests pass (pre-queue false, post-queue true, cleared on shutdown); HTTP-level proof blocked |
| nftables cleanup (D4) | **PASS (lab scope)** | `lab.sh down` verified clean; no `gsh_*` state remains; host tables untouched |
| Security regression (D5) | **PASS (local)** | bounded metric enum; no token in config/logs/metrics; fail-open invariant holds |
| CI checks | **UNVERIFIED** | origin/main still at `24bac6b`; our branch unpushed; GitHub API shows that run's privileged job failed (pre-fix Go 1.22 toolchain); logs admin-locked |

## D. Why the gate is blocked (exact missing prerequisites)

To run `sudo make integration` the host needs:

1. A kernel with `CONFIG_NETFILTER_NETLINK_QUEUE=y` (or the `nfnetlink_queue` module loadable), plus `nf_tables`/`nf_conntrack`.
2. `nft`, `ip`, and root/CAP_NET_ADMIN.
3. A disposable/isolated environment (netns lab is already isolated; host firewall untouched by design).

Approved-environment commands for the blocker:

```bash
# On a privileged Linux host (VM/runner) with NFQUEUE support:
sudo scripts/lab.sh up
sudo make integration          # now fails loudly if any test skips (CI guard)
sudo scripts/lab.sh down       # cleanup trap
```

## E. Changes made in Phase 8

- `.github/workflows/ci.yml`: integration job now greps test output for `--- SKIP` and exits non-zero — a green CI can no longer be produced by skipped privileged tests (commit `cbd4aee`).
- `docs/release/phase8-privileged-validation.md` (this report).
- No code changes, no weakened tests, no tags.

## F. Release assessment

- Completed gates: baseline regression, security regression (local), lab cleanup behavior, CI skip-guard.
- Outstanding blockers: **the entire privileged integration suite** (requires NFQUEUE-capable kernel). Also unverified: post-fix CI runs (branch not pushed; no authorization to push).
- Remaining risks: no runtime evidence (yet) for overflow counters, fail-closed under real queue pressure, crash/restart recovery, or readiness over HTTP.
- **Verdict: BLOCKED.** Not READY; not INCONCLUSIVE (the missing prerequisite is precisely identified and unreachable here).

## G. Next recommended actions

1. Run the privileged suite on an approved host (commands in §D), capture the full `go test -v` output, and record PASS/FAIL per test in this report.
2. If any test fails there, treat it as a production defect (Phase 8 Step E) and fix minimally before re-running.
3. Push/validate the branch on GitHub Actions only with authorization; verify the integration job is green **with no skips** (the new guard enforces this).
4. After the privileged gate passes: finalize release notes, propose `v0.1.0`, and tag only with explicit approval.
