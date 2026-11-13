# GoShiSen-Enforce

**A Linux lab firewall: Go policy engine over NFQUEUE, kernel conntrack, nftables hooks.**

> **Status: released v0.1.0; v0.2.0 drafted (OpenWrt package, IPv6 enforcement, flow-offload awareness); flow-offload policy (Phase 14) landed.** The daemon (policy engine, NFQUEUE enforcement, stateful filtering, management API/metrics, NAT, overflow monitoring, crash/restart hardening) is released as v0.1.0 with the privileged integration suite at 21/21 PASS and zero mandatory skips on GitHub-hosted runners. v0.2.0 adds reproducible Linux release binaries (amd64 + arm64), checksums, SBOM, packaging automation, and installation/upgrade documentation. See `docs/installation.md`.

## OpenWrt

The OpenWrt package (`openwrt/`, SDK-built `.apk`) adds GoShiSen-Enforce
as a fail-closed enforcement layer after firewall4 on the forward path.
Validated on OpenWrt 25.12.6 x86/64 (kernel 6.12.112); the service is
disabled by default.

- `docs/openwrt.md` — install, UCI options, procd usage, boot behavior,
  recovery, upgrade/removal.
- `docs/release/v0.2.0-notes.md` — verified behavior and known
  limitations (boot-time window, stateful IPv6, flow offloading,
  architecture coverage, stop semantics).
- `docs/release/phase11-openwrt-validation.md` — full evidence matrix,
  CI run IDs, reproducibility and package results.

## What works today

- **NAT** (`internal/hook`, kernel nftables): DNAT (`dstnat` prerouting chain) and MASQUERADE (`srcnat` postrouting chain) configured under `nat:` in the daemon config. Applied as one `nft -f` transaction; failed apply changes nothing; only `table ip goshisen` is ever touched. Documented semantics: **the policy engine sees post-DNAT destination and pre-MASQUERADE source** (see `docs/design/architecture.md` §8.2).
- **Packaging**: reproducible release archives (`goshisen-linux-amd64.tar.gz`, `goshisen-linux-arm64.tar.gz`) built by `make package` — static, CGO-disabled, version-stamped, with `checksums.txt` and an SPDX SBOM. The `package` CI job builds + verifies on every PR; the `release` workflow attaches artifacts to a draft release on `v*` tags (never auto-publishes). Installation/upgrade/rollback: `docs/installation.md`. Container packaging (multi-stage `deployments/docker/Dockerfile`) exists but is **deferred** — see `docs/container-feasibility.md`.
- **Queue overflow monitoring** (Phase 7): the daemon watches the kernel's per-queue stats (`/proc/net/netfilter/nfnetlink_queue`) and exports `goshisen_queue_overflow_total{cause="kernel_drop"|"user_drop"}` plus rate-limited `WARN` logs. The kernel drops overflowing packets by construction — the backend never sets `NFQA_CFG_F_FAIL_OPEN`, so overflow never bypasses the firewall. `queue_maxlen` (file/env/flag, default 1024) tunes the queue depth.
- Phase 1–5 unchanged: policy engine, NFQUEUE enforcement, stateful filtering, REST API, metrics, durable store, reload.

## Integration tests & privileges

The lab needs root (CAP_NET_ADMIN). As a non-root user, `go test ./...` compiles the suite and skips it with the reason `integration tests require root for CAP_NET_ADMIN`. Run it with:

```bash
sudo make integration
```

Privileged integration (21 scenarios: Phase 3 traffic pass/drop, default DROP, no-listener, clean shutdown; Phase 4 handshake+echo, unsolicited inbound, invalid state, timeout/hold, UDP re-use, reload-while-established; Phase 6 DNAT, MASQUERADE, return traffic, NAT+filtering, reload-while-NAT-connections, hook cleanup scope; Phase 7 crash/restart + readiness, queue overflow) runs on GitHub-hosted CI with a real netns lab — 21/21 PASS with zero mandatory skips. This unprivileged dev environment cannot run them locally (they require root + a kernel with NFQUEUE); the check is delegated to the isolated CI runner.

## Quick start

```bash
make build                      # produces bin/goshisen (version from git describe)

# Validate a config without starting the daemon:
./bin/goshisen -c configs/goshisen.yaml config check

# Validate a policy and inspect the compiled rules:
./bin/goshisen policy validate --policy-file policies/examples/allow-lab.yaml

# Simulate a packet — verdict + matching rule ID, no network changes:
./bin/goshisen policy simulate --policy-file policies/examples/allow-lab.yaml \
  --protocol tcp --state new --src 10.0.0.5 --dst 10.0.0.2 --dest-port 80
#   verdict: ACCEPT
#   matched rule: allow-http

# Run inside the lab router namespace (root). It installs the hook in
# that namespace only; host firewall untouched.
sudo scripts/lab.sh up
sudo ip netns exec gsh_fw ./bin/goshisen \
  --interface gsh-fw1 --policy-file policies/examples/allow-lab.yaml
sudo scripts/lab.sh down
```

Sample configuration: `configs/goshisen.yaml`. Sample policies: `policies/default/deny-all.yaml`, `policies/examples/allow-lab.yaml`.

## Configuration

Precedence (lowest to highest):

| Layer | Example |
| --- | --- |
| built-in defaults | `log_level: info`, `log_json: false` |
| config file | `interface: eth0` |
| environment | `GOSHISEN_INTERFACE=eth0` |
| flags | `--interface eth0` |

| Key | Flag | Environment | Default |
| --- | --- | --- | --- |
| `interface` | `--interface` | `GOSHISEN_INTERFACE` | auto (first up, non-loopback) |
| `policy_file` | `--policy-file` | `GOSHISEN_POLICY_FILE` | *(required)* |
| `queue_maxlen` | `--queue-maxlen` | `GOSHISEN_QUEUE_MAXLEN` | `1024` |
| `log_level` | `--log-level` | `GOSHISEN_LOG_LEVEL` | `info` |
| `log_json` | `--log-json` | `GOSHISEN_LOG_JSON` | `false` |

Boolean env values accept `true/false`, `1/0`, `yes/no`, `on/off`.

## Documentation

| Document | Contents |
| --- | --- |
| [Architecture](docs/design/architecture.md) | Full design: packet flow, policy schema, rule semantics, failure modes, lab topology, library risks |
| [MVP Scope](docs/design/mvp-scope.md) | Component inventory, scenarios S1–S12, per-phase acceptance criteria, non-goals |
| [User Guide](docs/user-guide.md) | CLI, configuration, examples |
| [Developer Guide](docs/developer-guide.md) | Build, test, CI, repository layout |

## Repository layout

```
cmd/goshisen/        daemon entry point
internal/app/        daemon lifecycle: policy load, hook install, queue loop, shutdown
internal/cli/        flag parsing, help/version, subcommand resolution
internal/config/     configuration model, YAML file, precedence, validation
internal/policy/     policy YAML parser + evaluation engine (snapshots, atomic replace)
internal/packet/     NFQUEUE backend + IPv4/TCP/UDP parser
internal/hook/       nftables hook provisioning (router namespace only)
configs/             sample configuration
policies/default/    deny-all policy example
policies/examples/   worked policy example
scripts/lab.sh       isolated netns lab (up/down/status)
tests/integration/   real-traffic integration suite + probe helper
docs/                design and user/developer guides
.github/workflows/   CI
```

## License

MIT — see [LICENSE](LICENSE). © 2026 Lê Thanh Phong.
