# Changelog

Status and notable history for GoShiSen-Enforce. Per-release detail lives in `docs/release/` (e.g. `v0.2.6-notes.md`, `proposal-next-version-scope.md`).

## Release status (snapshot, 2026-11-27)

- **v0.2.9** — latest release (published 2026-11-27): release-engineering / CI-hardening release with **no runtime change** (the CLI behaves exactly as v0.2.8): F-02 approval gate on the `sign` job, hardened OpenWrt e2e harness, bounded CI network steps, fail-closed S-005 govulncheck gates, the codified apk build recipe + build manifest gate, and the post-tag pin + step-9 recipe anchor. Notes: [`docs/release/v0.2.9-notes.md`](release/v0.2.9-notes.md); assets: [Releases](https://github.com/88kbleirad/GoShiSen-Enforce/releases/tag/v0.2.9).
- **v0.2.8** — previous release (published 2026-11-22): security/hardening release — bounded file reads everywhere operator input is read, private policy store (0700/0600) with bounded metrics listener, OpenWrt `api_token` written 0600, authenticated-read opt-ins (D1), govulncheck re-balanced (D2). Notes: [`docs/release/v0.2.8-notes.md`](release/v0.2.8-notes.md); assets: [Releases](https://github.com/88kbleirad/GoShiSen-Enforce/releases/tag/v0.2.8).

## Unreleased — release-run wrapper (P3.11, issue #142)

`scripts/release-run.sh` codifies one release run as a 12-step plan — pre-tag
gate → annotated tag → release workflow (draft) → post-tag pin PR → apk build
A/B → manifest + pin/version gates → unsigned VM smoke → draft upload →
production apk signing → signed smoke/attach → publish flip → post-publish
sweep — printing each step's exact commands, its owner (agent or maintainer) and
the evidence it must produce.

- **`--dry-run`** prints the plan and executes nothing — no tag, build, signing,
  upload, publish or file written.
- **`--execute`** runs the steps owned by `agent` (pre-tag gate, apk build A/B,
  manifest/pin gates, post-publish sweep), then stops at the first `maintainer`
  step (tag, workflow, pin PR, draft upload, apk signing, publish flip) or
  `manual` step (the lab smoke) with the exact commands to run — exit 5/6;
  resume with `--from-step N`. It refuses a step whose inputs are unset (7),
  fails closed on a failing step (1) and on a dirty/off-`main` tree (8), and
  writes one evidence file per run (step → command → exit → output sha256, plus
  the raw step log) under `$HOME/release-evidence/<tag>-<UTC>/` — outside the
  worktree, so step 1's clean-tree gate still passes.
- The tag name goes through `scripts/validate-release-tag.sh` (S-003) before it
  is used in any printed command; three hard stops are never crossed
  automatically (F-01 pre-tag gate, A/B hash mismatch, manifest/pin gate).
- The plan is codified from the v0.2.9 run, not invented; the two steps the last
  run could not drive from committed code (the VM smoke) are marked `manual` and
  say so.
- Guarded by `tests/ci/release-run-test.sh` (`release-run (P3.11)` in CI), which
  drives `--execute` against scratch git fixtures. Doc:
  [`docs/release/release-run.md`](release/release-run.md).

## Release signing anchor rotation (2026-11-28, post-v0.2.9)

The CLI release trust anchor rotated: `keys/goshisen-release.pub.pem` is now the
Ed25519 key with fingerprint `e9b4bf1194ce90d2377720102748460404eacbc7aba9aa8ad48532cd63294fb9`
(key ID `bdda690b97a4f49e`). The pre-rotation anchor
(`24215e016621925215dec0ad6ea14ac74253a4e97249a973e89d649a2217cca0`, created
2026-11-13) signed **v0.2.1 – v0.2.9** and stays committed as
`keys/goshisen-release-2026-11-13.pub.pem`.

- **Verifying an older release:** use the `public_key.pem` asset that ships with
  that release (or the archived anchor above). `main` now carries the rotated
  anchor and will not verify v0.2.x signatures — the install docs were updated
  accordingly.
- No runtime/CLI behaviour change, and no published artifact is re-signed or
  withdrawn.
- Record: [`docs/release/release-key-rotation-2026-11-28.md`](release/release-key-rotation-2026-11-28.md).

## v0.2.9 (published 2026-11-27)

Release-engineering and CI-hardening release. **No daemon code change since
v0.2.8** (`git diff 'v0.2.8^{}'..'v0.2.9^{}' -- '*.go' internal cmd go.mod
go.sum` empty): the CLI is unchanged and the apk payload differs only in the
embedded version stamp. Detail: [`docs/release/v0.2.9-notes.md`](release/v0.2.9-notes.md).

- **F-02 (#132):** `release.yml`'s `sign` job declares `environment: signing`
  (required reviewer, `main`/`v*` policies), so a tag release pauses for
  approval before `GH_RELEASE_SIGNING_KEY` is read; new guard rule
  `environment-scoped` + `bad-sign-no-environment.yml` negative fixture fail CI
  if a `sign` job loses the gate; the stray `release-cli` environment was
  deleted. The CLI key itself is **still repository-scoped** — the gate protects
  the job, not the key (`gh secret list --env signing` holds only
  `GH_RELEASE_APK_SIGNING_KEY`); remaining steps in
  `docs/release/f02-environment-migration.md` §0/§3.
- **OpenWrt e2e harness (#134):** the lab starts the daemon explicitly when
  `rc.local` loses the boot race (proc-first readiness, `setsid` fallback, 120 s
  re-poll) and collects diagnostics *before* `lab down`.
- **Bounded CI network steps (#135):** `CURL_BOUNDED`/`APT_BOUNDED` + job
  timeouts; measured `sign + verify` 37m16s → 46–58 s, `integration` 50+ min →
  ~3 min.
- **S-005 govulncheck gates fail closed (#137):** `-format json` exits 0 with
  findings, so the old gates could pass a broken or vulnerable scan (empty
  stream treated as clean, `${arr[@]:-}` matching an empty allowlist entry).
  The gates now share `scripts/lib/govuln-verdict.sh` and fail on a bad rc, an
  unparsable/empty stream, a finding without an `osv` id, or an unallowlisted
  finding; a new CI job runs the CLI-archive fixtures. Published v0.2.6–v0.2.8
  artifacts re-audited with raw `govulncheck -mode=binary` (9 artifacts, 0
  finding records) — **nothing was masked**.
- **Codified apk build + manifest gate (#146), post-tag pin (#148), step-9
  recipe anchor (#149):** `scripts/build-apk-release.sh` builds the unsigned apk
  from committed inputs only (digest-pinned SDK read from `apk-sign.yml`,
  sha256-pinned go1.27.4, zero-hash two-pass mirror hash) and writes
  `build-manifest-<label>.json`; `scripts/check-apk-build-manifest.sh`
  re-derives it, `release-pin-gate.sh` step 9 anchors the recipe to the tag (the
  previous wiring rejected the documented pin-commit flow; fixture now covers
  `F != T` plus two post-tag drift negatives).
- **Artifacts (20):** apk `goshisen-0.2.9-r1.apk`
  `1604a6802de6784b769075a98f437b2a6ea8375e1266f79221acd48d3102dfd7` signed with
  the EC P-256 anchor (fingerprint `542bb7ae…`; payload `ba8183b1…`, go1.27.4);
  CLI `b6c5b8cd…` (amd64) / `50c0892e…` (arm64) / sbom `c25f868a…`. The apk is
  intentionally not in `checksums.txt` (separate key domain) —
  `scripts/verify-release-assets.sh` verifies the whole set and fails closed on
  the unsigned apk.
- **VM verification:** signed install with no `--allow-untrusted`, queues
  100/101, paired DROP/ACCEPT controls and `apk del` cleanup PASS; the
  v0.2.8 → v0.2.9 upgrade under continuous probes shows a **~0.4 s fail-closed
  gap** during the restart (hook stays installed while the queue rebinds — new
  connections dropped, nothing accepted that the policy would deny).
- **Known limitations:** release tags are annotated but unsigned; `main` and the
  tags have no protection (rulesets empty); PR #150 (NAT test readiness) is open
  and touches tests only.

## v0.2.8 (published 2026-11-22)

- **Policy-store permissions tightened + metrics listener bounded
  (#102):** the policy store creates its directory `0700` and writes
  policy/`active.json` files `0600` (was `0755`/`0644`), so another
  local account can no longer read applied policies; the metrics HTTP
  listener now sets `ReadTimeout`/`WriteTimeout` 10 s and
  `IdleTimeout` 30 s, matching the API listener, so a stalled local
  client cannot hold a connection (and its goroutine) open. Regression
  test: `TestOpenRestrictedPerms`.
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
- **Queue-stats read bounded too (#100-adjacent, v0.2.8 P1.1 extension):**
  `packet.ReadQueueStats` (the `/proc/net/netfilter/nfnetlink_queue`
  reader used by the overflow watcher) was the last unbounded
  `os.ReadFile` in the tree; it now goes through
  `internal/safefile.ReadBounded`, so a non-regular file (FIFO, device,
  directory) or oversized input fails closed instead of hanging the
  daemon. Regression tests cover regular file, missing file, FIFO
  (must not block), directory and over-cap sizes.
- **Startup policy-path check is bounded too (F-4a/F-4b):** the daemon's
  pre-start validation opened `policy_file` with a plain `os.Open`, so a
  path to a FIFO blocked startup forever and a directory/device was
  accepted and only failed later. The check now goes through the bounded
  reader (non-blocking open, regular-file check, 16 MiB cap), so an
  unusable path fails fast with a clear `policy_file:` error. Tests cover
  FIFO (must not block), directory, device, over-cap and a regular file.

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

## Unreleased (after v0.2.8)

- **S-005 govulncheck gates failed OPEN on a broken scan (security fix):**
  both signing-path gates — `scripts/check-apk-payload-govuln.sh` and
  `scripts/check-cli-archive-govuln.sh` — carried a copy of the same
  verdict code, and that copy could print
  `S-005-…-GOVULN-GATE-PASS` for a scan that found vulnerabilities or
  never completed. Measured against the pre-fix scripts with a stubbed
  `govulncheck`: exit 3 with an empty stream, exit 3 with a finding
  entry that carried no OSV id, and exit 137 (SIGKILL) all PASSed, each
  reported as "findings are allowlisted", because the allowlist
  comparison ran `"${findings[@]:-}"` against `"${allowed[@]:-}"` — over
  an EMPTY array that expands to one empty word, so `[ "" = "" ]`
  matched. Both gates now source one implementation,
  `scripts/lib/govuln-verdict.sh`, which keys the verdict off the parsed
  stream rather than off rc: an unparsable or empty stream, a finding
  entry with no OSV id, a finding that is not on
  `packaging/govulncheck-allowlist.txt`, a missing or non-executable
  `govulncheck`, and any exit code other than 0/3 all fail the gate;
  findings are printed whether it passes or fails. The shared code also
  drops the assumption that `-mode=binary -format json` exits 3 when it
  finds vulnerabilities — govulncheck documents exit 0 in that case
  (`cmd/govulncheck/doc.go`, v1.9.0) and exits 0, so findings arrive on
  rc 0. Regression coverage: the stub outcome matrix in
  `tests/ci/check-apk-payload-govuln-test.sh` (rc 0/3 × clean /
  unlisted / allowlisted / no-OSV-id / empty / unparsable, plus missing
  and non-executable tool) together with the real-tool fixtures in both
  gate tests; each fixture 1 now also asserts the clean control's stream
  shape — at least one `osv` record and zero `finding` records, via
  `tests/ci/lib/govuln-clean-stream.sh` — so the control passes for the
  right reason, and the CLI gate's fixtures run in CI as the
  `cli-archive-govuln-gate (S-005)` job (they were unrun in CI before).
  No released artifact changes — gates and CI only.
- **The apk build recipe is committed and the artifact is tied to it by a
  manifest (#146):** the v0.2.6–v0.2.8 apks were built from a *local,
  uncommitted* recipe copy (the SDK's `golang/host` dependency disabled
  plus a mounted official go1.27.4), so nobody else could reproduce
  them. `packaging/apk-build/no-golang-host.patch` is that single recipe
  change as a committed patch, `packaging/apk-build/sdk-x86_64.config`
  the SDK config fragment used for the published v0.2.8 apk, and
  `packaging/go-toolchain.env` pins `GO_TOOLCHAIN_*` (go1.27.4 tarball
  and `bin/go` sha256). `scripts/build-apk-release.sh` builds the
  unsigned package from committed inputs only: it `git archive`s
  `--commit`, applies the overlay to a scratch copy (never the working
  tree's `openwrt/Makefile`), derives or verifies `PKG_MIRROR_HASH`,
  uses the SDK digest pinned in `apk-sign.yml`, verifies the Go
  tarball/`bin/go` hashes, asserts the package is unsigned inside the
  container (`apk verify` → `UNTRUSTED signature`), and writes
  `build-manifest-<label>.json`. `scripts/check-apk-build-manifest.sh`
  re-derives every claim in that manifest from the commit — recipe,
  overlay and SDK-config hashes, replay of the recipe to the recorded
  Makefile hash, pin ancestry, Go and SDK pins, artifact hashes,
  unsigned state, and (with `--compare`) A/B agreement.
  `scripts/release-pin-gate.sh` gained opt-in step 9 (`--manifest` /
  `GATE_MANIFEST`) so a release can require it, and CI runs
  `tests/ci/apk-build-manifest-test.sh` (generated fixtures; every
  negative case rejected for a named reason) as
  `apk-build-manifest-gate (F-01)`. Verified before merge: two
  independent builds at one commit produced identical apk
  (`fa3b9530...`, 3,286,016 B) and payload (`ba8183b1...`) hashes, and
  the same script rebuilding the v0.2.8 inputs reproduced the published
  pre-signing apk `b97f0da4...` byte-for-byte. This is not a pristine-SDK
  build (the Go toolchain comes from outside the SDK tree) and the mirror
  hash is toolchain-specific — `docs/release/v0.2.9-apk-build-recipe.md`
  records the runs and those limits. No runtime code changes; packaging,
  CI and docs only.
