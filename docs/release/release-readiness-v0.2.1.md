# v0.2.1 Release Readiness Audit — Report (Prompt 25)

**Date:** 2026-11-14 (UTC)
**Audited tree:** `main` head `80edf70` (local == origin, working tree clean)
**Auditor:** omp / 88kbleirad

## 1. Version & tag state

| Item | State |
| --- | --- |
| Source version | `internal/cli.Version` / `internal/app.Version` = `"dev"` (link-time stamped by `make`/`scripts/package.sh`) |
| Makefile | `VERSION ?= $(shell git describe --tags --always --dirty)` → `v0.2.1-9-g80edf70` at audited HEAD |
| Tag `v0.2.1` | exists; annotated tag object `6eb19f3` → commit `9ca71333`; **SSH-verified** (`verification.verified=true`, reason=`valid`) |
| Release `v0.2.1` | published `2026-11-13T12:43:29Z`, not draft; assets: checksums.txt (188 B), goshisen-linux-amd64.tar.gz (2,987,476), goshisen-linux-arm64.tar.gz (2,698,988), sbom.spdx.json (18,063) SQL Re-downloaded and `sha256sum -c checksums.txt` → both OK. |
| Tree vs tag | main = tag + 9 commits; **code** changes since tag: #20 (reload-gate harness), PR #29 (e2e runner/preflight harness + `internal/app` readiness fix) |
| OpenWrt pkg | `openwrt/Makefile`: `PKG_VERSION=0.2.0`, `PKG_RELEASE=4` (unchanged; OpenWrt packaging explicit separate from CLI release) |

## 2. Change coverage post-tag (what the audit validated on main)

- Hermetic preflight fixture abstraction (`scripts/e2e-runner.sh` real_env/fixture_env; fixture-only unit tests).
- Real-preflight integration target (`tests/integration/preflight_test.go`, tag `preflight_integration`), runs in the privileged CI job **before** provisioning with the runner's PATH.
- Lab port gate only `5555`/`2222` (no 8088 false positives).
- `internal/app` readiness race fix (`defer d.ready.Store(false)`, commit `0fb278b` → squashed into `e94d60a`).
- Environment manifest written pre-preflight; pinned image/kmod checksums.

## 3. Validation results (all PASS)

| Check | Result |
| --- | --- |
| `go test ./tests/package -run 'Preflight|Runner' -count=1` | PASS |
| `go test ./tests/package -count=1` | PASS |
| `go test -race ./...` | PASS |
| `PATH=/usr/bin:/bin go test ./tests/package -count=1` (hermetic) | PASS |
| `go test -tags preflight_integration -run '^TestPreflightReal$' ./tests/integration -count=1` | PASS (BLOCKED → explicit skip locally; READY on provisioned runners) |
| `make check` | PASS (fmt, vet, test, static build) |
| Privileged OpenWrt e2e (instabox sandbox, main tree) | PASS — DROP (curl exit=28, verdicts +7), ACCEPT (HTTP 200, +13); cleanup rc=0; no lab leftovers |
| Package reproducibility | two `make package VERSION=0.2.1-ready` builds → amd64 archive **byte-identical**, checksums identical |
| `make verify-package` | PASS (arch/version/checksums/SBOM) |
| Published release assets | re-downloaded, checksums verified OK (untouched by PR #29) |
| Post-merge main CI | runs 43547823103 / 43548043021 / 43548389934 — all success 4/4; audit docs run **43549454924 @ `80edf70`** — **success 4/4** (build/vet/test, package, integration, openwrt e2e) |

## 4. Artifact / package / signature status

- Release artifacts exist and verify; they were built from tag `9ca71333` (NOT from current main).
- Signing: tag SSH-signed and verified; artifact signature files (`sbom.spdx.json.sig`, `.provenance.json.sig`, `public_key.pem`) present in the dist dir (from the signing work; not re-generated in this audit).
- No new keys created; no signing key changed.

## 5. Documentation gaps found & fixed

| Gap | Fix (commit) |
| --- | --- |
| Evidence index lacked a prompt-21 / PR-29 row | added row with squash SHA `e94d60a`, CI runs, evidence pointers — `80edf70` |
| Evidence-index claim "no binary/package behavior changed after the tag" was wrong for PR #29 (readiness fix is binary behavior) | corrected to explicitly scope post-tag code changes out of v0.2.1 — `80edf70` |
| `v0.2.1-notes.md` remains draft | appropriate: it documents the *tagged* release; PR #29 changes belong to a future version's notes — no change |

## 6. Verdict

**v0.2.1 release assets and tag are intact and verified — they can be re-confirmed as-is.**
Current `main` is **not** the released tree: it contains post-tag code
(PR #29) that a future release must pick up. If the intent is to release
the *current* main, that would be a **new version** (e.g. v0.2.2 /
v0.3.0) — which is out of scope here (no tag move, no release publish, no
version bump in this task).

### Blockers / next actions
- None for the existing v0.2.1 release (assets verified).
- For a future release: decide version + create tag + run the release
  workflow (requires explicit approval; not performed in this audit).
- `v0.2.1-notes.md` should be finalized only if a release is actually
  executed from the tag tree; current main notes for the next version
  (preflight harness + readiness fix) are not yet drafted.
