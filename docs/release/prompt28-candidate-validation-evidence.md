# v0.2.2 Candidate Validation — Prompt 28 Evidence

**Candidate:** `c5de6e8ec52a55b84ed16bdb3c9e5cff654f4dc6` (main HEAD at
candidate time; docs-only successor `74846c882088971ea8620ccd10ffe79e423260f9`).
**Workstation:** CachyOS Linux 7.3.3-1 (x86_64), Go 1.27.3 (`go1.27.3-X:nodwarf5`).
**Date:** 2026-11-14 (UTC). All commands run in a clean checkout at the
exact candidate SHA. No secrets in this log.

## 1. Local validation runs (all PASS)

| Command | Result |
| --- | --- |
| `git checkout c5de6e8ec52a55b84ed16bdb3c9e5cff654f4dc6` | HEAD = full candidate SHA |
| `go test ./tests/package -count=1` | `ok ... tests/package 5.032s` |
| `go test -race ./...` | `ok` for cmd/goshisen, internal/{api,app,cli,config,hook,metrics,packet,policy}, tests/{integration,openwrt_integration,package} |
| `PATH=/usr/bin:/bin go test ./tests/package -count=1` | `ok ... 1.450s` (hermetic) |
| `go test -tags preflight_integration -run '^TestPreflightReal$' ./tests/integration -count=1` | `ok ... 0.067s` |
| `make check` | fmt/vet/test pass; static build `bin/goshisen (v0.2.1-25-gc5de6e8)` |
| `make package VERSION=0.2.2-ready` × 2 (clean `dist`/`build` between runs) | byte-identical outputs |
| `make verify-package VERSION=0.2.2-ready` | `OK amd64: goshisen 0.2.2-ready`; `OK arm64` (stamp present); `OK checksums + SBOM` |
| sign+verify pipeline (ephemeral fixture Ed25519 key) | `VERIFY-PASS` all 4 artifacts; tamper control → `VERIFY-FAIL: bad-signature` exit 4 |

## 2. Candidate release archive checksums (VERSION=0.2.2-ready, both builds identical)

| Artifact | SHA-256 |
| --- | --- |
| `goshisen-linux-amd64.tar.gz` | `25d72b271ad87f8cc1c55b5f3197a14ff253628576da1534c1af33fb7a728b09` |
| `goshisen-linux-arm64.tar.gz` | `440b16f8695db225f6b36fe33c349dceba45517a0009368b2e5c3888edb04df6` |
| `checksums.txt` | `5703d2c9f05f8ed55cad55e8513a072dd12964078ffef664ee98ec111390f152` |

## 3. CI (GitHub Actions, full head SHAs)

| Run | Head | Result |
| --- | --- | --- |
| 43560836375 | `c5de6e8` | 6/6 jobs success: build/vet/test, verify-signatures (test key), privileged integration, arm64 cross-build+static ELF, privileged openwrt e2e, package+verify |
| 43561323620 | `74846c8` | 6/6 jobs success (same jobs) |

## 4. Signing readiness

- Production anchor read from candidate tree `keys/goshisen-release.pub.pem`:
  full fingerprint `24215e016621925215dec0ad6ea14ac74253a4e97249a973e89d649a2217cca0`,
  key ID `f955ede984245d64`.
- All five signing scripts exist at candidate (`sign-artifacts.sh`,
  `verify-signatures.sh`, `release-sign.sh`, `check-key-match.sh`,
  `verify-single-artifact.sh`); `release.yml` tag-only, signing step wired
  to `GH_RELEASE_SIGNING_KEY`, DRAFT-only.
- Production-key signing NOT yet executed (requires tag push → DRAFT);
  negative controls with foreign keys recorded in evidence-index row 27.

## 5. OpenWrt package pin

- `openwrt/Makefile` at candidate: `PKG_VERSION=0.2.0`,
  `PKG_RELEASE=4`, `PKG_SOURCE_VERSION=f5f211a173dab2934a42e3aa8220ab2d54fa12da`.
- Package recipe at candidate includes prerm/lifecycle/init changes;
  daemon binary inside the built `.apk` comes from the pin (f5f211a
  features only). Independent package versioning per project policy;
  package pin bump is a separate task.
