# GoShiSen-Enforce — User Guide

Status: **Phase 7 (hardening/release).** This guide describes the daemon as built: CLI, configuration, lifecycle, policy engine, stateful filtering, REST API, metrics, NAT, and queue-overflow monitoring.

## Building

```bash
make build
```

Produces `bin/goshisen`. The binary's version comes from `git describe` (fallback `dev`); override with `make build VERSION=1.2.3`.

## Command line

```
goshisen [flags] [command]
```

| Flag | Description |
| --- | --- |
| `-h`, `--help` | print help and exit |
| `-v`, `--version` | print version and exit |
| `-c`, `--config <path>` | YAML configuration file |
| `-i`, `--interface <name>` | interface to use (overrides file/env) |
| `--policy-file <path>` | path to the YAML policy file (overrides file/env) |
| `--log-level <lvl>` | `debug` \| `info` \| `warn` \| `error` |
| `--log-json` | emit structured JSON logs |
| `--queue-maxlen <n>` | kernel NFQUEUE queue length in packets (default 1024; range 1–65535) |

Commands:

| Command | Behavior |
| --- | --- |
| `config show` | prints the resolved configuration, with the precedence source (`default`/`file`/`env`/`flag`) of each value |
| `config check` | validates configuration and host read-only: policy file readable, interface present and up. Makes no system changes; exit code 0 on success |
| `policy validate --policy-file <path>` | compiles the policy file, rejects invalid rules, prints a summary of the compiled rules. Makes no system changes |
| `policy simulate --policy-file <path> --src <ip> --dst <ip> [--protocol tcp\|udp\|any] [--source-port <n>] [--dest-port <n>]` | evaluates a synthetic packet against the policy and prints the verdict and matching rule ID (or `(default)`). Makes no system changes |
| `hook status` | reports whether the application-owned nftables table (`ip goshisen`) is installed. Read-only; makes no changes |
| `hook cleanup` | deletes only the application-owned nftables table (`ip goshisen`). Requires root (CAP_NET_ADMIN); never touches any other nftables state |
| *(none)* | run the daemon |

Flags must precede the command: `goshisen -c f.yaml config check`. Flags that belong to a subcommand go **after** the subcommand: `goshisen policy simulate --src 1.2.3.4 --dst 5.6.7.8`.

## Configuration

Sample: `configs/goshisen.yaml`.

```yaml
interface: eth0          # empty/omitted = auto (first up, non-loopback)
policy_file: policies/default/deny-all.yaml   # required
log_level: info          # debug | info | warn | error
log_json: false          # true = JSON logs, false = text
queue_maxlen: 1024       # kernel NFQUEUE queue length (1..65535)
```

Unknown keys are rejected at load time. `~` in `policy_file` is expanded to the user's home directory.

### Environment variables

All use the `GOSHISEN_` prefix; they override file values but not flags.

| Variable | Equivalent flag |
| --- | --- |
| `GOSHISEN_INTERFACE` | `--interface` |
| `GOSHISEN_POLICY_FILE` | `--policy-file` |
| `GOSHISEN_LOG_LEVEL` | `--log-level` |
| `GOSHISEN_LOG_JSON` | `--log-json` |
| `GOSHISEN_QUEUE_MAXLEN` | `--queue-maxlen` |

Boolean values accept `true/false`, `1/0`, `yes/no`, `on/off`. An invalid env value is a startup error.

### Precedence

```
built-in defaults < config file < environment < flags
```

`config show` is the authoritative way to see the effective configuration and where each value came from.

## Running the daemon

The daemon enforces traffic in the **current network namespace**. In normal use it runs inside the lab router namespace (`gsh_fw`), so all nftables changes stay isolated:

```bash
sudo scripts/lab.sh up
sudo ip netns exec gsh_fw ./bin/goshisen \
  --interface gsh-fw1 --policy-file policies/examples/allow-lab.yaml
sudo scripts/lab.sh down
```

- Startup order: resolve config → compile policy (invalid policy aborts before any change) → install the nftables hook (router namespace only) → bind NFQUEUE → enforce.
- Every queued packet receives a verdict. Parse failures, engine errors, and panics fail closed (DROP).
- `SIGINT`/`SIGTERM` stop the queue and exit gracefully (exit code 0). The hook stays in place — with no listener, the kernel drops (fail-closed) until the daemon returns.
- **The host firewall is never touched.** The hook touches only `table ip goshisen` in the current namespace; teardown deletes the namespaces and everything inside them.

Integration tests exercise the same path with real traffic; they require root:

```bash
sudo make integration
```

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | clean exit (help/version/check ok/graceful shutdown) |
| 1 | configuration or host validation failure, or runtime error |
| 2 | CLI usage error (unknown flag/command) |

## NAT

Configured under `nat:` in the daemon config (kernel-side nftables; no address/checksum rewriting in Go):

```yaml
nat:
  enabled: true
  masquerade:
    enabled: true
    out_iface: eth1
  dnat:
    - name: web
      protocol: tcp
      pub_ip: 203.0.113.10
      pub_port: 80
      target_ip: 10.0.0.10
      target_port: 8080
```

- The whole ruleset (filter + NAT) is applied as one transaction; a failed apply changes nothing, and only the app-owned `table ip goshisen` is touched.
- **Policy evaluation sees post-DNAT destination** (prerouting runs before the filter hook) and **pre-MASQUERADE source** (postrouting runs after). Rules must match the internal target addresses.
- Return traffic is un-NATed by conntrack and handled by ESTABLISHED/RELATED rules.

## REST API

The daemon exposes a management API (default `127.0.0.1:8080`). See `api/openapi.yaml` for the full spec.

| Endpoint | Auth | Purpose |
| --- | --- | --- |
| `GET /v1/policy` | — | view the active policy |
| `GET /v1/policy/versions` | — | list stored versions + active |
| `POST /v1/policy/validate` | bearer | compile-check a policy without applying |
| `POST /v1/policy` | bearer | store + activate a new version |
| `POST /v1/policy/versions/{v}/activate` | bearer | activate a stored version (rollback) |
| `GET /healthz` | — | liveness |
| `GET /readyz` | — | readiness (firewall fully up) |
| `GET /metrics` | — | Prometheus metrics |

- **Auth:** set `api_token` (config) or `GOSHISEN_API_TOKEN`. It is never accepted as a CLI flag. With no token set, administrative operations are refused (401).
- **Concurrency:** applying a policy requires `base_version` equal to the current active version; otherwise `409 version_conflict`. Concurrent writers therefore cannot silently overwrite each other.
- **Invalid policies:** rejected with `422 invalid_policy` before any state change; the active policy is preserved.
- **Durability:** policies are stored as immutable versioned files with an atomically-updated active pointer; restart recovery reloads the active version. `policy_dir` configures the store.
- **Audit:** every admin action is logged (action, outcome, version); tokens are never logged.

Example:

```bash
TOKEN=$(grep api_token configs/goshisen.yaml | awk '{print $2}' | tr -d '"')
# Validate first:
curl -s -X POST -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"policy\": \"$(cat policies/examples/allow-lab.yaml | tr '\n' ' ')\"}" \
  http://127.0.0.1:8080/v1/policy/validate
# Apply (version 1):
curl -s -X POST -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"policy\": \"$(cat policies/examples/allow-lab.yaml | tr '\n' ' ')\", \"base_version\": \"\"}" \
  http://127.0.0.1:8080/v1/policy
```

## Metrics

Prometheus text metrics at `GET /metrics` (API) and on `metrics_port` (default 9090): packet verdicts by action/protocol/state, backend errors, verdict-latency histogram, active policy version. Labels are bounded enums — no IPs, ports, or flow IDs.

## Queue overflow monitoring

When the kernel's NFQUEUE queue fills up (or the daemon's netlink socket cannot drain it), the kernel drops the excess packets — **it never accepts them**: the daemon never sets the kernel's fail-open flag. Dropped packets therefore never bypass the firewall.

The daemon polls `/proc/net/netfilter/nfnetlink_queue` (the queue lives in the daemon's network namespace) once per second and:

- records `goshisen_queue_overflow_total{cause="kernel_drop"}` (queue-full drops) and `{cause="user_drop"}` (netlink unicast failures);
- logs a `WARN` line with the drop counters (rate-limited to one per 10 s).

Queue statistics are only available when the kernel exposes the proc file; otherwise monitoring is disabled quietly. `queue_maxlen` configures the queue depth (smaller spills sooner — useful for validation/testing).

## Policy files

The policy YAML schema and semantics are specified in `docs/design/architecture.md` §4–§5. Current matchers:

- `id` (required, unique), `action` (`ACCEPT`/`DROP`), `protocol` (`tcp`/`udp`/`any`), `src`/`dst` (IPv4 CIDR, bare address, or `any`), `src_port`/`dst_port` (single port or inclusive `lo:hi`, only with an explicit protocol), `state` (`NEW`, `ESTABLISHED`, `RELATED`, `INVALID`), `log`, `description`.
- Rules are ordered; the first match decides. If nothing matches, `default_action` applies (default `DROP`).
- **Stateful semantics:** the kernel's conntrack metadata decides a packet's state and direction. `NEW` packets are the acceptance event; `ESTABLISHED`/`RELATED` only match connections the policy already accepted (an accepted-tracking set records them). Reply/smuggling traffic for unaccepted connections is dropped even if the tuple matches.
- Unknown fields, duplicate IDs, malformed CIDRs/ports, invalid states, and port matchers without a protocol are compile errors. `in_iface`/`out_iface` remain reserved and rejected.
- Check a policy without applying it: `goshisen policy validate --policy-file policies/examples/allow-lab.yaml`.
- Simulate with state: `goshisen policy simulate --state established ...` (default: stateless).

## Examples

```bash
# Validate without running:
goshisen -c configs/goshisen.yaml config check

# Use flags alone (no config file), leaving log level at default:
goshisen --interface eth0 --policy-file policies/default/deny-all.yaml

# Environment + file mix:
GOSHISEN_LOG_LEVEL=debug GOSHISEN_LOG_JSON=true \
  goshisen -c configs/goshisen.yaml

# Inspect effective config and sources:
goshisen -c configs/goshisen.yaml config show

# Validate a policy and see the compiled rules:
goshisen policy validate --policy-file policies/examples/allow-lab.yaml

# Simulate a TCP packet to port 80:
goshisen policy simulate --policy-file policies/examples/allow-lab.yaml \
  --protocol tcp --src 10.0.0.5 --dst 10.0.0.2 --dest-port 80
#   verdict: ACCEPT
#   matched rule: allow-http

# Simulate a packet no rule matches (default action):
goshisen policy simulate --policy-file policies/examples/allow-lab.yaml \
  --protocol udp --src 10.0.0.5 --dst 10.0.0.2 --dest-port 9999
#   verdict: DROP
#   matched rule: (default)
```
