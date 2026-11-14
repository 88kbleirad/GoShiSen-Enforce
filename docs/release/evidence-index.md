# GoShiSen-Enforce — Evidence Index (Prompt 04–10 closure)

**Generated:** 2026-11-13 (UTC) — final closure after Prompt 10 merge.
**main:** `447298df` (HEAD, origin/main) — after final evidence-docs merge
**Final docs commit:** `447298df` (PR #22, squash) — the evidence index itself.
**Release:** v0.2.1 published — tag object `6eb19f3` → commit `9ca71333` (verified).
**Tag note:** `v0.2.1` points at `9ca71333` — the *release commit* (the tree the
release assets were built from). Everything after it (`f0c923a`, `8e007a5d`,
`0d4dcd9f`) is post-tag documentation/harness work; it is **not** part of the
v0.2.1 build.

---

## 1. Chain of evidence: prompt → commit → CI → artifact

| Prompt | Deliverable | Commit / PR | CI runs (success) | Notes |
| --- | --- | --- | --- | --- |
| 00 | Baseline v0.2.0 | `bfbf813` (branch docs/phase14-v021-notes) | make-check.log / go-test-fresh.log (evidence/baseline-v0.2.0/) | 21/21 integration SKIP non-root |
| 03 | Performance before/after | worktree @ `bfbf813` (uncommitted at the time; shipped via #16) | — | evidence/perf-prompt03/ (3-run medians, 0 allocs/packet) |
| 04 | OpenWrt VM compatibility | merge `f5f211a` (#13+#14+#15), package r4 | phase16 branch; later on main via `c61da88` | `/data/phase16-openwrt-compat-artifacts/` on sandbox |
| 05 | Release candidate gates | PRs #16/#17/#18 | 43414029007 / 43414112649 / 43414117668 (+final docs heads) | all 4/4 jobs, 0 skips |
| 06 | Phase 15 final report | PR #18 (`docs/release/phase15-final-report.md`) | 43416016057 (report head) | verdict DONE |
| 07 | Merge + publish v0.2.1 | phase16→main `c61da88`; #16 `55065f6f`; #17 `dc6d992d`; #18 `9ca71333`; #19 `f0c923a` | tag CI 43427056935 (success 4/4) | tag `6eb19f3` = `9ca71333`, SSH-verified; release published |
| 09 | e2e reload-gate fix | PR #20 `8e007a5d` | push 43435999202 / PR 43436009943 (both success 4/4) | harness-only fix + 13 regression tests |
| 10 | Review + merge #20, record | PR #20 `8e007a5d`; PR #21 `0d4dcd9f` | main post-merge 43437820844 (success 4/4, 0 skips) | release decision: keep v0.2.1 |
| 11 | Evidence index + backlog | PR #22 `447298df` | push 43439564775 / PR 43439586070 (both success 4/4, 0 skips) | docs-only; tag/release untouched |
| 17 | Arm64 runtime verification | `feat/arm64-verify` (this PR) | local make check + go test green; live OpenWrt 25.12.6 `armsr/armv8` aarch64 QEMU TCG; CI arm64 job | aarch64 runtime: start/version/help, NFQUEUE bind + readiness, DROP/ACCEPT verdicts, atomic reload (valid+invalid), restart/graceful shutdown, offload enforce/observe. Cross-build + static-ELF CI job/tests. Evidence: `docs/release/prompt17-arm64-runtime-evidence.md` |
| 18 | OpenWrt downgrade cleanup | `feat/pkg-downgrade-cleanup` (this PR) | local make check + go test green; live OpenWrt 25.12.6 QEMU (r2/r4 apks) | prerm unconditional rc.d link removal (uninstall verified); empirical apk finding (no old-prerm on downgrade) → pre-downgrade `goshisen-boot disable` verified + `unlink` rescue documented; tests (lifecycle + Makefile + docs). Evidence: `docs/release/prompt18-downgrade-cleanup-evidence.md`, design `docs/design/openwrt-downgrade-cleanup-phase.md` |
| 27 | Merge signing PRs #24–#28 | PRs #24/#25/#26/#27/#28 (squash `dcc8cb5`/`35f7aef`/`d09ecbc`/`4c91d83`/`c5de6e8` on main) | local `make check` PASS + `go test -race` PASS + `make package`/`verify-package` PASS; sign+verify pipeline PASS (test key); production verifier rejects Instabox test key (`a8253141…`, exit 4 bad-signature) | all Phase 14.2 open items merged: artifact signing/provenance (#24), offload enforce-mode disable (#25), flowtable observability (#26), arm64 runtime verification (#27), downgrade cleanup (#28). Conflicts resolved per PR (evidence-index/backlog rows 15–18 + implemented items; `needCmd` duplicate removed). No tag/release/version/key change |
| 28 | v0.2.2 candidate revalidation (Phase 14.2 merged scope) | candidate `c5de6e8` (docs head `74846c8`) | local @ exact `c5de6e8`: `go test ./tests/package`, `go test -race ./...`, hermetic package, preflight-real, `make check`, double `make package VERSION=0.2.2-ready` byte-identical, `make verify-package`, sign+verify pipeline + tamper control — all PASS. CI 43560836375 (`c5de6e8`) and 43561323620 (`74846c8`): 6/6 jobs success | candidate scope now includes signing/provenance, offload enforce-mode disable + observability, arm64 runtime verification, downgrade cleanup, readiness fix, e2e runner fixes. Production anchor full fingerprint `24215e016621925215dec0ad6ea14ac74253a4e97249a973e89d649a2217cca0` verified from candidate tree; OpenWrt package pin unchanged (`f5f211a`, PKG 0.2.0-4) — documented gap, not a CLI-release blocker. Production-key signing gate remains post-tag/DRAFT only |
| 29 | v0.2.2 release execution | tag `v0.2.2` object `92cc351b` → commit `c5de6e8`; release workflow 43563276440 success (package+verify+publish job) | DRAFT created (all 19 assets); signed with production key; re-downloaded assets: checksums OK, binaries `goshisen v0.2.2` (amd64 static x86-64, arm64 static aarch64), SBOM SPDX-2.3 valid, provenance `source_commit=c5de6e8…`/`tag=v0.2.2`/builder `release.yml/package`/toolchain go1.25.14; `verify-signatures.sh --require-sign` with production anchor → VERIFY-PASS exit 0 (pre and post publish); released `public_key.pem` fingerprint == anchor. Published `2026-11-14T05:51:06Z`; GitHub tag verification `verified=true`/reason `valid`; v0.2.1 release/assets untouched (published_at unchanged, same 4 assets) | release URL `https://github.com/88kbleirad/GoShiSen-Enforce/releases/tag/v0.2.2`; OpenWrt package intentionally not bumped (independent versioning); v0.2.2 assets now signed with production key (first signed release) |

## 2. Release assets and checksums (verified 2026-11-13)

Artifacts attached to `v0.2.1` (published `2026-11-13T12:43:29Z`, isDraft=false):

| Asset | Size (bytes) | sha256 (from release checksums.txt) |
| --- | --- | --- |
| checksums.txt | 188 | — |
| goshisen-linux-amd64.tar.gz | 2,987,476 | `126c2a9e8e04a4542ce850011e31c03328e82fd2568669278122ef886f413f1d` |
| goshisen-linux-arm64.tar.gz | 2,698,988 | `2d8ca943c3e782d7f403ec4462095a635b10a35db9a0943c91f7afad823b8e93` |
| sbom.spdx.json | 18,063 | SPDX-2.3, 13 packages (valid) |

`sha256sum -c checksums.txt` → both OK. Archives verified: amd64 static x86-64
(`goshisen v0.2.1`), arm64 static aarch64.

## 3. Artifact manifests (sandbox evidence)

| Evidence | SHA256 |
| --- | --- |
| `/data/phase16-openwrt-compat-artifacts/evidence-final.txt` | `6a324c32934d8b82ab4d5e3d5c55bb5def600ea69945419413d4a997ff24ab00` |
| `/data/phase16-openwrt-compat-artifacts/summary.md` | `25f3e393fe125c1af03c016c3a3e3b7d71762187d8679f6ead886d52ce9e5871` |
| `/data/phase16-openwrt-compat-artifacts/versions.md` | `21a14eac8b8ea5074f15c9dc32c6c8009bac41bb3126557e29180982a505c8aa` |
| `/data/phase16-openwrt-compat-artifacts/ci-final.md` | `1cd64695ce99f4a111303c02dca47c729c60518fcb71c1dfcfdbffa8b6dc36d2` |
| `/data/phase16-openwrt-compat-artifacts/e2e-gate-fix.txt` | `c9d9f96cb4029658200ae21d809b7cee5e131624317c7bfdcc687ebab3725d51` |
| r4 apk `goshisen-0.2.0-r4.apk` | `5020b86157e775fb8c706b71ec4b9c2b40ecc0346680d8f3528837c243922754` |
| r4 binary `/usr/sbin/goshisen` | `6055cc16a87bcc801bdcf915c8fa8ac81a1fd9f935d9930e842fc77337a906c1` |
| r2 apk (v0.2.0 asset) | `b4925a60a594159809bd03c10f73843ca3be8ba6748e6343dcc0e1af00a9c2af` |
| r2 binary | `a00e4a52626a1f5203255c01ce92c8b94dcb04eaef3c5310f9c27a00136cf6d1` |

Full manifest: `manifest-sha.txt` in the same directory (sandbox).

## 4. CI runs referenced in this index

| Run | Event | Head/PR | Conclusion |
| --- | --- | --- | --- |
| 43414029007 | PR | #16 `d3830f7` | success 4/4 |
| 43414112649 | PR | #17 `87093c7` | success 4/4 |
| 43414117668 | PR | #18 `19d106c` | success 4/4 |
| 43416016057 | PR | #18 report head | success 4/4 |
| 43427056935 | push (tag) | v0.2.1 `9ca71333` | success 4/4 |
| 43433459371 / 43433469580 | push/PR | #20 `c92ea5e` | success 4/4 |
| 43435999202 / 43436009943 | push/PR | #20 `701a378` | success 4/4 |
| 43437820844 | push (main) | `0d4dcd9f` | success 4/4, 0 skips |

## 5. Live OpenWrt QEMU e2e (Prompt 09 fix)

Environment: instabox sandbox, OpenWrt 25.12.6 x86/64 (kernel 6.12.112),
QEMU TCG, dual-bridge lab. Two runs with the merged gate fix both PASS:

```
[e2e] DROP control PASS: curl exit=28, daemon drop verdicts +7, hook +7, queue drops +0
[e2e] ACCEPT control PASS: curl exit=0, HTTP 200, marker body, accept verdicts +11 (new +1, established +10), hook +11
[e2e] PASS: DROP blocks the request, ACCEPT permits it, all counters agree; kernel queue drops stayed flat.
```

## 6. Environment / limitations

- Sandbox kernel (`6.18.60-instabox`) lacks `CONFIG_NETFILTER_NETLINK_QUEUE`;
  `tests/integration`, `tests/openwrt_integration`, and `internal/hook` nft-queue
  tests fail there. The real gates are the CI privileged jobs (both reject skips).
- Reproducing those suites locally uses a privileged Docker container (loop-device
 / rootfs-offset workaround documented in the managed skill).
- Release `v0.2.1` was built from `9ca71333`; post-tag docs/PRs (#19/#20/#22/#23)
  do not alter the release assets or the tag. **Exception:** PR #29
  (squash `e94d60a`, post-tag) *does* change binary behavior — it fixes a
  readiness race in `internal/app` (`defer d.ready.Store(false)`) and adds
  the hermetic preflight/e2e harness. Those changes are **not** part of the
  published `v0.2.1` build (tag `9ca71333` + assets remain untouched); they
  would only ship in a future version.

## 7. Owner/handoff

Evidence curation owner: omp / 88kbleirad (this repo). Backlog for the next phase
is tracked separately (see `docs/release/phase11-next-backlog.md`).
