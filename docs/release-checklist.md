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
- [x] **Positive controls** added to every deny/fail-closed scenario (DeniedTraffic, DefaultDrop, QueueNoListener, StateUnsolicitedInbound, StateInvalidConnection) so a deny assertion cannot pass if the daemon/probe is broken. Verified PASS in the VM (5/5, exit 0). Evidence: `docs/release/positive-controls-ci.md`.
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

**Status note (CI validation):** GitHub-hosted CI (ubuntu-24.04, kernel 7.0.0-1017-azure, Go 1.25.14) is green: build job PASS, integration job 21/21 PASS with zero mandatory skips. The three previously-TCG-timeout-bound scenarios are verified: NAT-reload (18.47s), queue overflow (23.70s), state timeout/hold (2.82s/1.81s) — plus reload-while-established (18.48s). Evidence: `docs/release/ci-validation.md`. Draft PR [#1](https://github.com/88kbleirad/GoShiSen-Enforce/pull/1) targets main; release gate is READY pending merge/tag authorization.

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

- Foundation summary; hardening: queue-overflow monitoring, crash/restart hardening + readiness, supervisor examples, toolchain alignment, security hardening (typed metric enums, sample-config sanitization).
- Explicit note: privileged suite must pass before claiming production readiness; this checklist's §2 is the gate.

---

## OpenWrt release gates (additions)

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
- [x] APK signing test path:
- [x] APK production gate wiring:
- [x] APK staging verification:
- [x] APK key custody handoff:
- [x] APK staging verification results:
- [x] Release gate: **dry-run the package build from the exact commit
  before tagging** — run `make package/feeds/goshisen/{download,compile}`
  in a clean official SDK container at the exact commit and record the
  apk sha256 + daemon binary sha256 in /data/ with the commit SHA (the
  mirror hash must be the clean official-SDK value, e.g. 1867a2de... for
  0.2.3). approval record filled,
  `GH_RELEASE_APK_SIGNING_KEY` provisioned (presence-only), anchor merged
  (PR #34, fingerprint `542bb7ae...` verified). Production gate (GitHub
  Actions, SDK container) PASS; clean-VM install/upgrade/uninstall without
  `--allow-untrusted` PASS; unsigned/wrong-key rejected; gate `--sha256`
  rejects tampered/checksum/provenance mismatches; CLI key/anchor
  rejected for APK. Readiness proposal: READY (staging) — feed/release
  publish remains a separate approval. `docs/release/apk-signing-key-custody.md`
  — approval-record template, provisioning procedure (authorized personnel
  only), staging verification checklist, rotation/revocation/recovery
  procedures. Verdict BLOCKED: no approval record, `GH_RELEASE_APK_SIGNING_KEY`
  absent, APK anchor absent; the agent does not create/read/export/rotate
  keys or provision secrets. production-gate fail-closed
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
  deny verified on the VM; stateful v6 verified on kernel 6.12.112 with
  the canonical bidirectional `ESTABLISHED/RELATED` rule pattern
  (issue #5; `docs/release/issue-5-investigation.md`; e2e v6 CI job).
- [x] Flow offloading: daemon WARN + `goshisen_flow_offload_state`
  metric (0 disabled/1 enabled/2 unknown), re-checked every 10 s;
  `offload_policy` (warn|refuse|readiness|enforce) config/UCI plumbed
  with tests; `enforce` disables and verifies (fail-closed if disable
  fails or state unknown), `allow_bypass` diagnostic hatch; `offload
  status/disable` CLI; documented UCI recommendation; live OpenWrt lab
  evidence (`docs/release/offload-lab-evidence.md`).
- [x] Malformed packets: live parse-error path verified on the VM
  (`goshisen_backend_errors_total{kind="parse"}`), fuzz ≥ 60 s clean.
- [x] Recovery path documented and tested: `goshisen hook cleanup`
  restores forwarding after a stop.
- [ ] **Pin/version agreement gate (v0.2.4+, issue #49):** after
  tagging, commit the Makefile-only follow-up (pin = the tag commit SHA,
  `PKG_VERSION` = tag version) and run
  `scripts/release-pin-gate.sh --apk goshisen-<ver>-r<rel>.apk
  --tag v<ver> --payload <extracted binary>`; it must PASS
  (PKG_VERSION == tag version, pin ancestor of tag, empty runtime diff
  over cmd/internal/pkg/go.mod/go.sum/openwrt/files, payload `--version`
  == PKG_VERSION, apk filename == PKG_VERSION-r<PKG_RELEASE>). See
  `docs/release/release-pin-gate.md` for design and procedure.

## Post-release verification (v0.2.3+)

Run after each publish; every item must pass before the release is
declared done.

- [ ] Public re-verification (unauthenticated): `curl -fsSL
  https://api.github.com/repos/88kbleirad/GoShiSen-Enforce/releases/tags/vX`
  → `draft=false`, `prerelease=false`, expected asset count; download
  each public `browser_download_url`; `sha256sum -c checksums.txt`;
  verify the apk against its anchor (`keys/goshisen-apk.pub.pem`,
  fingerprint `542bb7ae…`); verify CLI signatures with the release key
  (`scripts/verify-signatures.sh --require-sign`).
- [ ] Upgrade/negative VM tests: from the previously published unsigned
  apk with an anchor installed and continuous probes (no fail-open),
  then negatives — anchor removed / unsigned / tampered must all be
  rejected (never `--allow-untrusted` on the production path).
- [ ] Issue status sweep: close resolved follow-up issues (e.g. the
  CI-flake tracker) with the fix commit/run reference; leave open only
  items with a pending decision.
- [ ] Immutable releases: confirm `GET
  /repos/88kbleirad/GoShiSen-Enforce/immutable-releases` →
  `enabled=true` (setting enabled 2026-11-15); no asset
  overwrite/deletion after publish. Keep the workflow's `--clobber`
  uploads to the pre-publish draft only — once published, a `--clobber`
  upload fails under immutability, which is the desired guard.
- [ ] CI flake policy: if a post-release main run fails on a
  docs-only commit, prove the flake before touching code (`git diff
  --name-only <tag-commit> main` must be docs-only; compare the same
  e2e job across runs on an identical SHA) and rerun the failed job
  (`gh api -X POST …/actions/runs/<id>/rerun-failed-jobs`) or apply
  the documented readiness-wait fix; record the outcome in the issue.
