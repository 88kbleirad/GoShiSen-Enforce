# GoShiSen-Enforce

**A Linux lab firewall: Go policy engine over NFQUEUE, kernel conntrack, nftables hooks.**

> **Status: Phase 6 — NAT + operational packaging complete.** The foundation, policy engine, packet path, stateful filtering, management API/metrics, and now kernel-side NAT with container packaging (Phase 6) are implemented and unit-tested. NAT (DNAT + MASQUERADE) runs entirely in nftables — no addresses or checksums are rewritten in Go — applied atomically in the app-owned table only. All privileged integration tests (now 17 scenarios, including NAT) remain root-gated and were **not runnable in this environment** (non-root).

## What works today

- **NAT** (`internal/hook`, kernel nftables): DNAT (`dstnat` prerouting chain) and MASQUERADE (`srcnat` postrouting chain) configured under `nat:` in the daemon config. Applied as one `nft -f` transaction; failed apply changes nothing; only `table ip goshisen` is ever touched. Documented semantics: **the policy engine sees post-DNAT destination and pre-MASQUERADE source** (see `docs/design/architecture.md` §8.2).
- **Packaging**: multi-stage `deployments/docker/Dockerfile` (Go builder + static nftables builder → distroless base), explicit `NET_ADMIN`/`NET_RAW` capabilities, **never privileged**, graceful TERM shutdown, health-check guidance (`/healthz`, `/readyz`), durable policy volume, restart/recovery notes. Operator cleanup: `goshisen hook status|cleanup` (app-owned table only). No install scripts (would need reliable uninstall/rollback first) and no Kubernetes manifests (CNI relationship must be defined first) — both documented as out of scope.
- Phase 1–5 unchanged: policy engine, NFQUEUE enforcement, stateful filtering, REST API, metrics, durable store, reload.

## Integration tests & privileges

The lab needs root (CAP_NET_ADMIN). As a non-root user, `go test ./...` compiles the suite and skips it with the reason `integration tests require root for CAP_NET_ADMIN`. Run it with:

```bash
sudo make integration
```

This environment is unprivileged, so the 18 scenarios (Phase 3: traffic pass/drop, default DROP, no-listener, clean shutdown; Phase 4: handshake+echo, unsolicited inbound, invalid state, timeout/hold, UDP re-use, reload-while-established; Phase 6: DNAT, MASQUERADE, return traffic, NAT+filtering, reload-while-NAT-connections, hook cleanup scope) were **not executed here**; the code, lab script, and probes are real — no mocks, no faked verdicts.

## Quick start

```bash
make build                      # produces bin/goshisen (version from git describe)

# Validate a config without starting the daemon:
./bin/goshisen -c configs/goshisen.yaml config check

# Validate a policy and inspect the compiled rules:
./bin/goshisen policy validate --policy-file policies/examples/allow-lab.yaml

# Simulate a packet — verdict + matching rule ID, no network changes:
./bin/goshisen policy simulate --policy-file policies/examples/allow-lab.yaml \
  --protocol tcp --src 10.0.0.5 --dst 10.0.0.2 --dest-port 80
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
