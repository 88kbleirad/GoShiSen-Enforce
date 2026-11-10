# GoShiSen-Enforce — v0.1.0 Release Notes (draft)

## Summary
GoShiSen-Enforce is a fail-closed, stateful nftables/NFQUEUE firewall daemon with a small embedded API and Prometheus metrics. v0.1.0 is the first tagged release.

## New in v0.1.0
- **NFQUEUE protocol fixes** — correct `NFQA_CT_INFO` 4-byte big-endian decode, 0-based `ip_conntrack_info` handling, direction-normalized accepted-set, and a `parseAttrs` truncation guard (previously a panic vector; now fuzzed).
- **Fail-closed enforcement** — every queue overflow / hook / parser / engine error results in DROP; `NFQA_CFG_F_FAIL_OPEN` is never set. No fail-open path exists.
- **Stateful filtering** — TCP/UDP connection tracking with handshake, established/related handling, INVALID drop, timeout behavior, and reload-while-established (SIGHUP atomic policy swap; old policy kept on failure).
- **NAT** — DNAT, MASQUERADE, return traffic, NAT+filtering, and reload-while-NAT with conntrack-preserved flows.
- **Queue-overflow monitoring** — kernel `queue_dropped` observability via `/proc/net/netfilter/nfnetlink_queue`, Prometheus overflow metric, rate-limited warnings.
- **Crash hardening** — fail-closed drop on SIGKILL, restart recovery, readiness gating (fake-ready impossible).
- **Privileged integration suite** — 21 real-netns scenarios with positive controls on every deny/fail-closed path, mandatory-skip rejection in CI.
- **Toolchain alignment** — Go 1.25; CI on `ubuntu-24.04`.

## Security
- API binds `127.0.0.1` by default; 16 KiB body limit; server timeouts set.
- Hook touches only `table ip goshisen`; NAT+filter applied atomically (one `nft -f` transaction).
- Metric labels bounded; no IP/port/flow-ID cardinality.
- Sample config has no credential; `config show` redacts.

## Known limitations / remaining risks
- Privileged integration requires a kernel with NFQUEUE+conntrack and `CAP_NET_ADMIN`; unsupported kernels are detected by preflight and fail loudly (never silently skipped).
- Conntrack timeouts are kernel defaults; the suite verifies behavior without waiting for full expiry.
- See `docs/release-checklist.md` §2 for the full gate.

## Verification
- GitHub-hosted CI (ubuntu-24.04, kernel 7.0.0-1017-azure, Go 1.25.14): build/vet/race/build/smoke PASS; privileged integration 21/21 PASS, zero mandatory skips.
- Three phase-8.7 leftovers now verified: NAT-reload (18.47s), queue overflow (23.70s), state timeout/hold (2.82s/1.81s); reload-while-established 18.48s.
- Positive controls on every deny/fail-closed scenario pass; no test weakened or skipped to green.
- Evidence: `docs/release/phase9-ci-validation.md`, `docs/release-checklist.md` §2.

## Instructions
```bash
make build VERSION=v0.1.0
sudo make integration   # full privileged suite; skips are a blocker
```
Container: include `--cap-drop=ALL --cap-add=NET_ADMIN --cap-add=NET_RAW`.
