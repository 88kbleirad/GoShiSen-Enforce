# Phase 10 — Release Validation Evidence (v0.1.0)

Date: 2026-11-10. Status: in progress — merge/tag/release pending explicit authorization.

## Baseline (Phase 9.2, verified)
- PR #1: `test: harden fail-closed integration coverage for v0.1.0` → main.
- Verified RC: `5bd1c57f4b65b46e5d0c172aff7dc1d030b99112` — CI green (build PASS; integration 21/21 PASS, 0 skips) on ubuntu-24.04 kernel 7.0.0-1017-azure, Go 1.25.14.
- Draft PR OPEN; main unprotected (no required reviews — author cannot self-approve, none needed).

## Tag CI trigger (Step B)
- Added `tags: ["v*"]` to `.github/workflows/ci.yml` push trigger; committed `f1f09c1a5db024d33ea5d4c2730363bd900c7170`.
- Workflow YAML validated; all jobs preserved (build/vet/race/integration, skip-rejection, streamed output, cleanup).
- Candidate PR run: `42819392387`; push run: `42819383699`.
- Final head (docs evidence commit): `77ce4d2dfb6dba4807e44f83fcd72c00d47c64d8`; PR run `42819556665`, push run `42819546461`.

## Final release gate (Step C)
| Gate | Result |
|---|---|
| Final PR CI (f1f09c1) | pending |
| Mandatory skips | 0 (21/21 at 5bd1c57) |
| Review requirements | none (main unprotected) |
| Secret scan | clean (no credentials in diff) |
| Release notes | `docs/release-notes-v0.1.0.md` present, reviewed |
| Version consistency | `make build VERSION=v0.1.0` supported (Makefile ldflags) |

## Merge / tag / release (steps D–I) — NOT AUTHORIZED YET
- Merge method proposed: merge commit (preserves 40-commit phase history; no repo convention contradicts).
- Tag: `v0.1.0` (annotated) on final main commit, resolved after merge.
- Tag CI must pass (build + integration, zero skips) before release publication.
- GitHub Release: draft first via `gh release create v0.1.0 --draft --notes-file docs/release-notes-v0.1.0.md`; publish only after tag CI passes and publication is authorized.

## Outstanding limitations
- Privileged suite requires NFQUEUE+conntrack kernels; unsupported kernels fail loudly (preflight + CI skip-rejection).
- Conntrack timeouts are kernel defaults; suite verifies behavior without full expiry waits.
