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

- [ ] Tag format: `v0.1.0` (semver). `make build VERSION=v0.1.0` stamps the version.
- [ ] `git tag v0.1.0 && git push origin v0.1.0` (after privileged suite passes).
- [ ] CI on the tag runs green (build + integration jobs).
- [ ] Publish the container image (optional) from the tagged checkout with `--cap-drop=ALL --cap-add=NET_ADMIN --cap-add=NET_RAW` per the Dockerfile guidance.

## 6. Release notes (draft)

- Phase 1–6 foundation summary; Phase 7: queue-overflow monitoring, crash/restart hardening + readiness, supervisor examples, toolchain alignment, security hardening (typed metric enums, sample-config sanitization).
- Explicit note: privileged suite must pass before claiming production readiness; this checklist's §2 is the gate.
