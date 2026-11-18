# Security Advisory (draft) — v0.2.5 OpenWrt apk and CLI archives built with vulnerable Go toolchains (S-005)

> **STATUS: DRAFT — not published.** This file is the advisory text under
> review for the v0.2.6 release. It is deliberately a plain file in the
> repository; no GitHub security advisory has been or will be published
> without the maintainer's explicit approval.

- **Finding:** S-005 (issue #78)
- **Affected products:** GoShiSen-Enforce v0.2.5 OpenWrt apk
  (`goshisen-0.2.5-r1.apk`) and v0.2.5 CLI/daemon archives
  (`goshisen-linux-amd64.tar.gz`, `goshisen-linux-arm64.tar.gz`).
  Earlier releases are affected to the same degree — see the per-release
  scan table below (v0.2.0, v0.2.3, v0.2.4, v0.2.5 ship apks; v0.2.1
  and v0.2.2 ship CLI archives only). The fix lands in **v0.2.6**.
- **Date of analysis:** 2026-11-18.
- **Severity:** Low for the default configuration (both HTTP listeners
  disabled in the shipped OpenWrt config; when enabled they bind
  127.0.0.1 and the nearest exposure is a local user on the loopback
  listener, with the API mutations requiring a bearer token). Elevated
  to Medium if the API listener is misconfigured to a non-loopback
  `api_host` (see Workarounds). No outbound connections and no TLS
  exist, so the TLS/x509/client-side vulnerability classes are not
  reachable in any configuration.

## Affected versions and toolchains (evidence)

| Artifact | sha256 (published) | embedded toolchain | x/net / x/sys |
| --- | --- | --- | --- |
| `goshisen-0.2.5-r1.apk` payload `usr/sbin/goshisen` | `c117a9aafe4b50313640006b67e53c45e5ab2e1ae8569e804d4b633f1a42c051` | **go1.27.1** | v0.53.0 / v0.43.0 |
| `goshisen-linux-amd64.tar.gz` → `goshisen` | tarball `f08b965ea28fc474b63a19e6435290d34fe90458f9eabc12e5e4aa76fd40dd80` | **go1.25.14** | v0.53.0 / v0.43.0 |
| `goshisen-linux-arm64.tar.gz` → `goshisen` | tarball `cf400ac852b8d5a7135c9f642d65058a775d58b2e5b48b2828dc6a97d3556a26` | go1.25.14 (same build) | v0.53.0 / v0.43.0 |

Toolchain identification: `go version -m` (CLI archives) and embedded
`Go buildinf:` block via `strings -n 8` (apk payload; the SDK build
strips the ELF section header string table, so `go version -m` fails
with "unrecognized file format").

### Per-release scan table (binary mode, every published asset)

All assets below were downloaded from the official GitHub release of each
tag and verified against that release's `checksums.txt` before scanning.
govulncheck v1.9.0, `-mode=binary`, vuln DB version 2026-11-11 02:51:11Z
(fetched 2026-11-18).

| Release | apk asset | apk payload Go | CLI amd64 Go | CLI amd64 findings | CLI arm64 Go | CLI arm64 findings |
| --- | --- | --- | --- | --- | --- | --- |
| v0.2.0 | `goshisen-0.2.0-r2.apk` | go1.27.1 (x/net v0.53.0) | go1.25.14 | 23 | go1.25.14 | 23 |
| v0.2.1 | **none published** | — | go1.25.14 | 23 | go1.25.14 | 23 |
| v0.2.2 | **none published** | — | go1.25.14 | 23 | go1.25.14 | 23 |
| v0.2.3 | `goshisen-0.2.3-r1.apk` | go1.27.1 (x/net v0.53.0) | go1.25.14 | 23 | go1.25.14 | 23 |
| v0.2.4 | `goshisen-0.2.4-r1.apk` | go1.27.1 (x/net v0.53.0) | go1.25.14 | 23 | go1.25.14 | 23 |
| v0.2.5 | `goshisen-0.2.5-r1.apk` | go1.27.1 (x/net v0.53.0) | go1.25.14 | 23 | go1.25.14 | 23 |

All CLI archives (all six releases) embed go1.25.14 and the identical 23
binary-mode findings:

- stdlib (14 IDs): GO-2026-6859, 6809, 6617, 6613, 6612, 6611, 6610,
  6609, 6608, 6607, 6605, 6603, 6600, 6599.
- module `golang.org/x/net` v0.53.0 (9 IDs): GO-2026-6866, 6860, 5942,
  5030, 5029, 5028, 5027, 5026, 5025.

apk payloads (v0.2.0/3/4/5) all embed go1.27.1 with x/net v0.53.0. Their
binary cannot be scanned in `-mode=binary` (the SDK's sstrip removes the
ELF section string table: `govulncheck: unrecognized binary format`;
`go version -m` fails identically). Source-mode scan of the v0.2.5 source
under its embedded toolchain (go1.27.1) reports the 11 stdlib IDs below;
because all four apk payloads embed the same toolchain, the same set
applies to v0.2.0/3/4/5.

## What the daemon listens on / connects out to (evidence)

Two HTTP servers, both plaintext, both bound to loopback by default:

1. REST API (`internal/api/server.go`): `api.New` sets
   `s.srv = &http.Server{Addr: net.JoinHostPort(o.Host, ...)}`
   (server.go:122); `Serve` does `net.Listen("tcp", s.srv.Addr)`
   (server.go:136) then `s.srv.Serve(ln)` (server.go:143), with
   `ReadHeaderTimeout 5s / ReadTimeout 10s / WriteTimeout 10s /
   IdleTimeout 30s / MaxHeaderBytes 8<<10` (server.go:122-130).
   Handlers: `GET /v1/policy`, `GET /v1/policy/versions`,
   `POST /v1/policy`, `POST /v1/policy/validate`,
   `POST /v1/policy/versions/{version}/activate`, `GET /healthz`,
   `GET /readyz`, `GET /metrics` (server.go:113-120). Mutations
   require a bearer token (`authorized`/`requireAuth`, server.go:199-
   220); with no token configured they are disabled entirely.
   Request bodies are bounded: `r.Body = http.MaxBytesReader(w,
   r.Body, MaxBodyBytes)` with `MaxBodyBytes = 16 << 10` (server.go:65,
   267). The GET endpoints (`/healthz`, `/readyz`, `/metrics`) are
   unauthenticated.
2. Metrics (`internal/app/app.go:518-542`): `serveMetrics` builds
   `http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", port)}`
   and serves only `/metrics`; the bind is hardcoded
   `127.0.0.1` and cannot be configured.

Bind configurability (`internal/config/config.go`):

- `DefaultAPIHost = "127.0.0.1"` (config.go:35), settable via YAML
  `api_host`, env `GOSHISEN_API_HOST` (config.go:330), or
  `--api-host` (internal/cli/cli.go:147). Validation only rejects
  whitespace (config.go:469-470) — **a non-loopback bind is
  configurable and passes validation**; the API then listens on the
  network.
- `DefaultAPIPort = 8080`, `DefaultMetricsPort = 9090` (config.go:37-
  38); `0` disables each server (app.go:295, 317). The shipped OpenWrt
  config sets `option api_port '0'` and `option metrics_port '0'`
  (openwrt/files/etc/config/goshisen).
- No TLS listener, no crypto/tls import, no http.Client, no net.Dial
  in shipped code (`cmd/`, `internal/`): `grep -rn --include='*.go'
  -E 'crypto/tls|http2|h2c|multipart|x509|http\.Client|http\.Get'
  cmd internal` (excluding `_test.go`) returns nothing. The only
  sockets are two `netlink.Dial(unix.NETLINK_NETFILTER, nil)` netlink
  sockets (internal/packet/capture.go:259, 263) — kernel AF_NETLINK,
  not network peers.

## Vulnerability summary

The S-005 finding set is the **11 stdlib IDs** reported by source-mode
govulncheck at the v0.2.5 source under the payload's own toolchain:

GO-2026-6859, 6809, 6617, 6613, 6612, 6611, 6610, 6608, 6607, 6605,
6603.

- **Source mode** (`-mode=source -show traces`, go1.27.1, shipped
  packages `./cmd/goshisen ./internal/...`) reports **6 of the 11 as
  reachable by a call path**:

  ```
  Your code is affected by 6 vulnerabilities from the Go standard library.
  ```

  These 6 (GO-2026-6859, 6617, 6613, 6612, 6611, 6603) all trace
  through `http.Server.Serve` (the API server) or
  `http.Server.ListenAndServe` (the metrics server). The other 5
  (6809, 6610, 6608, 6607, 6605) are listed by the same source run
  **without a call trace**: they are only reachable from
  `tests/integration/probe` (an unsupported test tool) and have no
  code path in the shipped daemon (no TLS, no `http.Client`, no
  multipart parse).

- **Binary mode** (`-mode=binary` on the published CLI archives)
  reports **all 14 stdlib IDs as symbols present in the binary**:

  ```
  Vulnerability #1: GO-2026-6859  ... net/http@go1.25.14
  Vulnerability #2: GO-2026-6809  ... crypto/x509@go1.25.14
  ... (14 stdlib IDs total, incl. GO-2026-6609, 6600, 6599)
  ```

  Binary mode proves a symbol is *linked*; it does not prove it is
  *callable from the program's own code*. The three IDs that appear
  only in binary mode (6609, 6600, 6599) have no reachable call path:
  GO-2026-6609 (`http.ServeContent`/`ServeFile`/`FileServer` Range
  CPU) requires a file-serving handler, which the daemon does not
  have; GO-2026-6600/6599 (html/template) require template rendering,
  which the daemon never performs.

The two modes answer different questions and are not contradictory:
source mode says which IDs are reachable from our code under the
embedded toolchain (6 of the 11 with a trace; the other 5 present but
untraceable); binary mode says which vulnerable symbols the linked
binary contains (all 14 stdlib IDs, of which the 11 S-005 IDs plus
6609/6600/6599). The plain verdict below uses source-mode reachability
for the 11 S-005 IDs.

## Per-vulnerability classification (daemon binary, default config)

| GO ID | package/symbol | call path from our code | precondition (attacker must control) | classification |
| --- | --- | --- | --- | --- |
| GO-2026-6859 | net/http (MaxBytesReader race) | `api.Server.Serve` → `http.Server.Serve` (server.go:143); `app.Daemon.serveMetrics` → `http.Server.Serve` (app.go:541) | an authenticated client (valid bearer token) sends an HTTP/1 POST with a body that exceeds `MaxBodyBytes` (16 KiB), so the handler's `readBody` (server.go:267, `http.MaxBytesReader`) races its own response-header write; `requireAuth` runs before `readBody`, so unauthenticated requests never read bodies; the metrics handler never reads bodies | **local user with API token** (loopback listener) / **remote with non-default `api_host`** |
| GO-2026-6613 | net/http (CONNECT desync) | API server: `http.Server.Serve` (server.go:143); metrics server: `http.Server.Serve` (app.go:541) | a client sends an HTTP/1 CONNECT request; the server handler must reply 2xx to CONNECT. Verified: the API server answers 405/404 to CONNECT (method-specific mux — `TestConnectMethodNotAllowed`); the standalone metrics server's methodless `/metrics` handler answers **200** to a path-form CONNECT (the GO-2026-6613 precondition holds there — `TestMetricsServerConnectToMetricsPath`). The smuggling impact needs an intermediate proxy in front of the listener; the metrics listener is loopback-only and disabled by default, so no proxy sits in front | **not practically exploitable** (metrics listener loopback-only + disabled by default; no proxy in front; API never answers 2xx to CONNECT) |
| GO-2026-6617 | net/http/internal/http2 (HPACK race) | `http.Server.Serve` → h2 only after TLS negotiation | TLS-negotiated h2 — no `tls.Config`, plaintext listener | **not reachable (no TLS)** |
| GO-2026-6612 | net/http/internal/http2 (double flow refund) | `http.Server.Serve` h2 path | same | **not reachable (no TLS)** |
| GO-2026-6611 | net/http/internal/http2 (SETTINGS CPU) | `http.Server.Serve` h2 path | same | **not reachable (no TLS)** |
| GO-2026-6610 | net/http/internal/http2 (malformed framing, client) | client-side h2 | an outbound h2 client — no `http.Client`, no `net.Dial` | **not reachable (no client)** |
| GO-2026-6603 | net/http/internal/http2 (Trailer memory) | `http.Server.Serve` h2 path | TLS-negotiated h2 client | **not reachable (no TLS)** |
| GO-2026-6608 | mime/multipart (memory limit bypass) | no call trace in the shipped daemon; on go1.25.14 the HTTP/1 server's header-parse path (`textproto.ReadMIMEHeader`) is reached for every request on an enabled listener (traced under go1.25.14) | a multipart form body (apk path) or any request to an enabled HTTP/1 listener (CLI archives) | apk: **not reachable (no multipart)**; CLI archives: **local user on an enabled listener** (unauthenticated header-parse path) |
| GO-2026-6605 | net/http (CONNECT rejection, client) | client-side | an outbound HTTP client | **not reachable (no client)** |
| GO-2026-6607 | crypto/tls (ECH outer extension) | no call trace; no `crypto/tls` import | a TLS listener/client | **not reachable (no TLS)** |
| GO-2026-6809 | crypto/x509 (intermediates) | no call trace; no x509 use (no TLS) | TLS verification | **not reachable (no TLS)** |
| GO-2026-6609 | net/http (`ServeContent` Range CPU) | no call path — no `http.ServeContent`/`ServeFile`/`FileServer` anywhere in `cmd/`/`internal/` | a Range header sent to a file-serving handler | **not reachable (no file-serving handler)**; binary-mode symbol only |
| GO-2026-6600 / 6599 | html/template (Execute) | no call path — no `html/template` import | template rendering | **not reachable (no html/template)**; binary-mode symbols only |

Notes:

- The whole-repo source scan (including `tests/integration/probe`, an
  unsupported test tool; `http.Client` at probe/main.go:248) reports 11
  affected stdlib IDs; the shipped daemon links the 6 above with traces.
- The go1.25.14 CLI archives additionally expose **GO-2026-6608 on the
  server header-parse path** (in go1.25.14 the HTTP/1 server calls
  `textproto.ReadMIMEHeader` on every request; the go1.27.1 server does
  not) — so the CLI archives are affected by the local-user class for
  6608 too.
- Default configuration: both ports disabled (`0`) on OpenWrt
  (`openwrt/files/etc/config/goshisen`); when enabled they bind
  loopback only. A root user can enable them; a non-root local user can
  connect to them if root enabled them.

## Plain verdict on exploitability

**No remote exploit exists with the default configuration.** The
daemon's only network surface is two plaintext HTTP servers on
127.0.0.1 that are disabled by default on the OpenWrt package; it has
no TLS and no outbound connections, so the h2/TLS/x509/client-side
classes are unreachable by construction. The nearest-to-reachable
class is **local users on the loopback HTTP listeners**: 6859 needs a
valid API bearer token plus an oversized POST body (mutations are
token-gated; unauthenticated GET endpoints never read bodies), and
6613's precondition exists on the metrics server (methodless `/metrics`
answers 200 to CONNECT) but requires a proxy in front for impact, which
the loopback-only, disabled-by-default listener does not have. On the
CLI archives (go1.25.14) 6608 is reachable on the unauthenticated
HTTP/1 server header-parse path when a listener is enabled. A
**non-default `api_host` config makes the API listener remotely
reachable** and raises these to remote — but the h2 and TLS classes
still require TLS, which the daemon cannot do, so those stay
unreachable even then.

The mitigation claimed in issue #78 — "HTTP listeners bind 127.0.0.1
only, no outbound TLS" — is **true for the default and shipped
configuration** (code lines above show the loopback binds and the
absence of TLS/client code) **but the loopback property is not closed
by construction**: `api_host` is user-configurable to a non-loopback
address and no config validation prevents it. The advisory therefore
recommends leaving both ports disabled and treating a non-loopback
`api_host` as a misconfiguration.

## Fix version

**v0.2.6** must ship:

- apk payload built with **go >= 1.27.4** (covers all 11 IDs) —
  enforced before signing by the payload Go-version gate
  (scripts/check-apk-payload-go.sh) and the payload govulncheck gate
  (scripts/check-apk-payload-govuln.sh, empty allowlist); the v0.2.6
  package recipe also drops `-buildid=` and sets `STRIP:=true` so the
  payload keeps readable buildinfo for the gates;
- CLI archives built with go1.27.4 via the pinned release.yml toolchain
  (`go-version: "1.27.4"`, setup-go v7 → GOTOOLCHAIN=local).

## Workarounds (until v0.2.6)

1. Keep `api_port` and `metrics_port` at **0 (disabled)** — the OpenWrt
   default — or, if needed, leave `api_host` at `127.0.0.1`.
2. Do not set `api_host` to a non-loopback address. The GET endpoints
   (`/healthz`, `/readyz`, `/metrics`) are **unauthenticated**; if the
   API must be enabled, keep it on loopback and reach it via an SSH
   tunnel (`ssh -L 8080:127.0.0.1:8080 root@router`) or a firewall rule
   that limits access to trusted hosts — never expose it directly on a
   network interface.
3. Do not expose the router's management plane to untrusted networks.
4. The daemon makes **no outbound connections** and does **no TLS**, so
   the x509/tls/client-side classes cannot be reached from a device that
   follows these defaults.

## Root cause (how the vulnerable toolchains got in)

- The apk payload is compiled by the **host Go toolchain that the
  release build mounts into the SDK container** and puts first on PATH
  (`/go-host/bin`) — not by the feed's `golang/host` package (the
  feed's golang build is a build-time dependency, not the compiler the
  package's `Build/Compile` step invokes; `openwrt/Makefile` runs
  `go build` from PATH). The pinned SDK image
  `openwrt/sdk@sha256:a17d6436f7add9207513b6edd0370f5a1aeeceeedf3c9080de8909112892f261`
  (last pushed 2026-10-18) bakes `feeds.conf.default` pinning the
  packages feed at commit `093d551675b1119a06ef224d419905e8d6b2af1a`,
  whose `lang/golang/golang1.27/Makefile` sets `GO_VERSION_PATCH:=1`
  (go1.27.1) — this sets the *feed's* Go, but the apk's compiler is
  the mounted host Go.
- The v0.2.5 release build mounted the host Go **go1.27.1** read-only
  into the SDK container and put `/go-host/bin` first on PATH, so the
  apk was compiled with the host go1.27.1.
- The CLI archives are built in `release.yml` with `make package`; at
  v0.2.5 that ran under setup-go with a floating spec (go1.25.14) and
  GOTOOLCHAIN=auto.

## Reproduction

The exact commands used for every analysis result in this advisory:

```sh
# Source mode under the payload's toolchain (v0.2.5 source):
git worktree add /tmp/v025 v0.2.5
cd /tmp/v025
GOTOOLCHAIN=go1.27.1 govulncheck -mode=source -show traces ./cmd/goshisen ./internal/...
GOTOOLCHAIN=go1.27.1 govulncheck -mode=source -show traces ./...

# Binary mode on published assets:
go version -m goshisen-linux-amd64/goshisen                # CLI archive
strings -n 8 <apk-payload> | grep -A1 'Go buildinf:'        # apk payload toolchain
govulncheck -mode=binary goshisen-linux-amd64/goshisen      # CLI archive
govulncheck -mode=binary <apk-payload>                      # fails: sstrip (unreadable)

# Listener behavior tests (committed in this PR):
go test ./internal/api/ -run 'TestConnectMethodNotAllowed|TestUnauthenticatedOversizedPostRejectedBeforeBodyRead' -v
go test ./internal/app/ -run TestMetricsServerConnectToMetricsPath -v
```

## References

- Issue #78 (S-005), PR #76 (CI hardening: digest pins, Go 1.27.4 in
  CI), S-005 readiness PR (#81), v0.2.6 scope proposal
  (`docs/release/proposal-next-version-scope.md`).
- govulncheck v1.9.0, vuln DB version 2026-11-11 02:51:11Z (fetched
  2026-11-18). Per-release scan outputs and asset digests are recorded
  in the PR description; every asset was sha256-verified against its
  release's `checksums.txt`.
