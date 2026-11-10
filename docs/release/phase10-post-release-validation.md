# Phase 10.1 — Post-Release Validation (v0.1.0)

Date: 2026-11-10.

## PR
- PR: [#1](https://github.com/88kbleirad/GoShiSen-Enforce/pull/1) — `test: harden fail-closed integration coverage for v0.1.0`
- Original PR head SHA: `44329230985b6efa3293128c181c6a8aa4304f26`
- Final main merge SHA (merge commit): `8d61f26d6ad71bfff86feaf441b308f71f133bd9`
- Merge method: merge commit (`gh pr merge 1 --merge`)
- PR CI: `42820969029` (PR), `42820959899` (push) — build PASS, integration 21/21 PASS, 0 skips

## Main validation
- Main HEAD after release fix: `55ffa8a` (version-stamp fix: `const` → `var`; CI regression guard)
- Main CI run: `42826379102` (merge commit) — build PASS, integration 21/21 PASS, 0 skips
- Fix CI run: `42827804359` — build PASS (incl. `Verify version stamp` step), integration 21/21 PASS, 0 skips
- Docs CI run: `42828018242` — PASS
- Build results: `go mod verify` / gofmt / `go vet` / `go test -race` / `go build` / CLI smoke all PASS
- Privileged integration: 21/21 PASS (159.674s), 0 mandatory skips, cleanup PASS

## Release tag
- Tag: `v0.1.0` (annotated)
- Original tag peeled commit: `8d61f26d6ad71bfff86feaf441b308f71f133bd9` (pre-fix; deleted because the build reported `dev` — recreated with authorization)
- Final tag peeled commit: `17490fd89d075a66b8518c652194c5fb76e21283` (fix + docs)
- Remote verification: `git ls-remote --tags origin v0.1.0` → `83a51b663872b6e2c986e4880b46d794bf330bc1` (annotated object)

## Tag CI
- Original tag CI run: `42827011604` (pre-fix, before recreation)
- Recreated tag CI run: `42828536762`
- Build result: PASS (build/vet/race/smoke + version-stamp)
- Integration test count: 21/21 PASS
- Mandatory skips: 0
- Cleanup: PASS

## GitHub Release
- Release URL: https://github.com/88kbleirad/GoShiSen-Enforce/releases/tag/v0.1.0
- State: draft → published (2026-11-10T13:56:35Z)
- Release notes: from `docs/release-notes-v0.1.0.md`, reviewed
- Artifacts: source archives (zip/tarball, HTTP 200 verified), no binaries attached (not in scope)
- No duplicate tag/release exists

## Clean-checkout validation
- `git clone --branch v0.1.0` → HEAD `17490fd` (matches tag peel)
- `go mod verify` PASS; `go test ./...` all PASS
- `make build VERSION=v0.1.0` → `bin/goshisen --version` prints `goshisen v0.1.0`
- CLI smoke: `--help` works; missing config exits 1

## Security
- Fail-open: disabled (NFQA_CFG_F_FAIL_OPEN never set; test-verified)
- Fail-closed behavior: PASS
- Queue overflow protection: PASS (kernel queue_dropped grew; policy still enforced)
- Crash/restart recovery: PASS (SIGKILL → fail-closed drop; restart → recovery)
- Mandatory security tests skipped: 0

## Final verdict
RELEASED

## Known limitations
- Privileged integration requires NFQUEUE+conntrack kernel; unsupported kernels fail loudly (preflight).
- Conntrack timeouts are kernel defaults; suite verifies behavior without full expiry waits.
- v0.1.0 binaries must be built with `make build VERSION=v0.1.0`; the CLI reports `goshisen v0.1.0` (regression-guarded in CI).
