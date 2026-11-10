# Phase 9 — Packaging & Distribution Validation (v0.2.0 prep)

Date: 2026-11-10. Status: complete — all local + CI validation PASS.

## Baseline
- Released: v0.1.0 (`17490fd`), CI green, 21/21 integration PASS, 0 skips.
- Branch: `phase9-packaging` (PR #2) — commits `bd4f653`, `f431554`, `4bdd567`.

## Local validation (all PASS)
| Check | Result |
|---|---|
| `go mod verify` | PASS |
| `gofmt -l .` | clean |
| `go vet ./...` | PASS |
| `go test ./... -count=1` | PASS (incl. `tests/package`) |
| `go test -race ./... -count=1` | PASS |
| `go build ./...` | PASS |
| `git diff --check` | clean |
| `make package VERSION=…` | archives + checksums + SBOM built |
| `make verify-package VERSION=…` | arch/static/version/checksums/SBOM OK |
| Reproducibility | byte-identical archive across rebuilds |
| `tests/package` | PASS (contents, arch, version, checksum, reproducibility) |

## Compatibility matrix
| Configuration | Evidence |
|---|---|
| linux/amd64, CGO_ENABLED=0 | static ELF x86-64; runs; version stamp verified |
| linux/arm64, CGO_ENABLED=0 | static ELF aarch64; stamp present; execution needs arm64 host |
| Reproducible builds | `-trimpath -buildvcs=false -buildid=` + normalized tar mtimes → identical bytes |
| Runtime deps | `nft` binary, `/proc/net/netfilter/nfnetlink_queue`, CAP_NET_ADMIN (+NET_RAW) |

## CI (GitHub, ubuntu-24.04)
- Run `42832928519` (first): build PASS, integration PASS (21/21), **package FAIL** — syft archive filename mismatch in checksum verification (workflow bug, not product bug). Fixed in `4bdd567`.
- Run `42833523627` (after fix): build PASS, integration 21/21 PASS (ok 152.963s, 0 skips), package PASS (arch/version/checksums/SBOM + reproducibility). Also verified on `42833539637` (push run). PR #2 checks all green.

## Security preservation
- Integration job unchanged; zero-skips gate intact; fail-closed invariants untouched.
- Packaging scripts additive: build-only; no nftables/firewall interaction.
- Actions pinned to immutable SHAs; least-privilege `contents: read` (CI) / `contents: write` (release workflow, tag-only).
- Release workflow attaches artifacts to a DRAFT release — never auto-publishes.
