# v0.2.2 Release Readiness — Candidate Report (Prompt 26)

**Date:** 2026-11-14 (UTC)
**Candidate tree:** the candidate commit is **`main` @ `c5de6e8`**
(HEAD == origin/main; previous heads `18ab946`, docs `3a95a46`/
`cc9c625`/`5a29232`/`39f94ee`/`dc1fb88`, runner fixes `df6b0a0`/
`9147a0e`). Two code fixes landed during the audit — see §3.1:
- `df6b0a0`: `e2e-runner.sh` booted the lab's default image instead of
  the one it provisioned; it now passes `OPENWRT_IMAGE=$IMAGE`.
- `9147a0e`: teardown (`cleanup` → `openwrt-lab.sh down`) now runs with
  the same `sudo -E` privilege as boot, so a non-root sudo host no
  longer keeps a live QEMU/netns after PASS.
Validation results below were re-run on `main` @ `9147a0e`; since then
PRs #24/#25/#26/#27/#28 merged to main (`dcc8cb5`/`35f7aef`/`d09ecbc`/
`4c91d83`/`c5de6e8`): artifact signing/provenance, offload enforce-mode
disable, flowtable observability, arm64 runtime verification, downgrade
cleanup. Local validation re-run PASS on merged head (`make check`,
`go test -race`, `make package`+`verify-package`, sign+verify pipeline);
CI on merged head green (see evidence-index row 27).
**Auditor:** omp / 88kbleirad

> This report prepares the candidate **only**. Nothing here tags, versions,
> publishes, or changes keys. The decision to tag/publish v0.2.2 is
> explicitly out of scope and requires approval (see §8).

## 1. Version & tag state

| Item | State |
| --- | --- |
| Source version | `internal/cli.Version` / `internal/app.Version` = `"dev"` (link-time stamped by `make`/`scripts/package.sh`) |
| Makefile | `VERSION ?= $(shell git describe --tags --always --dirty)` → `v0.2.1-19-gdc1fb88` at final head |
| Tag `v0.2.1` | exists; annotated tag object `6eb19f3` → commit `9ca71333`; SSH-signed (ED25519 `SHA256:0gqDKmaCx9LINggxwf3ERSvmT+GvFD3hMwZp+OQakWc`) — **untouched** |
| Release `v0.2.1` | published `2026-11-13T12:43:29Z`; assets verified intact — **untouched** |
| Candidate | `main` @ `9147a0e` = tag + 15 commits (23 files, +2782/−62) + final docs head `dc1fb88` |
| OpenWrt pkg | `openwrt/Makefile`: `PKG_VERSION=0.2.0`, `PKG_RELEASE=4` — **unchanged** (independent package versioning; see §6) |

## 2. Change inventory `v0.2.1..main` (classified)

Since the v0.2.1 audit, the only code merge was **PR #29**
(squash `e94d60a`, "reproducible privileged NFQUEUE e2e"), plus
documentation commits (`f0c923a` → `18ab946`).

### Runtime behavior (ships in the binary)
- `internal/app` readiness race fix (`defer d.ready.Store(false)`): `ready`
  cleared on every exit path, including backend-first completion. **This is
  the only binary change vs v0.2.1.** No policy-engine, packet-path,
  verdict, config, CLI, or package behavior changed.

### Operational (dev/CI tooling, not product runtime)
- `scripts/e2e-runner.sh` (new): typed-preflight (CONFIG_ERROR/BLOCKED/
  MISSING_DEPENDENCY/RESOURCE/READY) → environment manifest → pinned-image
  provisioning (single retry on the udev/partx race) → boot → e2e →
  trap teardown → failure artifacts.
- `scripts/e2e-env-manifest.sh` (new): env manifest written before
  preflight (blocked runs still record it); skipped in fixture mode.
- `scripts/openwrt-lab.sh`: stale-pidfile detection (cmdline must carry
  `gsh-owrt0`), pre-clean of stale netns/tap/bridge/veth, `down` kills all
  lab QEMU + removes VETH.
- `scripts/openwrt-e2e.sh`: serial sanitizer maps non-printable bytes to
  `?` (was: keep raw ≥0x80 bytes); reload-gate ERE requires
  `source=sighup` + full-token numeric boundary (`rules=N([^0-9]|$)`).

### Test-only
- `tests/package/e2e_reproducibility_test.go` (new): hermetic fixture
  tests (state machine × exit codes, precedence, hermeticity).
- `tests/integration/preflight_test.go` (new): real-preflight integration
  target behind `preflight_integration` tag.
- `tests/openwrt_integration/gate_harness_test.go`,
  `gate_shell_test.go` (new): reload-gate regressions (13 tests).
- `cmd/goshisen/main_test.go` (new): CLI smoke tests.
- `Makefile`: `preflight-integration` target; `integration` added to
  `.PHONY`.

### CI
- `.github/workflows/ci.yml`: `openwrt-integration` job rewritten — real
  preflight gate step (`sudo -E env "PATH=$PATH" go test -tags
  preflight_integration ...`) before the runner step; runner replaces the
  manual provision/boot/e2e/teardown steps; failure artifacts uploaded.

### Documentation-only
- `docs/design/e2e-reproducibility-phase.md`, `docs/release/evidence-index.md`,
  `phase11-next-backlog.md`, `phase15-final-report.md`, `prompt09-`,
  `prompt19-`, `prompt20-`, `prompt21-`, `release-readiness-v0.2.1.md`.

## 3. Validation evidence (candidate `9147a0e`, 2026-11-14)

| Check | Result | Evidence |
| --- | --- | --- |
| `go test ./tests/package -count=1` | PASS | run log |
| `go test -race ./...` | PASS | run log |
| `PATH=/usr/bin:/bin go test ./tests/package -count=1` | PASS | run log |
| `go test -tags preflight_integration -run '^TestPreflightReal$' ./tests/integration -count=1` | PASS | run log |
| `make check` | PASS | fmt/vet/test/static build |
| `make package VERSION=0.2.2-ready` × 2 | **byte-identical** | amd64 `cfa3cf7e82352b88fe5e0b4a538c84ec1bb5d3d2f3c0f9b6363b5403d869df8b`, arm64 `20372150d1e6739bd98e62fe1a330d95bbc60a5aa47000f7a33a391698591e88` on both runs |
| `make verify-package VERSION=0.2.2-ready` | PASS | arch, `goshisen 0.2.2-ready` stamp, checksums, SPDX-2.3 SBOM |
| Privileged OpenWrt e2e runner (instabox, main tree) | PASS | `scripts/e2e-runner.sh` on instabox (`main` @ `9147a0e`, daemon `goshisen 0.2.2-ready`): preflight READY → image provisioned (checksum-pinned, `/data/owrt-p26.img`) → boot printed `booting lab (image: /data/owrt-p26.img)` and QEMU cmdline confirmed `-drive file=/data/owrt-p26.img` (console up 4s) → `TestOpenWrtE2E` PASS in 159.50s — DROP control PASS (curl exit=28, daemon drop verdicts +7, hook +7, queue drops +0), ACCEPT control PASS (HTTP 200 marker, accept verdicts +13, hook +13); all 14 gate tests PASS; cleanup rc=0; post-run host clean (no netns, no lab QEMU, no pidfile); artifacts in `/data/prompt26-artifacts3/` |

### 3.1 Found-and-fixed during the audit

1. **Stale-image boot.** The first runner invocation (on `18ab946`)
   looked green but ran the e2e against a stale image:
   `e2e-runner.sh` provisioned `--image PATH` yet booted
   `scripts/openwrt-lab.sh` without `OPENWRT_IMAGE`, so the lab fell
   back to its own default `/tmp/owrt.img` (which contained
   `goshisen 0.2.1-audit` from an earlier audit). Fix `df6b0a0`: the
   runner now passes `OPENWRT_IMAGE="$IMAGE"` to `lab up`; regression
   test `TestE2ERunnerBootsProvisionedImage` added (PASS). The re-run
   verified the booted image was the provisioned one.
2. **Unprivileged teardown.** `cleanup()` ran `openwrt-lab.sh down`
   without `sudo -E`; on a non-root sudo host the root-owned QEMU/netns/
   tap from `lab up` survived PASS (observed twice on instabox). Fix
   `9147a0e`: `down` now runs under the same privilege as `up`, leaving
   a clean host (verified: no netns, no lab QEMU, no pidfile after
   PASS on the final run).

## 4. Reproducibility

- Two `make package VERSION=0.2.2-ready` builds → amd64 & arm64 tarballs
  byte-identical; checksums identical (build-id stripped, mtimes
  normalized, `--sort=name --owner=0 --group=0`).
- SBOM generated with pinned `syft` v1.30.0 (checksum-verified install).
- Verification will reuse the **current** signing key (the SSH key that
  signed `v0.2.1`, fingerprint `SHA256:0gqDKmaCx9LINggxwf3ERSvmT+GvFD3hMwZp+OQakWc`);
  no new keys created, no key changes.

## 5. Versioning / package decision

- CLI: source version stays `dev`; tag `v0.2.2` stamps binaries at build
  time via the existing `make package VERSION=$(git describe ...)` flow
  (same as v0.2.1).
- **OpenWrt package: keep `PKG_VERSION=0.2.0`, `PKG_RELEASE=4` — not
  bumped.** Project policy keeps the OpenWrt package version independent of
  the CLI release (package is versioned by its SDK-verified source pin,
  `PKG_SOURCE_VERSION=f5f211a…` + `PKG_MIRROR_HASH`). v0.2.2 changes no
  package/firmware-facing behavior; no package rebuild or version bump is
  warranted by this release. Decision recorded here and in
  `v0.2.2-notes.md` §Versioning.

## 6. Release checklist (publish steps after approval)

Pre-approval (done):
- [x] Repo state: `main` HEAD == origin; working tree clean.
- [x] Docs: `v0.2.2-notes.md` draft; this report; evidence-index row.
- [x] Validation suite PASS (see §3).
- [x] Reproducibility double-build byte-identical; `make verify-package` PASS.
- [x] Signing key confirmed unchanged.
- [ ] Privileged OpenWrt e2e runner PASS (instabox) — final evidence in §3.

Post-approval (NOT performed here):
1. [ ] Create annotated, SSH-signed tag: `git tag -s v0.2.2 -m "GoShiSen-Enforce v0.2.2" <candidate>` (same key as v0.2.1) and push it.
2. [ ] Tag push triggers `release.yml` (tag-only): builds archives, verifies, creates a **DRAFT** release with assets (never auto-publishes).
3. [ ] Verify the draft release: `gh release view v0.2.2`; download assets; `sha256sum -c checksums.txt`; binary reports `goshisen v0.2.2`; SBOM valid.
4. [ ] Attach signing/provenance artifacts (per-artifact `*.provenance.json(.sig)`, `provenance-index.txt.sig`, `public_key.pem`) — the release upload MUST include them or downloaded verification fails exit 8.
5. [ ] Verify signatures with `scripts/verify-signatures.sh --dist DIR --public-key PUB.pem --commit <sha> --require-sign`.
6. [ ] Publish: `gh release edit v0.2.2 --draft=false`; confirm release page/notes.
7. [ ] Post-release: verify tag signature (`gh api repos/<o>/<r>/git/tags/...` → verified=true), re-download assets, checksums OK; record in evidence index.
8. [ ] Do NOT touch v0.2.1 tag/assets; do NOT change signing keys.

## 7. Verdict

**Candidate `main` @ `18ab946` is ready to be released as v0.2.2** once
the privileged OpenWrt e2e runner evidence is appended and the approval
below is given. No blockers found in the candidate itself.

### Remaining before publish
- Append instabox e2e runner result to §3 (pending).
- Human approval to tag + publish v0.2.2 from `18ab946` (or the final
  candidate commit if more commits land before approval — re-run the §3
  suite on the final head first).
