# Stateful IPv6 e2e evidence — v0.2.4 (issue #5)

**Status: PASS — 4/4 scenarios.** Kernel: OpenWrt 25.12.6 x86/64,
**6.12.112** (guest). Host: QEMU TCG, dual-tap lab
(`scripts/openwrt-lab-v6.sh`). Run log:
`/data/issue5-v6-e2e-log.txt` sha256
`2e8c76ffce4aaef2ea966f621d3faf7e8cc70895ab994a443559b13012edd9e1`.

## Topology

- LAN ns `gsh_v6_lan`: `10.0.3.2/24` + `fd70:1::2/64`, gw `10.0.3.1`/`fd70:1::1`
- WAN ns `gsh_v6_wan`: `10.0.2.2/24` + `fd00::2/64` (dual-stack marker server :8089)
- Guest eth0 (WAN): `10.0.2.15/24` + `fd00::1/64`; eth1/br-lan (LAN): `10.0.3.1/24` + `fd70:1::1/64`
- WAN→LAN return route via guest WAN address (`fd70:1::/64 via fd00::1`)

## Scenarios (each with paired IPv4 control + counter evidence)

| # | Scenario | v4 control | v6 result | Evidence | Guest kernel |
| --- | --- | --- | --- | --- | --- |
| 1 | DENY blocks new flows | BLOCKED (curl 28, hook +7, drop +14) | BLOCKED (curl 28, hook +7, drop +14) | queue/hook/verdict deltas; server hits flat | 6.12.112 |
| 2 | stateful ALLOW completes | HTTP 200 (accept +22, hook +12) | HTTP 200 (accept +22, hook +10) | accept verdicts +, hook +, marker body | 6.12.112 |
| 3 | one-directional policy blocked | BLOCKED (curl 28, hook +12, drop +21) | BLOCKED (curl 28, hook +11, drop +21) | drop verdicts + both families | 6.12.112 |
| 4 | ALLOW→DENY switch blocks new flows | BLOCKED (curl 28, hook +7, drop +14) | BLOCKED (curl 28, hook +7, drop +14) | drop verdicts + both families | 6.12.112 |

## Counter evidence (from the run log)

```
[e2e-v6] v4 BLOCKED PASS: curl exit=28, drop verdicts +14, hook +7
[e2e-v6] v6 BLOCKED PASS: curl exit=28, drop verdicts +14, hook +7
[e2e-v6] v4 ALLOWED PASS: curl exit=0, HTTP 200, accept verdicts +22, hook +12
[e2e-v6] v6 ALLOWED PASS: curl exit=0, HTTP 200, accept verdicts +22, hook +10
[e2e-v6] v4 BLOCKED PASS: curl exit=28, drop verdicts +21, hook +12
[e2e-v6] v6 BLOCKED PASS: curl exit=28, drop verdicts +21, hook +11
[e2e-v6] guest kernel: 6.12.112
[e2e-v6] PASS: v6 stateful scenarios 1-4 completed with v4 controls and counter evidence.
```

Full log: `/data/issue5-v6-e2e-log.txt` (sha256
`2e8c76ffce4aaef2ea966f621d3faf7e8cc70895ab994a443559b13012edd9e1`).

## CI run (PR #52, `openwrt-integration-v6` job)

- Run: 43735801998, job 132878036620 — **PASS** (all 4 scenarios, same
  counter evidence as the lab).
- Runner kernel: **7.0.0-1017-azure** (GitHub ubuntu-24.04 runner).
- Guest kernel: **6.12.112** (OpenWrt 25.12.6 x86/64, pinned kmods).
- The job fails on SKIP and on "no tests to run" (runner guards).

## CI wiring

- `scripts/openwrt-e2e-v6.sh` — scenario matrix (this evidence).
- `tests/openwrt_integration/e2e_v6_test.go` — Go gate: runs the script,
  skips with explicit reason when the lab is down; CI rejects skips.
- `scripts/e2e-runner.sh` `E2E_LAB_MODE=v6` — boots the v6 lab, runs the
  v6 test, teardown via the v6 lab script.
- `.github/workflows/ci.yml` `openwrt-integration-v6` job — privileged,
  GitHub-hosted ubuntu-24.04, records the runner kernel, fails on SKIP.

## Limits

- Verified kernel: **6.12.112 x86/64 only** (this lab). arm64 and any
  other kernel remain UNVERIFIED (fail-closed by construction).
- QEMU TCG emulation only; no hardware claim.
- CI runner kernel is recorded in the job log but the guest kernel is
  the verified one (the VM's kmods are the pinned 6.12.112 set).
