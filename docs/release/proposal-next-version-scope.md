# Proposal — v0.2.6 scope (after v0.2.5)

**Status: COMPLETED (v0.2.6 published 2026-11-19, S-005 fixed and
post-publish verified).** Historic planning document; see
`docs/release/v0.2.6-notes.md` and the S-005 advisory for the outcome.
Base: main at `9a303e1` (post-PR #76; the v0.2.5 tag is `79927ca`).

## v0.2.5 status (previous scope, completed 2026-11-17)

v0.2.5 shipped the upgrade-hook restart fix (issue #57), release-apk
supplementary evidence, and signed-tag planning. It also introduced
**S-005 (issue #78)**: the published apk payload embeds **go1.27.1**
and the CLI archives **go1.25.14** — both below the go1.27.4 that
fixes the 11 reachable stdlib vulnerabilities govulncheck reports at
the v0.2.5 source commit. The mitigation claimed at the time ("HTTP
listeners bind 127.0.0.1 only, no outbound TLS") was **not proven**;
this investigation (`docs/security/advisory-v0.2.5-go-toolchain.md`,
S-005 readiness PR) now classifies every finding and closes the gap
with gates. **v0.2.6 is the fix version.**

## S-005 classification (evidence in the advisory draft)

- 11 stdlib GO IDs (GO-2026-6859, 6809, 6617, 6613, 6612, 6611, 6610,
  6608, 6607, 6605, 6603). Source-mode govulncheck at the v0.2.5 source
  (go1.27.1) gives traces for 6 (6859, 6613, 6617, 6612, 6611, 6603)
  through `http.Server.Serve`; the other 5 (6809 x509, 6610 h2 client,
  6608 textproto, 6607 tls, 6605 http client) have no call trace — the
  daemon has no TLS, no h2, no `http.Client`, no multipart (listener
  inventory in the advisory).
- Default OpenWrt config: both listeners **bind loopback** and are
  **enabled whenever the daemon runs** — measured 2026-11-20 on OpenWrt
  25.12.6 (kernel 6.12.112): a fresh install of the signed 0.2.5-r1 /
  0.2.6-r1 apk and an in-place 0.2.3-r1 → 0.2.6-r1 upgrade both show
  `127.0.0.1:8080` + `127.0.0.1:9090`. The packaged `api_port 0` /
  `metrics_port 0` are rendered into the YAML but **ignored** by the
  daemon in ≤ v0.2.6 (the file layer treated `0` as "not set"); only
  `GOSHISEN_API_PORT=0` / `GOSHISEN_METRICS_PORT=0` or `enabled=0`
  disable. The metrics host is hardcoded 127.0.0.1; `api_host` is
  configurable in the daemon YAML — a non-loopback `api_host` exposes
  the API to the network (only whitespace validation), so remote
  reachability exists only under that non-default config. Evidence and
  per-scenario table: `docs/security/advisory-v0.2.5-go-toolchain.md`
  § "Measured listener defaults" (this paragraph originally said the
  listeners were "disabled"; that was wrong).
- Verdict: **not remotely exploitable in the default configuration**;
  locally reachable DoS (HTTP/1 server races/desync) when a listener
  is enabled; TLS/x509/h2/client IDs are not reachable by construction
  (no TLS, no h2, no `http.Client`). The v0.2.5 "loopback only"
  mitigation is TRUE for the default/shipped config but is **not
  closed by construction** (`api_host` configurable) — the advisory
  treats non-loopback `api_host` as a misconfiguration.

## Go-pinning approach (chosen)

Task 2 evidence (SDK container, pinned image
`openwrt/sdk@sha256:a17d6436...`):

- The image bakes **no feeds** — only `feeds.conf.default` pinning
  each feed to a commit (`packages` @ `093d551675...`). `scripts/feeds
  update -a` honors those pins (verified in-container), so the same
  image digest yields the **same** feed commits and the **same** Go
  version on any date. Digest pinning alone therefore DOES fix the Go
  version **for the feed state** — but it fixes it at go1.26.9
  (`GO_DEFAULT_VERSION:=1.26` on the 25.12 feed branch) with
  golang1.27=go1.27.1, i.e. **not** at 1.27.4.
- The v0.2.5 apk got go1.27.1 from the **host toolchain mounted into
  the SDK container** (build recipe exported `/go-host/bin`), not from
  the feed default; the recipe is the real compiler selector.
- `go.mod` now declares `go 1.26.0` + `toolchain go1.27.4`. Proven
  (host + in-container): with `GOTOOLCHAIN=auto` an older SDK Go
  (1.26.9/1.27.1) auto-downloads and builds with **go1.27.4**; with
  `GOTOOLCHAIN=local` the toolchain line is **silently ignored** (the
  v0.2.5 CLI drift root cause). The line stays (fixes the auto path)
  but is not the enforcement — the payload gates are.

**Decision (recommended, Task 2c option 3):** the v0.2.6 apk is
built in the pinned SDK container with a **sha256-verified go1.27.4
toolchain mounted at /go-host** (`GOTOOLCHAIN=local`, PATH updated),
exactly as the v0.2.5 recipe did but with the pinned patch version
(`scripts/sdk-build-v026.sh`). This is a legitimate OpenWrt-style
build — the package is still built by the OpenWrt buildroot via
`PKG_BUILD_DEPENDS:=golang/host`; only the Go compiler is pinned, as
CI pins setup-go. Feed repinning (option 2) was **verified to break**
on this SDK snapshot: pinning `packages` to a commit carrying
golang1.27=1.27.4 fails the SDK's metadata scan
(`ERROR: please fix feeds/.../Makefile` for several packages, e.g.
admin/bottom, lang/python/python-cryptography), so it is **not** a
viable fix today. A newer SDK image digest (option 1) would carry a
newer feed but is a larger trust/reproducibility change and still
needs verification. The `toolchain go1.27.4` line stays (backstop for
the auto path) and the two S-005 gates enforce the outcome on the
payload.

## Verified results (S-005 readiness PR, 2026-11-18)

- **Dual clean-SDK test builds** (option 3 recipe, `0.2.6-r0` test
  label): payloads byte-identical (sha256
  `c3bb7c52c8e5bb58eb349b0380ad6a6c5be1cd97147a5759064efc8813730827`),
  apks byte-identical (sha256
  `767b8901fad3f2c1df9106ab5c3db1c375c20de832bbf21e9b2d56847a130962`);
  embedded Go **go1.27.4**; `govulncheck -mode=binary` on the payload
  -> **No vulnerabilities found**. `STRIP:=true` in the package
  Makefile is required (the SDK's default `sstrip` breaks `go version
  -m` and binary-mode scanning even with `-buildid=` removed).
- **VM smoke** (fresh OpenWrt 25.12.6 x86/64 QEMU, kernel 6.12.112):
  queue-100 DROP/ACCEPT controls with counter evidence; stateful IPv6
  queue-101 with paired IPv4 controls (4 scenarios); apk upgrade
  signed 0.2.5-r1 -> test 0.2.6-r0 with continuous deny probes blocked,
  new binary running without restart (`/proc/<pid>/exe` sha256 == new
  payload), SIGKILL respawn by procd, `apk del` cleanup (hook removed,
  binary gone, no processes).
- **Gates (Task 4):** `scripts/check-apk-payload-go.sh` and
  `scripts/check-apk-payload-govuln.sh` (empty allowlist) run in
  `apk-sign.yml` production mode before signing and in CI
  (`apk-payload-go-gate` + `apk-payload-govuln-gate` jobs); the pin
  gate `scripts/release-pin-gate.sh` gained opt-in S-005 checks
  (`GATE_S005_CHECKS=1`). Tests cover: good payload passes, old-Go
  payload fails both gates with the right reason, allowlisted finding
  passes and is reported, non-Go payload fails closed.
- **release.yml:** `go-version: "1.27.4"` + `GOTOOLCHAIN: local`
  (new); verified locally the CLI archives built this way embed
  go1.27.4 and govulncheck binary-mode reports no findings.

## v0.2.6 release run — maintainer steps (in order)

1. **Merge** the S-005 readiness PRs (this PR: advisory draft + scope
   update + govulncheck gate; PR #80: payload Go-version gate; recipe
   draft PR).
2. **Makefile-only pin PR** (v0.2.4 pattern, PR #56): bump
   `openwrt/Makefile` `PKG_VERSION` to 0.2.6, `PKG_RELEASE` 1,
   `PKG_SOURCE_VERSION` to the v0.2.6 tag commit, new `PKG_MIRROR_HASH`
   (derived from the official SDK container), and **remove `-buildid=`
   from the Go ldflags** and **set `STRIP:=true`** so the payload keeps
   readable buildinfo (required by the govulncheck gate; the SDK's
   `sstrip` otherwise breaks `go version -m` and binary-mode
   govulncheck). The apk is then built with the
   sha256-verified go1.27.4 mounted into the pinned SDK container
   (`scripts/sdk-build-v026.sh`, option 3).
3. **Approve + create** the signed tag `v0.2.6` at the source head
   (signing method per `docs/release/signed-tags-plan.md`).
4. **Run the pin gate** `scripts/release-pin-gate.sh` + tag-signature
   check; then **both S-005 gates** on the unsigned staged apk:
   `scripts/check-apk-payload-go.sh` (min Go 1.27.4 from
   `packaging/go-toolchain.env`) and
   `scripts/check-apk-payload-govuln.sh` (empty allowlist).
5. **Signing environment check:** `GH_RELEASE_APK_SIGNING_KEY`
   provisioned + approved anchor/fingerprint; production APK signing is
   BLOCKED without it.
6. **CI green** on the tag: build, package, verify-signatures,
   integration (no SKIPs), openwrt e2e (slirp + v6), arm64
   cross-build, plus the two apk-payload gate jobs.
7. **DRAFT staging:** tag push stages a DRAFT release with signed CLI
   assets (release.yml never auto-publishes); build/attach the apk
   from the pinned-feed build into the draft; verify pre-signing
   sha256; **run the production apk-sign workflow** (maintainer
   dispatch; it now runs the S-005 min-Go and govulncheck gates before
   signing).
8. **Publish** (maintainer only; immutable releases gate active).
9. **Post-publish evidence:** fresh-VM apk smoke (queues 100/101,
   DROP/ACCEPT controls, stateful IPv6 + paired IPv4 control, SIGKILL
   respawn), upgrade run v0.2.5 → v0.2.6 with continuous deny probes
   and `/proc/<pid>/exe` sha256 == new payload, apk del cleanup,
   CLI archive smoke; record kernel version.

## v0.2.6 release notes must say (honest S-005 text)

- v0.2.5 (and earlier v0.2.x) apk payloads were built with
  go1.27.1/go1.25.14-era toolchains carrying 11 reachable stdlib
  vulnerabilities (list); full per-ID classification and the
  "no default-config remote exploit" analysis in
  `docs/security/advisory-v0.2.5-go-toolchain.md`.
- v0.2.6 ships the apk payload **and** both CLI archives built with
  go1.27.4 (payload enforced by the sign-time gates; CLI archives by
  the release.yml `go-version: 1.27.4` pin), closing all 11 IDs.
- Workarounds for v0.2.5 and earlier: `api_port`/`metrics_port` `0`
  does not disable on those releases — set `GOSHISEN_API_PORT=0` /
  `GOSHISEN_METRICS_PORT=0` in the service environment or keep
  `goshisen.main.enabled=0`; never set a non-loopback `api_host`.

## Remaining out of scope for v0.2.6

- **LuCI (issue #8):** separate feature; depends on offload health
  flag surfacing (issue #6). Not in v0.2.6.
- **Hardware arm64 (issue #7):** runtime verification beyond
  x86_64/aarch64 QEMU remains pending; hardware optional. Not in
  v0.2.6.
- Advisory publication (GitHub security advisory) is maintainer-only;
  only the draft file exists.

## Boundaries

Publishing, issue closing, repository/environment/settings changes,
tags, signing, and the apk-sign dispatch require **explicit per-run
maintainer authorization**. The agent never creates/moves/deletes
tags, never signs/publishes releases, never touches secrets/signing
keys/the APK anchor, and never merges PRs.

## v0.2.7 scope (proposal, 2026-11-20)

Scheduled for the release after the published v0.2.6:

1. **Port-zero semantics reach a published artifact** — the fix on `main`
   since PR #89 (`137dd24`): file/flag `0` disables the listeners like
   the environment variable already did. User-visible: a router whose
   packaged UCI kept `api_port '0'` / `metrics_port '0'` stops exposing
   both loopback listeners after upgrading (enable them explicitly with
   `uci set goshisen.main.api_port='8080'` / `metrics_port='9090'`).
2. **Init script always renders explicit port values** — absent/empty
   UCI options become `0` (disabled) with a logread warning instead of
   the old silent `api_port '' out of range` abort; invalid values abort
   naming the option (fail-closed via the boot guard).
3. **`api_host` loopback guard** — the API refuses to start on a
   non-loopback host unless the operator opts in
   (`allow_non_loopback_api_host` / `GOSHISEN_ALLOW_NON_LOOPBACK_API_HOST`
   / `--allow-non-loopback-api-host`), so a typo can no longer expose the
   unauthenticated GET endpoints to the network.
4. **Metrics method hardening** — the standalone metrics listener
   answers `405` to `CONNECT` and other non-`GET`/`HEAD` methods.
5. **Docs** — `docs/release/v0.2.7-notes-draft.md` plus the listener
   defaults correction already merged for v0.2.5/v0.2.6 users.

Out of scope for v0.2.7: requiring the token for the read endpoints
(tracked by its own issue with the compatibility analysis), LuCI, and
non-x86_64 hardware runtime verification.
