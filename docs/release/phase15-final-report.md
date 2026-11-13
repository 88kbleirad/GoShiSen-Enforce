# GoShiSen-Enforce — Phase 15 Final Report (Prompt 06)

**Date:** 2026-11-13
**Verdict: DONE** (release-candidate work complete; tag/publish gated on approval)
**Scope:** baseline capture, atomic policy reload (Phase 15), hardening findings,
performance before/after, OpenWrt VM compatibility, CI, risks, known limitations,
rollback plan, and the exact approval-gated release commands.

---

## 0. Baseline (v0.2.0, Prompt 00)

Environment: dev box, Linux 7.3.3-1-cachyos, Go go1.27.3, 12-thread
i5-11400H, 32 GB RAM; repo HEAD `bfbf813` (branch `docs/phase14-v021-notes`),
working tree clean.

| Check | Result | Evidence |
| --- | --- | --- |
| `make check` (fmt+vet+test+build+static) | PASS | `evidence/baseline-v0.2.0/make-check.log` |
| `go test -count=1 ./...` (fresh) | PASS | `evidence/baseline-v0.2.0/go-test-fresh.log` |
| `go test -race ./internal/... ./tests/package/` | PASS | inline |
| `go test ./tests/integration/` | 21/21 SKIP (root-gated) | skip: "integration tests require root for CAP_NET_ADMIN" |
| Benchmarks | none existed at baseline | `grep 'func Benchmark'` → empty |

Baseline binary: `bin/goshisen` v0.2.0-12-gbfbf813, static, 7,835,772 B,
sha256 `63ec91cc…b3f5`.

---

## 1. Atomic policy reload (Phase 15)

Unified durable publish pipeline, shared by SIGHUP reload and REST API
apply/activate:

```
compile → next version from store → WritePolicy (fsync) → SetActive (fsync)
       → King.Replace (in-memory snapshot swap)
```

Under one process-wide mutation lock; any failure keeps the previous policy in
both the store and the live ruleset; a daemon killed mid-publish recovers the
previous active version (torn versions never activate). New observability:
`goshisen_policy_updates_total{outcome="success"|"failure"|"conflict"}`;
`policy dry-run` CLI previews a publish read-only (missing store is an error,
never created).

**Tests:** 10 new — `internal/app/atomic_reload_test.go` (malformed keeps old,
valid publishes, partial failure keeps old, concurrent serializes to distinct
versions, IPv6+IPv4 publish, end-to-end SIGHUP, mid-publish kill recovery),
`internal/api/server_test.go` (shared mutex, outcome metrics), `cmd/goshisen/main_test.go`
(dry-run read-only, missing-store error).

**Verification:** `make check` PASS; `go test -race -count=2 ./internal/app/
./internal/api/ ./internal/metrics/` PASS (11.8s); `go test -race ./...` PASS.

---

## 2. Hardening findings (CI + privileged repro)

Three real defects were found by the release-candidate CI and reproduced/fixed
locally in a privileged Docker container (the instabox sandbox kernel lacks
NFQUEUE):

| # | Defect | Symptom in CI | Fix | Regression test |
| --- | --- | --- | --- | --- |
| 1 | `tupleKey` port packing aliased ports (`protocol<<16 | srcPort<<8 | dstPort` in a `uint32`; 8-bit fields overlap) | stateful handshake/reply, overflow, reload-while-established timeouts | `ppm uint64 = proto<<32 | sport<<16 | dport` | `tuplekey_test.go` (16-bit ports, tcp-vs-udp discrimination) |
| 2 | e2e reload gate grepped `policy reloaded" rules=N`; Phase 15 logs `policy published" version=… source=… rules=N` | OpenWrt e2e "policy did not reload to rules=1 within 30s" | `grep -Ec 'policy (reloaded|published)" .*rules=$want'` | (script gate) |
| 3 | Integration daemons shared default `policy_dir` (`policy_store` in cwd); Phase 15 startup recovery then loaded the *previous* test's active policy | `TestQueueOverflowKernelDrop` + every stateful test after it failed ("allowed TCP … i/o timeout") | per-daemon `--policy-dir $(mktemp -d)` in `startFirewallFull`, `startStateFirewall`, `startFirewallNAT`, `TestNATReloadWhileConnections` | (integration suite) |

All three are fixed on `rc/core-phase15-16`, verified by the privileged jobs:
`integration (real netns lab, privileged)` and `openwrt e2e (QEMU lab, privileged)`
(both must not skip, both PASS).

---

## 3. Performance before/after (Prompt 03)

Same idle machine, 3 runs per side, median (σ) reported in `docs/design/performance-phase.md`.
Baseline `base_idle_*`, optimized `opt_*_run{1,2,3}.txt` in `evidence/perf-prompt03/`.

| Benchmark | Base median | Opt median | Δ | Base allocs | Opt allocs |
| --- | ---: | ---: | ---: | ---: | ---: |
| King.Process NEW accept | 934 ns | 103 ns | **−89%** | 16 | 0 |
| King.Process ESTABLISHED reply | 409 ns | 53 ns | **−87%** | 7 | 0 |
| tupleKey (accepted-set key) | 359 ns | 4.4 ns | **−99%** | 6 | 0 |
| Pipeline NEW accept (e2e) | 1129 ns | 253 ns | **−78%** | 15 | 0 |
| Pipeline ESTABLISHED reply | 634 ns | 193 ns | **−70%** | 7 | 0 |
| Pipeline NEW default-drop | 668 ns | 206 ns | **−69%** | 8 | 0 |
| Pipeline metrics-only | 86.5 ns | 15.8 ns | **−82%** | 1 | 0 |
| Latency p50 / p95 / p99 (µs) | 1.039 / 1.719 / 2.686 | 0.306 / 0.31 / 0.48 | −71% / −82% / −82% | 15 | 0 |

- **0 allocations/packet** post-optimization (parser, engine, metrics, verdict build).
- ~3.9 M packets/s single-core decision capacity (253 ns/op).
- Semantics preserved: `go test -race ./...` + `make check` PASS; reply
  canonicalization pinned by `tuplekey_test.go`; verdict wire format golden test.

---

## 4. OpenWrt VM compatibility (Prompt 04, phase 16)

Target: OpenWrt 25.12.6 x86/64 (kernel 6.12.112), QEMU dual-bridge lab with
real IPv4+IPv6 LAN/WAN peers. Package r4 built from merge `f5f211a`
(PRs #13+#14+#15), `PKG_RELEASE=4`.

| Artifact | SHA256 |
| --- | --- |
| `goshisen-0.2.0-r4.apk` | `5020b86157e775fb8c706b71ec4b9c2b40ecc0346680d8f3528837c243922754` |
| `/usr/sbin/goshisen` (r4) | `6055cc16a87bcc801bdcf915c8fa8ac81a1fd9f935d9930e842fc77337a906c1` |
| `goshisen-0.2.0-r2.apk` (v0.2.0 asset) | `b4925a60a594159809bd03c10f73843ca3be8ba6748e6343dcc0e1af00a9c2af` |
| `/usr/sbin/goshisen` (r2) | `a00e4a52626a1f5203255c01ce92c8b94dcb04eaef3c5310f9c27a00136cf6d1` |

Reproducible: two clean SDK builds → byte-identical apk and binary.

**Paired controls matrix (positive/negative, per family):**

| Case | r2 baseline | r4 post-upgrade | r2 post-rollback |
| --- | :-: | :-: | :-: |
| v4 deny blocks | PASS | PASS | PASS |
| v4 allow passes (NEW) | PASS | PASS | PASS |
| v4 established reply passes | PASS | PASS | PASS |
| v6 deny blocks | PASS | PASS | PASS |
| v6 allow passes (NEW) | PASS | PASS | PASS |
| v6 established reply passes | PASS | PASS | PASS |

**Other verified behavior:** upgrade r2→r4 (old UCI config preserved,
`policy.yaml.apk-new`, no fail-open window); init ordering `S12goshisen-boot`
→ `S19firewall` → `S90goshisen`; boot guard fail-closed table + atomic daemon
replacement; service-disabled guard no-op; flow-offload policy `warn`/`refuse`/`readiness`;
reload failure keeps previous policy; `kill -9` → procd respawn fail-closed;
rollback r4→r2 PASS (caveat: dangling `S12/K10goshisen-boot` symlinks on
downgrade — run `/etc/init.d/goshisen-boot disable` first).

---

## 5. CI (real runs, pushed SHAs)

Workflow `.github/workflows/ci.yml`: `build, vet, test` / `package + verify release archives`
/ `integration (real netns lab, privileged)` / `openwrt e2e (QEMU lab, privileged)` —
lab jobs reject skips.

| PR | Branch | Head | CI run | Conclusion |
| --- | --- | --- | --- | --- |
| #16 | rc/core-phase15-16 | `d3830f7` | `43414029007` | success (4/4 jobs, 0 skips) |
| #17 | rc/feat-dry-run | `87093c7` | `43414112649` | success (4/4 jobs, 0 skips) |
| #18 | rc/packaging-docs | `19d106c` | `43414117668` | success (4/4 jobs, 0 skips) |

All runs: build/vet/test, package+verify release archives, integration (real netns,
privileged), openwrt e2e (QEMU lab, privileged) — all success, zero skips.

---

## 6. Risks

| Risk | Level | Mitigation / note |
| --- | --- | --- |
| Tag/publish not yet approved | — | exact commands in §9; release workflow attaches to a **draft** release (never auto-publishes) |
| Signing proposal unimplemented | Medium | `docs/design/signing-proposal.md`; apk currently `--allow-untrusted` (documented) |
| `S12/K10goshisen-boot` dangling symlinks on downgrade to r2 | Low | documented; `goshisen-boot disable` before downgrade |
| Flow-offload bypass impact unobservable under QEMU TCG (flowtable offloads 0 packets) | Low | detection/policy responses fully verified; recommend `flow_offloading` disabled when relying on enforcement |
| IPv6 stateful on other kernels | Low | verified on kernel 6.12.112; other kernels may need re-verification (v0.2.0 note) |
| Sandbox kernel lacks NFQUEUE | — | integration/openwrt jobs run on CI (ubuntu, NFQUEUE present); local repro via privileged Docker documented in skill |
| CI runs after doc-only rebase need refresh | Low | §5 tables will be updated with final run IDs | (reviewed, §5 above)

---

## 7. Known limitations (from v0.2.1 draft notes)

- Flow offloading is detected, not disabled; `offload_policy` only controls the
  daemon's response.
- Offload watcher polls ~10s; observed latency ≤2–4s.
- `refuse` mode exits at startup when offloading is enabled → outage-like; recovery:
  disable offloading or switch mode, then (re)start.
- `policy_store` is the durable source of truth; a file-only daemon creates a
  store version only at first reload/apply.
- arm64 binaries are built/verified statically only (runtime needs arm64 host).
- Container image (docker) deferred — `docs/container-feasibility.md`.
- `nft` must be present on target host.

---

## 8. Rollback plan

**Product rollback (r4 → r2):**
```
apk add --allow-untrusted goshisen-0.2.0-r2.apk
/etc/init.d/goshisen-boot disable   # BEFORE downgrade (removes S12/K10 links)
/etc/init.d/goshisen start
```
Verified PASS; controls still pass on r2.

**Release-commit rollback (if a tagged release must be pulled):**
- Do not delete the tag; publish a `v0.2.1+rollback` note or a follow-up patch.
- The `release` workflow only *uploads to a draft release*; a human must
  explicitly publish. If published by mistake, `gh release delete v0.2.1
  --cleanup-tag` then recreate the draft.

**Deployment rollback (runtime):**
- `apk del goshisen` runs prerm → `hook cleanup` + stop, so forwarding is not left
  blocked; reinstall previous apk.
- `goshisen hook cleanup` restores firewall4-only forwarding.

---

## 9. Exact commands requiring approval (tag/publish)

**Prerequisite (approval):** merge PRs #16 → #17 → #18 in order:
```
gh pr merge 16 --squash --repo 88kbleirad/GoShiSen-Enforce
gh pr merge 17 --squash --repo 88kbleirad/GoShiSen-Enforce
gh pr merge 18 --squash --repo 88kbleirad/GoShiSen-Enforce
```

**Tag (must be signed; triggers release workflow → draft release):**
```
git tag -s v0.2.1 -m "GoShiSen-Enforce v0.2.1"
git push origin v0.2.1
```

**Verify the draft release exists with all assets:**
```
gh release view v0.2.1 --repo 88kbleirad/GoShiSen-Enforce
gh release download v0.2.1 --repo 88kbleirad/GoShiSen-Enforce
sha256sum -c dist/checksums.txt
```

**Publish (final, explicit human step):**
```
gh release edit v0.2.1 --draft=false --repo 88kbleirad/GoShiSen-Enforce
```

> Nothing in this phase tagged, merged, or published without approval; all PRs
> remain OPEN.

---

## 10. Handoff to later phases (docs & developer experience)

**Documentation produced/updated:**
- `docs/design/performance-phase.md` — baseline/profile/optimize methodology + results.
- `docs/design/architecture.md`, `docs/design/mvp-scope.md` — Phase 15/16 status rows.
- `docs/developer-guide.md`, `docs/user-guide.md` — atomic publish, metrics, dry-run.
- `docs/release/v0.2.1-notes.md` — draft notes (verified claims only).
- `docs/release/phase16-openwrt-compat.md` — OpenWrt compatibility report + rollback caveat.
- `docs/openwrt.md` — status line + r4 pin + caveat.
- `evidence/` — baseline, perf (3-run logs), phase15-atomic-reload, phase16 artifacts
  (`/data/phase16-openwrt-compat-artifacts/` on the sandbox: summary.md, versions.md,
  evidence-final.txt, policies, ci-final.md).

**Developer experience handed off:**
- Managed skill `goshisen-rc-gates-and-integration-repro` — RC gate workflow + local
  privileged Docker repro (container setup, 3 defect signatures, fix recipes).
- Managed skill `goshisen-instabox-remote-verify` — sandbox/OpenWrt lab facts & gotchas.
- `make fuzz` now targets both fuzz functions (was broken).
- `make package`/`make verify-package` documented (syft 1.30.0 pinned, checksum-verified).

**Deferred to later phases:**
- Release signing implementation (`docs/design/signing-proposal.md`).
- Container image publish (dockerfile exists; deferred).
- Real-hardware/other-kernel re-verification of IPv6 stateful path.
- LuCI/UI proposal (`docs/design/luci-proposal.md`).
