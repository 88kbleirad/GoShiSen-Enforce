# Changelog

Status and notable history for GoShiSen-Enforce. Per-release detail lives in `docs/release/` (e.g. `v0.2.4-notes.md`, `proposal-next-version-scope.md`).

## Release status (snapshot from README, 2026-11-17)

- **v0.2.4** — latest release: production-signed OpenWrt apk; signed CLI archives.
- **v0.2.5** — staged as a DRAFT release pending maintainer review: upgrade-hook packaging fix (issue #57). Scope and release-run checklist: `docs/release/proposal-next-version-scope.md`.

## Details moved out of the README

- Integration suite: 21 netns scenarios (e2e, state, NAT, crash, overflow in `tests/integration/`), root-gated, run with `sudo make integration`; CI runs the full suite on GitHub-hosted runners (21/21 PASS, zero mandatory skips).
- OpenWrt validation: OpenWrt 25.12.6 x86/64 (kernel 6.12.112); service disabled by default; stateful IPv6 ACCEPT verified on kernel 6.12.112 with the canonical bidirectional `ESTABLISHED/RELATED` rule pattern — `docs/openwrt.md`, `docs/release/issue-5-investigation.md`.
- Allocation-free hot path (0 allocs/packet); benchmarks: `docs/design/performance.md`.
- Metrics inventory (12 families) and Grafana setup: `docs/release/metrics-grafana-validation.md`.
- Repository layout: `docs/developer-guide.md`.
