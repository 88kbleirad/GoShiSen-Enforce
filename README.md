# GoShiSen-Enforce

**A Linux lab firewall: Go policy engine over NFQUEUE, kernel conntrack, nftables hooks.**

> **Status:** latest release and known limitations on [Releases](https://github.com/88kbleirad/GoShiSen-Enforce/releases) and in `docs/release/`. Release-tag signing (SSH `allowed_signers`) is a goal pending maintainer key configuration — see `docs/installation.md` → Verifying release tags.

## Features

- **Fail-closed by design**: the NFQUEUE backend never sets `NFQA_CFG_F_FAIL_OPEN`, so queue overflow or a missing listener never bypasses the firewall.
- **Policy engine + stateful filtering** over NFQUEUE and kernel conntrack. Atomic, durable policy publishes (SIGHUP and REST API) and `policy dry-run` / `policy simulate` previews.
- **NAT** via nftables (DNAT and MASQUERADE under `nat:` in the config), applied as a single `nft -f` transaction that touches only `table ip goshisen`. The policy engine sees the post-DNAT destination and pre-MASQUERADE source (`docs/design/architecture.md` §8.2).
- **Queue overflow monitoring**: `goshisen_queue_overflow_total{cause}` metric plus rate-limited warnings. Tune depth with `queue_maxlen` (default 1024).
- **Allocation-free hot path** (0 allocs/packet). Benchmarks: `docs/design/performance.md`.
- **Reproducible packaging**: static `linux/amd64` and `linux/arm64` archives with `checksums.txt` and an SPDX SBOM (`make package`). The release workflow only creates draft releases. See `docs/installation.md`.

## OpenWrt

The `openwrt/` SDK-built `.apk` adds a fail-closed NFQUEUE enforcement layer after firewall4 on the forward path; it never replaces firewall4, never modifies fw4's tables, and never NATs. Install, UCI options, lifecycle, recovery and upgrade (incl. the upgrade-hook fix, issue #57): `docs/openwrt.md`. CLI binaries, upgrade and rollback: `docs/installation.md`.

## Quick start

```bash
make build                      # produces bin/goshisen

# Validate config and policy
./bin/goshisen -c configs/goshisen.yaml config check
./bin/goshisen policy validate --policy-file policies/examples/allow-lab.yaml

# Simulate a packet (no network changes)
./bin/goshisen policy simulate --policy-file policies/examples/allow-lab.yaml \
  --protocol tcp --state new --src 10.0.0.5 --dst 10.0.0.2 --dest-port 80
#   verdict: ACCEPT
#   matched rule: allow-http

# Run in the lab router namespace (root); the host firewall is untouched
sudo scripts/lab.sh up
sudo ip netns exec gsh_fw ./bin/goshisen \
  --interface gsh-fw1 --policy-file policies/examples/allow-lab.yaml
sudo scripts/lab.sh down
```

## Configuration

Precedence (lowest to highest): defaults, config file, environment, flags.

| Key | Flag | Environment | Default |
| --- | --- | --- | --- |
| `interface` | `--interface` | `GOSHISEN_INTERFACE` | auto (first up, non-loopback) |
| `policy_file` | `--policy-file` | `GOSHISEN_POLICY_FILE` | *(required)* |
| `queue_maxlen` | `--queue-maxlen` | `GOSHISEN_QUEUE_MAXLEN` | `1024` |
| `log_level` | `--log-level` | `GOSHISEN_LOG_LEVEL` | `info` |
| `log_json` | `--log-json` | `GOSHISEN_LOG_JSON` | `false` |

Booleans accept `true/false`, `1/0`, `yes/no`, `on/off`. Samples: `configs/goshisen.yaml`, `policies/`.

## Metrics

Prometheus text format 0.0.4: `GET /metrics` on the API server (`127.0.0.1:8080`) or a dedicated `--metrics-port` (default 9090, localhost only, no auth). Bounded-cardinality labels only. Metric inventory and Grafana setup: `docs/release/metrics-grafana-validation.md`.

## Testing

```bash
go test ./...            # unprivileged; integration suite is skipped without root
sudo make integration    # real netns scenarios, needs root (CAP_NET_ADMIN)
```

CI runs the full integration suite on GitHub-hosted runners.

## Documentation

| Document | Contents |
| --- | --- |
| [Architecture](docs/design/architecture.md) | Packet flow, policy schema, failure modes, lab topology |
| [MVP Scope](docs/design/mvp-scope.md) | Components, scenarios, acceptance criteria, non-goals |
| [User Guide](docs/user-guide.md) | CLI, configuration, examples |
| [Developer Guide](docs/developer-guide.md) | Build, test, CI, repository layout |
| [Installation](docs/installation.md) | CLI binaries, upgrade, rollback |
| [OpenWrt](docs/openwrt.md) | Package install, UCI, procd, recovery |

## Documentation site (`docs-site/`)

A static, dependency-free site for browsing these docs, with Catppuccin
light/dark themes and an English/Vietnamese language switch.

```bash
python3 -m http.server 8901          # serve the repo root, then open:
# http://127.0.0.1:8901/docs-site/index.html
```

- No build step, no CDN: markdown rendering is vendored (`docs-site/vendor/md.js`);
  the site works from `file://` and any static host (including GitHub Pages).
- UI chrome is bilingual (`docs-site/i18n/{en,vi}.json`); doc content comes from
  the repo markdown, with Vietnamese translations mirrored under
  `docs-site/translations/vi/` (same relative path). Missing translations fall
  back to English with a notice.
- The sidebar manifest (`docs-site/manifest.json`) is generated from the repo;
  regenerate it after adding/renaming docs (see
  `docs-site/README.md` for the exact command).

## License

MIT, see [LICENSE](LICENSE). © 2026 Lê Thanh Phong.
