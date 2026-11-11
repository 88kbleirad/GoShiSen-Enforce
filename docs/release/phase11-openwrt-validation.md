# Phase 11 — OpenWrt Hardening & Release Candidate Verification (evidence)

Status: in progress. Date: 2026-11-11. This file records executed
evidence only; never inferred results.

## A. Environment

| Item | Value |
| --- | --- |
| OpenWrt | 25.12.6 r33311-981c978eff, x86/64 |
| Kernel | 6.12.112 (OpenWrt) |
| QEMU | 10.0.13 (TCG), -machine pc, 512 MB, 2 vCPU |
| Go (host) | 1.27.1 (CI uses 1.25) |
| SDK | OpenWrt SDK 25.12.6 (gcc-14.3.0, x86-64) |
| Daemon | `goshisen 0.2.0-openwrt`, static (CGO=0) |
| Binary sha256 | `bd965f30…` (observer build), rebuild per commit |

## B. Results matrix

| Scenario | Result | Evidence |
| --- | --- | --- |
| Task 1 audit: gofmt/vet/test(-race)/build | PASS | local runs; CI build job |
| Task 1 pin fix + push | PASS | b53ce58..HEAD pushed; PKG_MIRROR_HASH added |
| Task 2 IPv6 bypass proof | PASS | v4-only hook: v6 HTTP 200, queue id_seq 0 (bypass) |
| Task 2 inet hook + v6 parser | PASS | v6 fail-closed 000 under default; stateless allow 200; SIGKILL 000 |
| Task 2 cleanup scope | PASS | `hook cleanup` removes only `inet goshisen`; fw4 intact |
| Task 3 offload detection | PASS | gauge 0→1 on enable, 1→0 on disable (10 s tick); WARN |
| Task 4 malformed live | PASS | +5 parse errors metric, queue id_seq +5, positive control 200 |
| Task 4 fuzz | PASS | FuzzParse 15.7M execs, FuzzParseAttrs 13.9M execs, 60 s each, 0 findings |
| Task 5 procd lifecycle | PASS | start/stop/restart/reload; reboot persistence; respawn (kill→new pid); disable-default; `uci commit` no restart |
| Task 5 stop/fail-closed | PASS | stop → daemon gone, hook retained, v4 000; hook cleanup → 200 |
| Task 5 interactions | PASS | firewall restart/reload, network restart, br-lan flap, conntrack -F: tables intact, queues bound, v4 200 |
| Task 5 apk install/upgrade/remove | PASS | r1 install, r1→r2 upgrade (no fail-open), remove runs `hook cleanup` (table gone; fw4 only) |
| Task 6 static release build | PASS | `make build` static; `make verify-static`; CI check added |
| Task 6 SDK git-pin build | in progress | clean SDK build from PKG_SOURCE_VERSION + mirror hash |
| Task 6 reproducibility | pending | two clean builds compared |
| Task 7 CI | in progress | runs listed below |
| Task 8 docs | PASS | docs/openwrt.md, SECURITY.md threat model, checklist gates |
| Task 9 version | in progress | version determination + release notes |

## C–F. (to be completed at end of phase with final run IDs and verdicts)
