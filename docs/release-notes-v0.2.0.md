# GoShiSen-Enforce — v0.2.0 Release Notes (draft)

## Summary
v0.2.0 adds reproducible Linux binary distribution and production
packaging to the v0.1.0 firewall daemon. No changes to the firewall
enforcement model.

## New in v0.2.0
- **Reproducible Linux release binaries** — `goshisen-linux-amd64.tar.gz`
  and `goshisen-linux-arm64.tar.gz`, statically linked (CGO disabled),
  version-stamped, byte-for-byte reproducible for a fixed source tree
  and version.
- **Checksums** — `checksums.txt` (SHA-256) for every release archive.
- **SBOM** — `sbom.spdx.json` (SPDX 2.3) generated with syft 1.30.0
  (pinned, checksum-verified).
- **Packaging automation** — the CI `package` job builds + verifies the
  archives on every pull request (arch, static linkage, version stamp,
  checksums, SBOM, reproducibility). The `release` workflow runs on
  `v*` tags and attaches artifacts to a **draft** release — publication
  remains a manual, authorized step.
- **Installation documentation** — `docs/installation.md`: supported
  platforms, kernel requirements, binary install, checksum
  verification, configuration, capabilities, systemd, safe
  activation, upgrade, rollback, troubleshooting, safe removal.
- **Container feasibility evaluation** — `docs/container-feasibility.md`:
  containerized deployment is documented but deferred; the OCI image
  is not advertised as a host firewall.

## Security
- Fail-closed behavior unchanged: the daemon never sets
  `NFQA_CFG_F_FAIL_OPEN`; every error path drops.
- Packaging scripts are additive; they do not touch nftables or the
  firewall model. The privileged integration suite remains a
  mandatory CI gate (21/21 PASS, zero skips).

## Known limitations
- arm64 binaries are built and statically verified; runtime execution
  on arm64 requires an arm64 host (validated there).
- The container image is not published; see
  `docs/container-feasibility.md`.
- `nft` (nftables userspace) must be present on the target host.

## Verification
- Go 1.25, ubuntu-24.04 CI: build/vet/race/smoke PASS; package job
  build+verify PASS; privileged integration 21/21 PASS, zero skips.
- Local: `go mod verify`, `gofmt`, `go vet`, `go test -race ./...`,
  packaging regression test (`tests/package`) PASS.

## OpenWrt integration (added in phase 10/11)

- **OpenWrt package** (`openwrt/`, SDK-built `.apk`): procd init
  script (disabled by default; `config_load`, `procd_send_signal`
  reload, enabled-gate), UCI config, fail-closed default policy.
- **IPv6 enforcement**: the hook is now an `inet` table that queues
  IPv4 (queue 100) and IPv6 (queue 101) forwarded TCP/UDP separately;
  the parser and policy prefixes support IPv6. IPv6 no longer bypasses
  the daemon (previously a fail-open gap). Stateful IPv6 ACCEPT is
  limited on kernels without IPv6 conntrack-attribute delivery
  (packets are then INVALID → DROP; stateless IPv6 rules work).
- **Flow-offload detection**: the daemon detects nftables flowtables /
  `flow add @ft` and warns loudly (WARN + `goshisen_flow_offload_active`
  gauge, re-checked every 10 s). It never modifies the firewall.
- **Malformed-packet observability**: parse/panic/send backend errors
  are exported as `goshisen_backend_errors_total{kind=...}`.
- **Static release builds**: `make build` produces a statically linked,
  trimmed, reproducible binary; CI asserts it.
