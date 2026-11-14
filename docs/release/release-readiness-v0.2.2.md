# v0.2.2 Release Readiness — Candidate Report (Prompt 28)

**Date:** 2026-11-14 (UTC)
**Candidate tree:** the candidate commit is **`main` @ `c5de6e8`**
(full SHA `c5de6e8ec52a55b84ed16bdb3c9e5cff654f4dc6`; HEAD == origin/main
at candidate time). Docs-only successor `74846c8` (full SHA
`74846c882088971ea8620ccd10ffe79e423260f9`, PR #30) is NOT part of the
candidate build — same pattern as v0.2.1 (tag at code commit, docs after).
Prior candidate `9147a0e` superseded by the Phase 14.2 merges.

**Auditor:** omp / 88kbleirad

> This report prepares the candidate **only**. Nothing here tags, versions,
> publishes, or changes keys. The decision to tag/publish v0.2.2 is
> explicitly out of scope and requires approval (see §8).

## 1. Version & tag state

| Item | State |
| --- | --- |
| Source version | `internal/cli.Version` / `internal/app.Version` = `"dev"` (link-time stamped by `make`/`scripts/package.sh`) |
| Makefile | `VERSION ?= $(shell git describe --tags --always --dirty)` → `v0.2.1-25-gc5de6e8` at candidate |
| Tag `v0.2.1` | exists; annotated tag object `6eb19f3` → commit `9ca71333`; SSH-signed (ED25519 `SHA256:0gqDKmaCx9LINggxwf3ERSvmT+GvFD3hMwZp+OQakWc`) — **untouched** |
| Release `v0.2.1` | published `2026-11-13T12:43:29Z`; assets verified intact — **untouched** |
| Candidate | `main` @ `c5de6e8` = tag + 25 commits (64 files, +6440/−160) |
| `v0.2.2` tag/release | **does not exist** on remote (verified `git ls-remote --tags`) |
| OpenWrt pkg | `openwrt/Makefile`: `PKG_VERSION=0.2.0`, `PKG_RELEASE=4`, `PKG_SOURCE_VERSION=f5f211a…` — **unchanged from v0.2.1** (independent package versioning; see §5) |

## 2. Change inventory `v0.2.1..c5de6e8` (classified)

### Runtime behavior (ships in the binary)
- `internal/hook/offload.go` — flow-offload state machine
  (disabled/enabled/failed/unknown), `offload_policy=enforce` fail-closed
  **disable**, `allow_bypass` diagnostic hatch (enforce-only), watchdog
  re-disable, flowtable hit-counter parsing for `offload observe`
  (PRs #25/#26).
- `internal/cli/cli.go` — new CLI `offload status` / `offload disable` /
  `offload observe` (PRs #25/#26).
- `internal/metrics/metrics.go` — `goshisen_flow_offload_active` +
  `goshisen_flowtable_hits_packets`/`_available` (PRs #15/#16).
- `internal/app/app.go` — readiness race fix
  (`defer d.ready.Store(false)`); `config show` surfaces offload config
  (PRs #25/#26).
- `internal/config/config.go` — `offload_policy=enforce` + `allow_bypass`
  validation.
- Signing tooling is release-time infra, not daemon runtime.

### OpenWrt package tree (source, not in the pinned .apk — see §5)
- `openwrt/files/lib/goshisen-lifecycle.sh` + `openwrt/Makefile` prerm —
  unconditional rc.d link removal on uninstall (PR #28).
- UCI `allow_bypass` YAML boolean rendering (`b2e35c2`).

### Operational (dev/CI tooling)
- `scripts/e2e-runner.sh` (new): typed-preflight → env manifest → pinned
  provisioning → boot → e2e → trap teardown → failure artifacts; boots the
  provisioned image; tears down with same privilege as boot.
- `scripts/e2e-env-manifest.sh` (new); `scripts/openwrt-lab.sh`
  hardening; `scripts/openwrt-e2e.sh` reload-gate ERE + serial sanitizer.
- `scripts/sign-artifacts.sh`, `verify-signatures.sh`, `release-sign.sh`,
  `check-key-match.sh`, `verify-single-artifact.sh` (new, PR #24).

### Test-only
- `tests/package/`: signing (16 tests), arm64 cross-build, offload unit
  suites, e2e reproducibility, downgrade cleanup, openwrt init.
- `tests/integration/preflight_test.go` (real, tagged); `tests/openwrt_integration/*` gate regressions; `cmd/goshisen/main_test.go`.
- `Makefile`: `preflight-integration` target.

### CI
- `.github/workflows/ci.yml`: `verify-signatures` job (test key,
  fail-closed tamper), `arm64` job, `openwrt-integration` runner job.
- `.github/workflows/release.yml`: tag-only; "Sign release artifacts with
  production key" step; DRAFT-only (never auto-publishes).

### Documentation-only
- `docs/design/{e2e-reproducibility,offload-disable,offload-observability,openwrt-downgrade-cleanup}-phase.md`, `signing-design.md`,
  `docs/release/ev-index`, `phase11-next-backlog`, prompt evidence files,
  `v0.2.2-notes.md`, release-readiness reports.

## 3. Validation evidence (candidate `c5de6e8`, 2026-11-14)

Local (workstation, Go 1.27.3, linux/amd64, checkout at exact candidate):

| Check | Result | Evidence |
| --- | --- | --- |
| `go test ./tests/package -count=1` | PASS | local run |
| `go test -race ./...` | PASS | local run (all pkgs) |
| `PATH=/usr/bin:/bin go test ./tests/package -count=1` | PASS | hermetic, local run |
| `go test -tags preflight_integration -run '^TestPreflightReal$' ./tests/integration -count=1` | PASS | local run |
| `make check` | PASS | fmt/vet/test/static build |
| `make package VERSION=0.2.2-ready` × 2 (clean dist/build) | **byte-identical** | both arches + checksums identical across builds |
| `make verify-package VERSION=0.2.2-ready` | PASS | arch, `goshisen 0.2.2-ready` stamp, checksums, SPDX-2.3 SBOM |
| Sign+verify pipeline (ephemeral fixture Ed25519 key) | PASS | `VERIFY-PASS` all 4 artifacts; tamper negative control → exit 4 |
| Signing regression tests | PASS | `signing_artifacts_test.go` (9) + `production_signing_test.go` (7) |
| Privileged OpenWrt e2e runner | PASS | CI run 43560836375 job `openwrt e2e` (QEMU lab, privileged) success @ candidate head; instabox runner run (`TestOpenWrtE2E`, 14/14 gates) documented in prompt 26 evidence — daemon code at candidate identical modulo readiness fix |
| arm64 runtime | PASS | CI run 43560836375 job `arm64 cross-build + static ELF` success @ candidate head; live armsr/armv8 QEMU TCG evidence (prompt 17) — daemon code at candidate identical to lab source modulo readiness fix |

CI (GitHub Actions, full head SHAs):

| Run | Head | Result |
| --- | --- | --- |
| 43560836375 | `c5de6e8` | **6/6 jobs success**: build/vet/test; verify-signatures (test key, fail-closed); integration (privileged); arm64 cross-build + static ELF; openwrt e2e (privileged); package+verify |
| 43561323620 | `74846c8` (docs-only) | **6/6 jobs success** (same jobs) |

### 3.1 Found-and-fixed during the audit

1. **Stale-image boot** (`df6b0a0`) and **unprivileged teardown**
   (`9147a0e`): runner fixes from prompt 26, merged and re-verified.
2. **`needCmd` duplicate** removed from `arm64_build_test.go` when
   merging PR #27 (conflicted with PR #24's `signing_artifacts_test.go`).

## 4. Reproducibility

- Two `make package VERSION=0.2.2-ready` builds at the candidate → amd64 &
  arm64 tarballs byte-identical; checksums identical.
- SBOM generated with pinned `syft` v1.30.0 (checksum-verified install).
- Artifact signing: production anchor `keys/goshisen-release.pub.pem`
  full fingerprint **`24215e016621925215dec0ad6ea14ac74253a4e97249a973e89d649a2217cca0`** (key ID `f955ede984245d64`) — reads straight from the
  candidate tree. Instabox ECDSA test key (`a8253141…`) and the
  unregistered local keypair (`e9b4bf11…`) are rejected by the production
  verifier path (exit 4) — negative controls executed and recorded in
  evidence-index row 27; neither is a trust anchor.

## 5. Versioning / package decision

- CLI: source version stays `dev`; tag `v0.2.2` stamps binaries at build
  time.
- **OpenWrt package: `PKG_VERSION=0.2.0`, `PKG_RELEASE=4`, source pin
  `f5f211a173dab2934a42e3aa8220ab2d54fa12da` — unchanged at the
  candidate.** The `.apk` built from this pin ships the f5f211a daemon;
  it does **NOT** contain the v0.2.2 signing, enforce-mode offload
  disable, observability, or downgrade-cleanup package-tree changes.
  Per project policy the package is versioned independently of the CLI
  release; this is a documented gap (package pin bump + SDK rebuild is a
  separate task) and not a release blocker for the CLI release.
- The release tag `v0.2.2` does not imply an OpenWrt package bump.

## 6. Signing readiness audit

- Trust anchor exists at candidate: `keys/goshisen-release.pub.pem`
  (fingerprint above). Signing scripts present at candidate (all five).
- `release.yml` at candidate: tag-only push trigger; `release-sign.sh`
  step wired to `secrets.GH_RELEASE_SIGNING_KEY`; fails closed on missing
  secret / key mismatch; key trap-removed; release created DRAFT-only.
- Secret existence is metadata-only (API returns 401 to the public token,
  expected; the secret was created 2026-11-13 and is unreadable via API,
  consistent with prior sessions). **Secret existence does NOT prove the
  private key matches the public anchor** — that is verified at release
  time by `check-key-match.sh` inside `release-sign.sh`.
- Negative controls (test/ephemeral keys) verified locally and in CI;
  **no production-key signing run has been executed** — that gate can
  only be exercised when the tag is pushed (DRAFT release) and the assets
  are verified with the production anchor, before publish.

## 7. Release checklist (publish steps after approval)

Pre-approval (done):
- [x] Repo state: candidate `c5de6e8` verified (full SHA); origin/main
  has only docs after it (`74846c8`); working tree clean.
- [x] Docs: `v0.2.2-notes.md` draft rewritten to merged scope; this
  report; evidence-index row (prompt 28).
- [x] Validation suite PASS at exact candidate (see §3).
- [x] Reproducibility double-build byte-identical; `make verify-package` PASS.
- [x] Artifact signing readiness: anchor fingerprint verified; scripts
  present; test-key pipeline PASS; tamper control PASS.
- [ ] Privileged OpenWrt e2e runner evidence (CI job at candidate head) —
  DONE in §3 (CI 43560836375).
- [ ] Production-key signing run — only possible on tag push (DRAFT);
  not executable pre-approval.

Post-approval (NOT performed here):
1. [ ] Create annotated, SSH-signed tag: `git tag -s v0.2.2 -m "GoShiSen-Enforce v0.2.2" c5de6e8ec52a55b84ed16bdb3c9e5cff654f4dc6` (same key as v0.2.1); verify signature/target; push tag.
2. [ ] Tag push triggers `release.yml` (tag-only): builds archives, verifies, signs with production key, creates a **DRAFT** release with assets (never auto-publishes).
3. [ ] Read the release workflow run: confirm the `Sign release artifacts with production key` step PASSed (production key verified by `check-key-match.sh` against anchor).
4. [ ] Verify the draft release: `gh release view v0.2.2`; download assets; `sha256sum -c checksums.txt`; binary reports `goshisen v0.2.2`; SBOM valid.
5. [ ] Verify signatures/provenance with the **production anchor**: `scripts/verify-signatures.sh --dist DIR --public-key keys/goshisen-release.pub.pem --commit c5de6e8ec52a55b84ed16bdb3c9e5cff654f4dc6 --require-sign` → must PASS (exit 0).
6. [ ] Publish: `gh release edit v0.2.2 --draft=false`; confirm release page/notes.
7. [ ] Post-release: re-download assets; checksums OK; signatures/provenance verified with production anchor; tag signature verified (`gh api .../git/tags/...` → verified=true); record in evidence index.
8. [ ] Do NOT touch v0.2.1 tag/assets; do NOT change signing keys. If any verification FAILs, keep the DRAFT, do not publish (no unsigned fallback).

## 8. Verdict

**Candidate `main` @ `c5de6e8` is ready to be released as v0.2.2**
(approval required). Validation green at the exact candidate; CI 6/6 on
the candidate head; signing tooling present with verified production
anchor; the only gate that cannot be pre-run is the production-key signing
step, which occurs inside the release workflow on tag push and must be
verified on the DRAFT before publishing.

### Remaining before publish
- Human approval to tag + publish v0.2.2 from `c5de6e8`.
- Execute the post-approval checklist (§7).
