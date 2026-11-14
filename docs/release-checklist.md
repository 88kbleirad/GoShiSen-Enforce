# GoShiSen-Enforce — Release Checklist (v0.1.0)

Run in order. Every item must pass before tagging.

## 1. Clean-checkout build gate

- [ ] `git clone` the repo fresh (cold clone); `go mod verify` passes.
- [ ] `gofmt -l .` outputs nothing.
- [ ] `go vet ./...` passes.
- [ ] `go test -race ./...` passes (integration tests skip as non-root with explicit reasons).
- [ ] `go build ./...` passes.
- [ ] `make check` passes (fmt + vet + test + build).
- [ ] CLI smoke: `go build -o /tmp/goshisen ./cmd/goshisen`; `--help`, `--version`; missing config exits non-zero.

## 2. Privileged integration suite (root host, in the lab)

- [x] Prereqs verification is now **functional**: `checkPrereqs` verifies an nftables queue rule installs in a throwaway namespace (NFQUEUE present) and asserts `nf_conntrack_count > 0` after a real TCP flow (skip with explicit reason otherwise). Reviewer: `tests/integration/e2e_test.go` `checkNFQUEUERule` / `checkConntrackNFQUEUE`.
- [x] **Positive controls** added to every deny/fail-closed scenario (DeniedTraffic, DefaultDrop, QueueNoListener, StateUnsolicitedInbound, StateInvalidConnection) so a deny assertion cannot pass if the daemon/probe is broken. Verified PASS in the VM (5/5, exit 0). Evidence: `docs/release/phase8.7-positive-controls-ci.md`.
- [x] `sudo scripts/lab.sh up` succeeds; `sudo make integration` runs the full suite (all mandatory tests **must execute; skips are a release blocker**):
  - default DROP; explicit TCP/UDP allow; denied traffic; first-match semantics
  - stateful handshake+echo; unsolicited inbound; INVALID state; timeout/hold; UDP reuse
  - policy reload while established (SIGHUP atomic; old policy kept on failure)
  - DNAT, MASQUERADE, return traffic, NAT+filtering, reload-while-NAT
  - queue overflow (S11): kernel `queue_dropped` grows; warning logged; policy still enforced
  - daemon crash (SIGKILL) → fail-closed drop; restart → recovery; readiness gating
  - hook cleanup scope (host nftables untouched; foreign table survives)
- [x] `sudo scripts/lab.sh down` clean; `ip netns list` shows no `gsh_*` leftovers.
- [x] Teardown also runs after a killed/aborted test (cleanup traps).

**Status note (Phase 9.2):** GitHub-hosted CI (ubuntu-24.04, kernel 7.0.0-1017-azure, Go 1.25.14) is green: build job PASS, integration job 21/21 PASS with zero mandatory skips. The three previously-TCG-timeout-bound scenarios are verified: NAT-reload (18.47s), queue overflow (23.70s), state timeout/hold (2.82s/1.81s) — plus reload-while-established (18.48s). Evidence: `docs/release/phase9-ci-validation.md`. Draft PR [#1](https://github.com/88kbleirad/GoShiSen-Enforce/pull/1) targets main; release gate is READY pending merge/tag authorization.

## 3. Security & hardening checks

- [ ] No token in argv/logs/metrics; `config show` redacts; sample config has no credential.
- [ ] API binds 127.0.0.1 by default; body limit 16 KiB; server timeouts set.
- [ ] Hook touches only `table ip goshisen`; NAT+filter applied atomically (one `nft -f` transaction).
- [ ] No fail-open path: overflow/queue/hook/parser/engine errors all DROP; `NFQA_CFG_F_FAIL_OPEN` never set.
- [ ] Metric labels bounded (no IP/port/flow-ID); `go mod verify` clean.

## 4. Docs consistency

- [ ] README status, user guide, developer guide, architecture, mvp-scope, SECURITY.md, OpenAPI agree with the implementation.
- [ ] Go version consistent: go.mod `1.25.0`, CI `1.25`, Docker `golang:1.25`, docs `≥ 1.25`.
- [ ] Every documented command is executable from the repo root.

## 5. Release mechanics

- [ ] Tag format: `v0.1.0` (semver). `make build VERSION=v0.1.0` stamps the version. CI workflow now triggers on `v*` tags (added in `f1f09c1`).
- [ ] `git tag v0.1.0 && git push origin v0.1.0` (after privileged suite passes).
- [ ] CI on the tag runs green (build + integration jobs, zero mandatory skips).
- [ ] Version stamp: `make build VERSION=v0.1.0` then `bin/goshisen --version` must print `goshisen v0.1.0` (not `dev`). Guarded in CI (`Verify version stamp` step; `internal/cli.Version` must be a `var`, not a `const` — ldflags `-X` cannot set constants).
- [ ] Packaging: `make package VERSION=$TAG` builds `goshisen-linux-amd64.tar.gz` + `goshisen-linux-arm64.tar.gz` + `checksums.txt` + `sbom.spdx.json`; `make verify-package VERSION=$TAG` validates arch, static linkage, version stamp, checksums, SBOM; archives byte-reproducible. Guarded by the CI `package` job and `tests/package`.
- [ ] Release workflow (`.github/workflows/release.yml`) runs on `v*` tags, builds + verifies, and attaches artifacts to the tag's release as a **draft** (never auto-publishes).
- [ ] Publish the container image (optional) from the tagged checkout with `--cap-drop=ALL --cap-add=NET_ADMIN --cap-add=NET_RAW` per the Dockerfile guidance.

## 6. Release notes (draft)

- Phase 1–6 foundation summary; Phase 7: queue-overflow monitoring, crash/restart hardening + readiness, supervisor examples, toolchain alignment, security hardening (typed metric enums, sample-config sanitization).
- Explicit note: privileged suite must pass before claiming production readiness; this checklist's §2 is the gate.

---

## Phase 11 additions (OpenWrt release gates)

- [x] OpenWrt package pin: `PKG_SOURCE_VERSION` points to a **pushed**
  commit and `PKG_MIRROR_HASH` is set (verify with `curl -fsSL
  https://codeload.github.com/.../tar.gz/<sha>` + `sha256sum`).
- [x] `make build` produces a **statically linked** `bin/goshisen`
  (`make verify-static` passes); CI build job asserts it via `file` and
  `ldd`.
- [x] OpenWrt SDK build of `package/feeds/goshisen` succeeds from the
  git pin alone (no file:// override), and the `.apk` payload contains:
  init script (with `service_enabled` gate), UCI (netdev `interface`),
  fail-closed policy, static binary, `prerm` that runs `hook cleanup`.
- [x] OpenWrt VM procd lifecycle: start/stop/restart/reload (SIGHUP
  atomic; invalid policy keeps previous + logs), disabled-by-default,
  boot persistence (daemon auto-starts, queues 100/101 bound), respawn
  after SIGKILL with traffic blocked during the gap and no duplicate
  tables, `uci commit` causes no restart loop.
- [x] apk install/upgrade/remove: no fail-open window; `apk del`
- [x] APK signing test path (Prompt 35):
- [x] APK production gate wiring (Prompt 36):
- [x] APK staging verification (Prompt 37): production-gate fail-closed
  verified at `94bd7df` (missing secret → BLOCKED exit 1); staging
  runbook documented (secret provision → anchor commit+fingerprint →
  gate → /etc/apk/keys/ install → clean-VM install without
  `--allow-untrusted` → production-like negative controls). Verdict
  stays BLOCKED: `GH_RELEASE_APK_SIGNING_KEY` not provisioned, APK
  anchor absent, owner/rotation/rollback records not recorded. `scripts/apk-release-gate.sh`
  (secret-gated sign → `apk-verify.sh --sha256` → provenance manifest;
  fail-closed exit codes) + `.github/workflows/apk-sign.yml` (test mode
  end-to-end; production mode requires `GH_RELEASE_APK_SIGNING_KEY` +
  approved anchor/fingerprint; negative-controls job). Production APK
  signing stays BLOCKED until an approved key/anchor is provisioned;
  never `--allow-untrusted` on the production path; no feed/release
  publish from this workflow. `scripts/apk-sign.sh` signs a
  v3 apk with an ephemeral key; `scripts/apk-verify.sh` verifies against
  a trust dir with an optional `--sha256` full-file integrity gate.
  Verified on a clean OpenWrt VM: `apk add <signed>` succeeds WITHOUT
  `--allow-untrusted` (pubkey in `/etc/apk/keys/`); unsigned and tampered
  apks are rejected by the package manager. Production APK signing
  remains BLOCKED (no approved key/secret); do not use
  `--allow-untrusted` on the production path.
  removes the app-owned `inet goshisen` table (no orphaned queue rule);
  user config survives as conffiles.
- [x] IPv6: `inet` hook queues both families; stateless IPv6 allow/
  deny verified on the VM; stateful-v6 limit documented (CT-glue gap →
  INVALID → DROP).
- [x] Flow offloading: daemon WARN + `goshisen_flow_offload_state`
  metric (0 disabled/1 enabled/2 unknown), re-checked every 10 s;
  `offload_policy` (warn|refuse|readiness|enforce) config/UCI plumbed
  with tests; `enforce` disables and verifies (fail-closed if disable
  fails or state unknown), `allow_bypass` diagnostic hatch; `offload
  status/disable` CLI; documented UCI recommendation; live OpenWrt lab
  evidence (`docs/release/prompt15-offload-lab-evidence.md`).
- [x] Malformed packets: live parse-error path verified on the VM
  (`goshisen_backend_errors_total{kind="parse"}`), fuzz ≥ 60 s clean.
- [x] Recovery path documented and tested: `goshisen hook cleanup`
  restores forwarding after a stop.
