# Changelog

Status and notable history for GoShiSen-Enforce. Per-release detail lives in `docs/release/` (e.g. `v0.2.6-notes.md`, `proposal-next-version-scope.md`).

## Release status (snapshot, 2026-11-21)

- **v0.2.7** — latest release (published 2026-11-21): hardening release — port-zero semantics (v0.2.6 docs-gap), `api_host` loopback guard, metrics method hardening, listener-defaults docs correction. Release notes and assets: [Releases](https://github.com/88kbleirad/GoShiSen-Enforce/releases/tag/v0.2.7).
- **v0.2.6** — previous release (published 2026-11-19): S-005 toolchain-fix release (issue #78) — apk payload and both CLI archives built with go1.27.4.

## Post-v0.2.7 hardening (merged 2026-11-21, next release)

- **Bounded file reads (#104):** config/policy/store reads now reject
  non-regular files before opening and cap at 16 MiB — a policy path
  pointing at /dev/urandom previously hung forever (memory ~1 GiB/s).
- **CI robustness (#105–#107):** the 257-case CLI anomaly sweep and the
  config fuzz targets are committed and run in CI, alongside staticcheck
  (empty baseline) and govulncheck source mode.
- **UCI api_token (#108, closes #103):** OpenWrt config can now set the
  API token; the rendered daemon config is chmod 600 and the token
  charset is restricted (A-Za-z0-9._-); the init script warns when the
  API listener is enabled without a token.

## Details moved out of the README

- Integration suite: 21 netns scenarios (e2e, state, NAT, crash, overflow in `tests/integration/`), root-gated, run with `sudo make integration`; CI runs the full suite on GitHub-hosted runners (21/21 PASS, zero mandatory skips).
- OpenWrt validation: OpenWrt 25.12.6 x86/64 (kernel 6.12.112); service disabled by default; stateful IPv6 ACCEPT verified on kernel 6.12.112 with the canonical bidirectional `ESTABLISHED/RELATED` rule pattern — `docs/openwrt.md`, `docs/release/issue-5-investigation.md`.
- Allocation-free hot path (0 allocs/packet); benchmarks: `docs/design/performance.md`.
- Metrics inventory (12 families) and Grafana setup: `docs/release/metrics-grafana-validation.md`.
- Repository layout: `docs/developer-guide.md`.

## Audit fixes (shipped in v0.2.6, 2026-11-18)

- **Strict port parsing (F-001):** policy `src_port`/`dst_port` matchers and `GOSHISEN_API_PORT`/`GOSHISEN_METRICS_PORT` env values must now be plain integers. Previously `fmt.Sscanf("%d")` silently accepted trailing garbage (`"80evil"` → 80), so a typo could change the enforced policy or the bound port instead of failing configuration.
- **Backend error attribution (F-002):** receive-side NFQUEUE socket errors are now reported as `kind=receive` (matching the documented `goshisen_backend_errors_total` label set) instead of `kind=send`.

## Security fixes (shipped in v0.2.6, 2026-11-19)

- **Release-tag injection guard (S-003):** the release workflow now
  validates the pushed tag name against `^v[0-9]+\.[0-9]+\.[0-9]+$` in
  a first step before anything else, passes it through an environment
  variable, and quotes it at every use; `apk-sign.yml` production mode
  validates its ref the same way. A `release-tag-guard` CI job proves
  tag-name fixtures containing shell metacharacters are rejected before
  use. No public reproduction steps are included until this lands.
- **Container images pinned by digest (S-004):** `openwrt/sdk`
  (`apk-sign.yml`), `golang:1.25` and `gcr.io/distroless/base-debian13`
  (Dockerfile) are digest-pinned; update notes at each pin.
- **CI actions on Node 24 runtime:** actions/checkout v7.0.1,
  actions/setup-go v7.1.0, actions/upload-artifact v7.0.2 (SHA-pinned),
  removing the Node.js 20 deprecation warnings on every job.
- **Dockerfile and CI on go1.27.4 (S-001):** Dockerfile base images
  bumped from golang:1.25 (go1.25.14, 11 reachable stdlib vulns) to
  `golang:1.27.4@sha256:205c7636…`; all CI jobs install go1.27.4.
- **apk payload gates (S-005):** the published v0.2.5 apk payload was
  built with go1.27.1 → 11 reachable stdlib vulns (issue #78). Closed
  in v0.2.6 (apk payload + both CLI archives built with go1.27.4). Two
  fail-closed signing-path gates now run in `apk-sign.yml` production
  mode before signing, plus CI regression jobs:
  - `scripts/check-apk-payload-go.sh` rejects any apk whose payload was
    built with Go < `MIN_GO_VERSION` (`packaging/go-toolchain.env`,
    1.27.4);
  - `scripts/check-apk-payload-govuln.sh` runs govulncheck binary-mode
    on the extracted payload and fails on any reachable stdlib/module
    vulnerability not in `packaging/govulncheck-allowlist.txt` (empty
    by default). The payload must carry readable Go buildinfo, so the
    v0.2.6 package recipe drops `-buildid=` and sets `STRIP:=true` —
    the SDK's default `sstrip` removes the ELF section string table,
    which breaks `go version -m` and binary-mode scanning even when
    `-buildid=` is kept (the v0.2.5 recipe stripped both, defeating
    binary-mode scanning entirely).
  The current SDK image's baked feed does NOT yet build Go 1.27.4
  (feed default go1.26.9, golang1.27 go1.27.1; repinning the feed to a
  newer commit breaks this SDK's metadata scan) — the v0.2.6 build
  mounts a sha256-verified go1.27.4 toolchain
  (`scripts/sdk-build-v026.sh`, option 3 in the S-005 report). The
  shipped v0.2.5 apk is unchanged.
- **Toolchain pinned to go1.27.4 (S-001):** 11 reachable standard-library
  vulnerabilities (net/http Range-header DoS, MaxBytesReader race, http2
  HPACK races, crypto/x509) under go1.27.1 are closed; `golang.org/x/net`
  v0.62.0 and `golang.org/x/sys` v0.49.0. The `go` directive is now
  `go 1.26.0` (requires Go ≥ 1.26; CI installs go1.27.4 directly).
- **queue_num bounded to 0..65534 (S-002):** queue_num 65535 was accepted
  but could never be installed (the IPv6 queue rule at queue_num+1 would
  be `queue to 65536`, outside the u16 range); config now rejects it with
  a clear message.

## Corrections (post-v0.2.6 docs, 2026-11-19; re-verified 2026-11-20)

- **HTTP listener defaults (docs correction):** the v0.2.6 notes, the
  v0.2.5 security advisory and the OpenWrt docs said the HTTP listeners
  are "disabled by default". In fact the daemon's built-in default is
  API `127.0.0.1:8080` and metrics `127.0.0.1:9090` (loopback) once the
  service is enabled; the shipped OpenWrt config stores
  `api_port '0'`/`metrics_port '0'`, but v0.2.6 ignores a file/flag
  value of `0` (falls back to the defaults; `internal/config/config.go`
  and `internal/cli/cli.go`). The only way to disable the listeners
  today is `GOSHISEN_API_PORT=0` / `GOSHISEN_METRICS_PORT=0`, or
  leaving the service disabled (`enabled=0`). No code behavior changed;
  severity remains Low (default) / Medium (non-loopback `api_host`).
  A code fix to honor explicit `0` is present on `main` after the
  v0.2.6 tag and is planned to ship in **v0.2.7**; no published
  artifact contains it yet.
- **Listener defaults measured (2026-11-20), CLI archive included:**
  the same correction applies to the v0.2.5/v0.2.6 **CLI archives**,
  which had been described only as "loopback-only". Measured with no
  config/env/flags they listen on `127.0.0.1:8080` + `127.0.0.1:9090`;
  YAML `api_port: 0`, an empty YAML value and `--api-port 0` are
  ignored there, while `GOSHISEN_API_PORT=0` disables. OpenWrt: a fresh
  install of the signed 0.2.5-r1 / 0.2.6-r1 apk with the packaged
  config, and an in-place upgrade 0.2.3-r1 → 0.2.6-r1 while running,
  both end with the two loopback listeners up; a config without the
  options (or with them empty) aborts in the init script
  (`api_port '' out of range`) with no listener. Precedence confirmed
  with `goshisen config show`: defaults < file < env < flags. Evidence
  table: `docs/security/advisory-v0.2.5-go-toolchain.md` § "Measured
  listener defaults"; contract tests:
  `tests/package/listener_defaults_contract_test.go`.
  The code fix that honors an explicit `0` is present on `main` after
  the v0.2.6 tag but is **not in any published artifact yet**
  (v0.2.7 candidate).

## Unreleased (after v0.2.6)

- **Explicit `0` disables the HTTP listeners (fixes the v0.2.6
  docs-gap):** `api_port: 0` / `metrics_port: 0` in the config file and
  `--api-port 0` / `--metrics-port 0` on the command line now disable
  the API / metrics listener instead of being treated as "not set" and
  falling back to 8080/9090. The file layer tracks key presence
  (`File.UnmarshalYAML`); the flag layer uses a presence-recording
  `flag.Value` (`portFlag`, which also rejects out-of-range values at
  parse time). An unset port still resolves to the built-in loopback
  default. The shipped OpenWrt config (`option api_port '0'`) now
  disables both listeners as intended; `GOSHISEN_API_PORT=0` /
  `GOSHISEN_METRICS_PORT=0` behave as before. Tests:
  `TestLoadFileZeroPortsDisable`, `TestLoadFilePortPresence`,
  `TestLoadFlagZeroPortDisables`, `TestLoadFilePortRejectsNonScalar`,
  `TestParsePortZeroFlags`, `TestParsePortInvalidValues`. No security
  severity change (loopback-only defaults).
- **Lab fixture enables the metrics listener explicitly:** the QEMU lab
  image (`scripts/openwrt-image-prep.sh`) is the only consumer that needs
  a listener — it reads verdict counters from `127.0.0.1:9090/metrics` —
  so it now writes `metrics_port: $LAB_METRICS_PORT` (9090) instead of
  relying on `0` falling back to the default; `api_port` stays `0`
  (policy is applied via SIGHUP). Both e2e scripts read
  `${LAB_METRICS_PORT:-9090}` and fail fast with
  `ENV: metrics listener unreachable` when a snapshot cannot fetch
  metrics, instead of the misleading "no daemon DROP verdicts recorded".
  Guard: `TestProvisionLabMetricsPort`.
