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
| S-002 | integer overflow, config validation (CWE-190) | low | `internal/config/config.go:478`, env layer | fixed | `TestValidateQueueNumIPv6Reserved`, `TestValidateQueueNumBoundaries` (see below) | `880e625` |
| S-003 | CI script injection (CWE-78/CWE-94) | medium | `.github/workflows/release.yml` — unquoted `${{ github.ref_name }}` in run steps | fixed (follow-up PR, unmerged) | `release-tag-guard` CI job; `tests/ci/validate-release-tag-test.sh` | this PR |
| S-004 | supply chain, mutable container tag | low | `.github/workflows/apk-sign.yml` `openwrt/sdk:x86_64-25.12.6` | fixed (follow-up PR, unmerged) | — | this PR |
| S-005 | supply chain, unpatched stdlib in apk payload (CWE-1104) | medium | OpenWrt apk payload (SDK `golang/host` feed, go1.27.1) | **fixed** — fail-closed signing-path gate (script + CI regression) | `scripts/check-apk-payload-go.sh` + `apk-sign.yml` production gate; `tests/ci/check-apk-payload-go-test.sh` | issue #78 |

### S-001 — reachable stdlib vulnerabilities
The API server is an HTTP/1.1 `net/http` server on 127.0.0.1 using
`http.MaxBytesReader` directly (`internal/api/server.go:267`), so the
MaxBytesReader-header race (GO-2026-6859) and the Range-header parsing
limit (GO-2026-6609) are reachable. Fixed by locking `toolchain go1.27.4`
(installed directly by CI setup-go; see below) and bumping
`golang.org/x/net` v0.62.0 + `golang.org/x/sys` v0.49.0. The `go`
directive rose 1.25.0 → 1.26.0 (required by x/net v0.62.0). CI
setup-go installs go1.27.4 directly (`go-version: "1.27.4"`,
matching the toolchain directive); setup-go v6+ sets
`GOTOOLCHAIN=local`, so no build can silently drift to another
toolchain.

### S-002 — queue_num 65535 accepted but uninstallable
`queue_num: 65535` passed validation, but the hook always renders an
IPv6 queue rule at `queue_num+1`, producing `queue to 65536` — outside
the u16 queue-number range — so every `nft -f` install fails at startup
(fail-closed, but the config is accepted and then cannot run).
`validate()` and `GOSHISEN_QUEUE_NUM` now bound it to 0..65534.

Exact behavior (config file and `GOSHISEN_QUEUE_NUM` env layer):

| queue_num | Result | Why |
| --- | --- | --- |
| `0` | accepted (default) | IPv4 rule `queue to 0`, IPv6 rule `queue to 1` — both valid u16 |
| `65534` | accepted | largest usable value; IPv6 rule renders `queue to 65535` (max u16) |
| `65535` | **refused to start** | IPv6 `queue+1` would render `queue to 65536` — outside u16; never silently clamped to 65534 |
| `65536` / `70000` | **refused to start** | outside `0..65534`; never wraps to 0 |
| `-1` | **refused to start** | negative; never wraps/truncates |
| `abc` | **refused to start** | non-numeric; never defaults |

Proven by `TestValidateQueueNumBoundaries` (file + env layers, incl.
0/65534 accepted and 65535/65536/-1/70000/abc rejected with no
wrap/truncate/default), `TestValidateQueueNumIPv6Reserved` (65535
rejected, 65534 accepted), `TestValidateQueueNum` (70000 rejected), and
`TestBuildFilterOnly` in `internal/hook` (queue_num 0 renders IPv4
`queue to 0` + IPv6 `queue to 1`). A rejected value makes
`config.Load` return an error and `app.RunCLI` abort — the daemon
refuses to start (fail closed).

### S-003 — CI script injection via tag ref name
`release.yml` interpolates `${{ github.ref_name }}` (the pushed tag
name) into shell commands in the release job. Git ref names are
attacker-controllable up to tag-push rights, which are held by the same
maintainers who run the release workflow; the release job also holds the
release signing key material. A tag name containing shell metacharacters
would therefore create a command-injection risk in that job (CWE-78 /
CWE-94).

**Mitigation (in this PR):** the release workflow validates the tag
name against `^v[0-9]+\.[0-9]+\.[0-9]+$` in a first step before any
other step (including checkout), passes the tag through an environment
variable, and quotes it at every use. `apk-sign.yml` production mode
gets the same ref validation. The `release-tag-guard` CI job runs
`tests/ci/validate-release-tag-test.sh`, which proves tag-name fixtures
containing shell metacharacters (`;`, `$()`, backticks, pipes,
redirection, quotes) are rejected before use, while valid semver tags
pass. No public reproduction steps are included.

### S-004 — mutable container image tag
`openwrt/sdk:x86_64-25.12.6` (apk-sign.yml) was a mutable tag; a
compromised/republished tag would change the signing environment.
Fixed in this PR by pinning the image by digest
(`openwrt/sdk@sha256:a17d6436f7add9207513b6edd0370f5a1aeeceeedf3c9080de8909112892f261`,
amd64 runner). The Dockerfile base images (`golang:1.27.4`,
`gcr.io/distroless/base-debian13`) are digest-pinned the same way, with
update instructions at each pin. The syft checksum-verification pattern
for tools is unchanged.

### S-005 — apk payload built with a vulnerable Go (fixed by signing-path gate)
The published v0.2.5 OpenWrt apk payload (`usr/sbin/goshisen`) is built
by the OpenWrt SDK's `golang/host` feed (not CI's Go) and embeds
**go1.27.1** with `golang.org/x/net v0.53.0` / `x/sys v0.43.0`.
govulncheck under that toolchain reproduces the S-001 finding exactly:
**11 reachable standard-library vulnerabilities** (GO-2026-6859
MaxBytesReader race, GO-2026-6610/6611/6612/6613/6617 http2 races,
GO-2026-6603/6605/6607/6608, GO-2026-6809 x509) — **not fixed** in the
apk. The CLI release archives are a separate build (go1.25.14 at
v0.2.5; fixed to go1.27.4 by S-001 in CI from the next release on).
The SDK feed now pins Go 1.27.4 (`lang/golang/golang1.27/Makefile`).

**Fix (issue #78, option 2):** a fail-closed gate
(`scripts/check-apk-payload-go.sh`) runs in `apk-sign.yml` production
mode **before signing**: it extracts the payload from the staged apk and
verifies the embedded Go toolchain is >= 1.27.4 (the S-001-pinned
version). An apk built with an older SDK/feed Go (or one whose payload
is missing/unreadable) fails the signing path closed. The gate itself
cannot regress: the `apk-payload-go-gate (S-005)` CI job
(`tests/ci/check-apk-payload-go-test.sh`) exercises pass + fail-closed
fixtures on every PR. The released v0.2.5 apk remains as-is; the next
apk rebuild with the current SDK (Go 1.27.4) will satisfy the gate.

## Release tags are strict semver (no -rc)

The S-003 release-tag validator accepts only `vX.Y.Z` (no suffix) in
the release workflow and in `apk-sign.yml` production mode. If release
candidates (`v0.2.6-rc.1`) are ever wanted, the pattern must be changed
in **all three places together**: `scripts/validate-release-tag.sh`,
the identical inline check in `.github/workflows/release.yml`, and the
ref-validation case in `.github/workflows/apk-sign.yml`; and the
`release-tag-guard` fixture in `tests/ci/validate-release-tag-test.sh`
must be extended with the new allowed pattern.

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
  govulncheck). Remaining: none (S-004 and S-005 fixed).

## Environment note

`internal/hook` nft syntax tests fail on this host (kernel lacks
`NFT_QUEUE`); reproduced on unchanged `origin/main`, passes in CI. Not a
finding (see `docs/audit-self-review-20261117.md`).
