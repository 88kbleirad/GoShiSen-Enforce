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
| Task 6 SDK git-pin build | PASS | clean SDK build from PKG_SOURCE_VERSION + PKG_MIRROR_HASH (no file:// override) |
| Task 6 reproducibility | PASS | two clean SDK builds → byte-identical apk (`b4925a60…`) |
| Task 7 CI | PASS | push `43065507036` and PR `43065518847`: all 4 jobs success (build/vet/test, openwrt e2e, package, integration), 0 skips |
| Task 8 docs | PASS | docs/openwrt.md, SECURITY.md threat model, checklist gates |
| Task 9 version | PASS | v0.2.0 proposed; release notes draft; teardown done |
| Final local e2e (VM, r2 package, dual-queue) | PASS | DROP curl 28 (+7 drop), ACCEPT 200 (+13 accept: new +1/est +12); identity v4 hook 20 == verdicts 13+7; queue100 id_seq 20; backend_errors absent |

## C. Defects found and fixed

| Defect | Root cause | Fix | Regression test |
| --- | --- | --- | --- |
| IPv6 full bypass | hook table family `ip` | `inet` table + v6 parser + policy v6 prefixes + dual queues (100/101) | parser/policy/hook unit tests; VM v6 allow/deny/kill |
| Same-queue dual bind EPERM | nfnetlink_queue rejects second bind, queue number | v6 queue = queue_num+1 | config-sequence tests |
| fw4 `flow add @ft` not detected | detection only looked for `flow offload` | detect `flow add @` + flowtable objects | fake-nft tests |
| `backend_errors_total` never incremented | metric registered but never wired | `SetErrorObserver` wired in app | `TestBackendErrorObserver` |
| apk remove orphans hook | prerm only stopped service | prerm runs `hook cleanup` | apk remove re-test on VM |
| e2e hook-delta read v6 line | two queue rules; grep took last | snapshot targets `nfproto ipv4` rule | CI e2e green |
| procd reload used `procd_kill` | deleted the instance | `procd_send_signal` | VM reload tests |

## D. Package and reproducibility

- Static release build: `make build` → `bin/goshisen` static, `make verify-static` passes, CI checks `file`+`ldd`.
- SDK build from git pin: `PKG_SOURCE_VERSION` (pushed) + `PKG_MIRROR_HASH` (SDK `.tar.zst` sha256) — clean build succeeds without local overrides.
- Reproducibility: two clean SDK builds → byte-identical apk `b4925a60…` (binary inside: `a00e4a52…`).
- apk payload: `/usr/sbin/goshisen` (static), `/etc/init.d/goshisen` (procd, `service_enabled` gate), `/etc/config/goshisen` (netdev `interface`), `/etc/goshisen/policy.yaml` (fail-closed), `prerm` runs `hook cleanup`.

## E. CI status (per job, run 43066814143, head fa9b58e)

| Job | Conclusion |
| --- | --- |
| build, vet, test | success |
| openwrt e2e (QEMU lab, privileged) | success |
| package + verify release archives | success |
| integration (real netns lab, privileged) | success |

Zero mandatory skips (both privileged jobs fail on `--- SKIP`). PR run 43066824685 (same head) success. Earlier infra flake 43041340437 (apt DNS timeout) not code-related; rerun path green.

## F. Remaining risks and blockers

Product limitations (documented, not failures):
- Flow offloading is detected and warned, not disabled (by design).
- Stateful IPv6 depends on kernel CT-attribute delivery; otherwise INVALID→DROP (fail-closed); stateless v6 works.
- Only x86_64 verified; other OpenWrt arches untested.
- Boot window before daemon start is governed solely by fw4 (no policy until hook install).
- TCG VM timing/performance figures are indicative only (no numbers published).

Environment limits: privileged host suite cannot run on this workstation (host lacks nf_tables/nft); CI covers it (green).

Unverified/blocked: none for the mandatory gates.

## G. Release verdict

READY — all mandatory gates have evidence and CI is green at the pushed head with zero skips.

## H. Proposed version and next commands

Proposed version: **v0.2.0** (v0.1.0 already tagged; current daemon/package report 0.2.0; notes draft at `docs/release-notes-v0.2.0.md`).

Eligible commit for tagging: `fa9b58e1c043c016f888073511cff7bdee5bd106` (the CI-green head, run 43066814143). The branch head is `6ffd4f1` (docs-only follow-ups: evidence verbosity); tag `fa9b58e` (or the post-merge main SHA) after merge.

Merge/tag commands (await explicit authorization):
```sh
# 1. merge the draft PR (#3: phase10-openwrt → main) after CI refresh
gh pr merge 3 --merge
# 2. verify main CI on the merged head (zero skips)
# 3. tag
git tag v0.2.0 fa9b58e    # or the post-merge main SHA
git push origin v0.2.0
# 4. release workflow drafts the GitHub release; publish manually
```
Nothing merged, tagged, or published yet (authorization required).
