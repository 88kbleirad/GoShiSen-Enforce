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

- [x] Prereqs verification is now **functional**: `checkPrereqs` creates a throwaway namespace and asserts `nf_conntrack_count > 0` after a real TCP flow (skip with explicit reason otherwise). Reviewer: `tests/integration/e2e_test.go` `checkConntrackNFQUEUE`.
- [ ] `sudo scripts/lab.sh up` succeeds; `sudo make integration` runs the full suite (all mandatory tests **must execute; skips are a release blocker**):
  - default DROP; explicit TCP/UDP allow; denied traffic; first-match semantics
  - stateful handshake+echo; unsolicited inbound; INVALID state; timeout/hold; UDP reuse
  - policy reload while established (SIGHUP atomic; old policy kept on failure)
  - DNAT, MASQUERADE, return traffic, NAT+filtering, reload-while-NAT
  - queue overflow (S11): kernel `queue_dropped` grows; warning logged; policy still enforced
  - daemon crash (SIGKILL) → fail-closed drop; restart → recovery; readiness gating
  - hook cleanup scope (host nftables untouched; foreign table survives)
- [ ] `sudo scripts/lab.sh down` clean; `ip netns list` shows no `gsh_*` leftovers.
- [ ] Teardown also runs after a killed/aborted test (cleanup traps).

**Status note (Phase 8.5):** the VM (Debian 13, kernel 6.12.116, QEMU TCG) passes BIND/readiness/hook-cleanup/fail-closed-DROP but does not deliver conntrack metadata on queued packets (VM kernel/netns quirk; root-ns tracking works). Stateful allow/NAT/overflow/recovery therefore remain **unvalidated** pending a conntrack-capable runner (GitHub `ubuntu-24.04`). Evidence: `docs/release/phase8.5-conntrack-ci-validation.md`.

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
