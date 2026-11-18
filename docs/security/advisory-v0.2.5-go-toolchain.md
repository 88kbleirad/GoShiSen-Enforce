# Security Advisory (draft) — v0.2.5 OpenWrt apk and CLI archives built with vulnerable Go toolchains (S-005)

> **STATUS: DRAFT — not published.** This file is the advisory text under
> review for the v0.2.6 release. It is deliberately a plain file in the
> repository; no GitHub security advisory has been or will be published
> without the maintainer's explicit approval (out of scope for this
> investigation).

- **Finding:** S-005 (issue #78)
- **Affected products:** GoShiSen-Enforce v0.2.5 OpenWrt apk
  (`goshisen-0.2.5-r1.apk`) and v0.2.5 CLI/daemon archives
  (`goshisen-linux-amd64.tar.gz`, `goshisen-linux-arm64.tar.gz`).
  Earlier releases (v0.2.0–v0.2.4) are **also affected** — they were
  built with the same or older Go toolchains (v0.2.4 SDK feed Go 1.26.x
  and a go1.27.x host toolchain; see build records in `docs/release/`).
  The fix lands in **v0.2.6**.
- **Date:** 2026-11-18 (analysis), govulncheck DB 2026-11-11, scanner
  govulncheck v1.9.0 / Go 1.27.4.
- **Severity (CVSS v3.1):** Low (local) / Medium (misconfigured remote),
  see classification. No remote reachability with the default
  configuration: the daemon's HTTP listeners bind 127.0.0.1 and both
  ports default to disabled (0) in the OpenWrt package config. The
  daemon makes no outbound connections and does no TLS.

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
   267).
2. Metrics (`internal/app/app.go:519-533`): `serveMetrics` builds
   `http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", port)}`
   (app.go:521) and serves only `/metrics`; the bind is hardcoded
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

## Vulnerability summary (11 stdlib IDs, govulncheck source mode)

govulncheck v1.9.0, source mode with `-show verbose`, at the v0.2.5
tag commit `79927ca` under the payload's go1.27.1 toolchain
(`GOTOOLCHAIN=go1.27.1`), on the shipped packages `./cmd/goshisen
./internal/...`:

- **Your code is affected by 6 vulnerabilities from the Go standard
  library** (GO-2026-6859, 6617, 6613, 6612, 6611, 6603 — all with
  `http.Server` / HTTP listener traces).
- **6 more stdlib packages are listed without a call trace** (GO-2026-
  6809 x509, 6610 h2, 6609 net/http Range, 6608 mime/multipart,
  6607 crypto/tls, 6605 net/http CONNECT) — govulncheck binary-mode
  would classify these as "called but not traced"; with no code path
  (no TLS, no client, no multipart parse) they are not reachable.
- The vuln DB (vuln.go.dev, fetched 2026-11-18) fixes all 11 IDs in
  go1.27.4 (`net/http` 6859; `crypto/x509` 6809; `net/http/internal/
  http2` 6617/6612/6611/6610/6603; `crypto/tls` 6607; `mime/
  multipart` 6608; `net/http` 6613/6609/6605).

## Per-vulnerability classification (daemon binary, default config)

| GO ID | package/symbol | call path from our code | precondition (attacker must control) | classification |
| --- | --- | --- | --- | --- |
| GO-2026-6859 | net/http (MaxBytesReader race) | `api.Server.Serve` → `http.Server.Serve` (server.go:143); `app.Daemon.serveMetrics` → `http.Server.ListenAndServe` (app.go:530) | an authenticated client (valid bearer token) sends an HTTP/1 POST with a body that exceeds `MaxBodyBytes` (16 KiB), so the handler's `readBody` (server.go:267, `http.MaxBytesReader`) races its own response-header write; `requireAuth` runs before `readBody`, so unauthenticated requests never read bodies; the metrics handler never reads bodies | **local user with API token** (loopback listener) / **remote with non-default `api_host`** |
| GO-2026-6613 | net/http (CONNECT desync) | same `http.Server.Serve` path (server.go:143) | a client sends an HTTP/1 CONNECT request; the server handler would have to reply 2xx to CONNECT — no handler accepts CONNECT (mux has no CONNECT pattern; net/http answers 405) | **not reachable in practice** (no handler emits 2xx CONNECT; listener reachable only when enabled) |
| GO-2026-6617 | net/http/internal/http2 (HPACK race) | `http.Server.Serve` → h2 only after TLS negotiation | TLS-negotiated h2 — no `tls.Config`, plaintext listener | **not reachable (no TLS)** |
| GO-2026-6612 | net/http/internal/http2 (double flow refund) | `http.Server.Serve` h2 path | same | **not reachable (no TLS)** |
| GO-2026-6611 | net/http/internal/http2 (SETTINGS CPU) | `http.Server.Serve` h2 path | same | **not reachable (no TLS)** |
| GO-2026-6610 | net/http/internal/http2 (malformed framing, client) | client-side h2 | an outbound h2 client — no `http.Client`, no `net.Dial` | **not reachable (no client)** |
| GO-2026-6603 | net/http/internal/http2 (Trailer memory) | `http.Server.Serve` h2 path | TLS-negotiated h2 client | **not reachable (no TLS)** |
| GO-2026-6608 | mime/multipart (memory limit bypass) | no call trace; no `mime/multipart` parse in shipped code (bodies are raw JSON via `io.ReadAll` after `MaxBytesReader`) | a multipart form body | **not reachable (no multipart)** |
| GO-2026-6605 | net/http (CONNECT rejection, client) | client-side | an outbound HTTP client | **not reachable (no client)** |
| GO-2026-6607 | crypto/tls (ECH outer extension) | no call trace; no `crypto/tls` import | a TLS listener/client | **not reachable (no TLS)** |
| GO-2026-6809 | crypto/x509 (intermediates) | no call trace; no x509 use (no TLS) | TLS verification | **not reachable (no TLS)** |

Notes:

- The whole-repo scan (including `tests/integration/probe`, an
  unsupported test tool; `http.Client` at probe/main.go:248) reports 11
  affected stdlib IDs; the shipped daemon links the 6 above.
- The go1.25.14 CLI archives additionally have **GO-2026-6608 on the
  server header-parse path** (in go1.25.14 the HTTP/1 server calls
  `textproto.ReadMIMEHeader` on every request; the go1.27.1 server does
  not) — so the CLI archives are affected by the local-user class for
  6608 too. (go1.25.14 source mode also lists 6600/6599 html/template;
  the daemon never imports html/template.)
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
6613 needs a handler that replies 2xx to CONNECT, which no handler
does (net/http answers 405). On the CLI archives (go1.25.14) 6608 is
reachable on the unauthenticated HTTP/1 server header-parse path when
a listener is enabled. A **non-default `api_host` config makes the
API listener remotely reachable** and raises these to remote — but
the h2 class still requires TLS, which the daemon cannot do, so those
stay unreachable even then.

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
2. Do not set `api_host` to a non-loopback address.
3. Do not expose the router's management plane to untrusted networks.
4. The daemon makes **no outbound connections** and does **no TLS**, so
   the x509/tls/client-side classes cannot be reached from a device that
   follows these defaults.

## Root cause (how the vulnerable toolchains got in)

- The apk payload is compiled by the OpenWrt SDK's `golang/host` feed
  package, **not** by CI's Go. The pinned SDK image
  `openwrt/sdk@sha256:a17d6436f7add9207513b6edd0370f5a1aeeceeedf3c9080de8909112892f261`
  (last pushed 2026-10-18) bakes `feeds.conf.default` pinning the
  packages feed at commit `093d551675b1119a06ef224d419905e8d6b2af1a`,
  whose `lang/golang/golang1.27/Makefile` sets `GO_VERSION_PATCH:=1`
  (go1.27.1).
- The v0.2.5 release build mounted the host Go **go1.27.1** read-only
  into the SDK container and put `/go-host/bin` first on PATH, so the
  apk was compiled with the host go1.27.1.
- The CLI archives are built in `release.yml` with `make package`; at
  v0.2.5 that ran under setup-go with a floating spec (go1.25.14) and
  GOTOOLCHAIN=auto.

## References

- Issue #78 (S-005), PR #76 (CI hardening: digest pins, Go 1.27.4 in
  CI), S-005 readiness PR (this investigation), v0.2.6 scope proposal
  (`docs/release/proposal-next-version-scope.md`).
- govulncheck v1.9.0 output files archived under `/data/s005/` (host
  evidence; sha256 in the evidence index).
