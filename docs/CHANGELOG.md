# Changelog

Status and notable history for GoShiSen-Enforce. Per-release detail lives in `docs/release/` (e.g. `v0.2.4-notes.md`, `proposal-next-version-scope.md`).

## Release status (snapshot, 2026-11-17)

- **v0.2.5** — latest release (published 2026-11-17): upgrade-hook packaging fix (issue #57). Release notes and assets: [Releases](https://github.com/88kbleirad/GoShiSen-Enforce/releases/tag/v0.2.5).
- **v0.2.4** — previous release: production-signed OpenWrt apk; signed CLI archives. Scope and release-run checklist: `docs/release/proposal-next-version-scope.md`.

## Details moved out of the README

- Integration suite: 21 netns scenarios (e2e, state, NAT, crash, overflow in `tests/integration/`), root-gated, run with `sudo make integration`; CI runs the full suite on GitHub-hosted runners (21/21 PASS, zero mandatory skips).
- OpenWrt validation: OpenWrt 25.12.6 x86/64 (kernel 6.12.112); service disabled by default; stateful IPv6 ACCEPT verified on kernel 6.12.112 with the canonical bidirectional `ESTABLISHED/RELATED` rule pattern — `docs/openwrt.md`, `docs/release/issue-5-investigation.md`.
- Allocation-free hot path (0 allocs/packet); benchmarks: `docs/design/performance.md`.
- Metrics inventory (12 families) and Grafana setup: `docs/release/metrics-grafana-validation.md`.
- Repository layout: `docs/developer-guide.md`.

## Audit fixes (2026-11-17, PR in review)

- **Strict port parsing (F-001):** policy `src_port`/`dst_port` matchers and `GOSHISEN_API_PORT`/`GOSHISEN_METRICS_PORT` env values must now be plain integers. Previously `fmt.Sscanf("%d")` silently accepted trailing garbage (`"80evil"` → 80), so a typo could change the enforced policy or the bound port instead of failing configuration.
- **Backend error attribution (F-002):** receive-side NFQUEUE socket errors are now reported as `kind=receive` (matching the documented `goshisen_backend_errors_total` label set) instead of `kind=send`.

## Security fixes (2026-11-17, PR in review)

- **Toolchain pinned to go1.27.4 (S-001):** 11 reachable standard-library
  vulnerabilities (net/http Range-header DoS, MaxBytesReader race, http2
  HPACK races, crypto/x509) under go1.27.1 are closed; `golang.org/x/net`
  v0.62.0 and `golang.org/x/sys` v0.49.0. The `go` directive is now
  `go 1.26.0` (requires Go ≥ 1.26; builds auto-download the pinned
  toolchain).
- **queue_num bounded to 0..65534 (S-002):** queue_num 65535 was accepted
  but could never be installed (the IPv6 queue rule at queue_num+1 would
  be `queue to 65536`, outside the u16 range); config now rejects it with
  a clear message.
