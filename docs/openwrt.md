# OpenWrt Integration

GoShiSen-Enforce adds a fail-closed NFQUEUE enforcement layer **after**
firewall4 on the forward path. It never replaces firewall4, never
modifies fw4's tables, and never NATs.

Status: validated on OpenWrt 25.12.6 (x86/64, kernel 6.12.112) in QEMU.
Only x86_64 is verified; other architectures are untested.

Release r4 (0.2.0-r4, pin `f5f211a`) is verified end-to-end: upgrade
from v0.2.0-r2, old-config compatibility, boot guard (S12) ordering,
flow-offload policy modes (warn/refuse/readiness), stateful IPv6
(allow/deny/established replies with real v6 peers), reload-failure
atomicity, crash/restart recovery, and rollback to r2. See
`docs/release/phase16-openwrt-compat.md`. Note: rolling back from r4 to
r2 leaves dangling `S12goshisen-boot`/`K10goshisen-boot` rc.d symlinks
(r2 does not ship the guard). **Before downgrading**, disable the guard
so its rc.d links are removed while the script still exists:

```sh
/etc/init.d/goshisen-boot disable   # removes S12/K10goshisen-boot
apk add --allow-untrusted goshisen-0.2.0-r2.apk
```

apk does not run the *old* package's pre-deinstall on a downgrade, so
the cleanup cannot be automated from the r4 side; the pre-downgrade
`disable` is the supported, verified path. If the downgrade was done
without it, remove the two dangling links directly (fixed paths, no
recursive deletion):

```sh
unlink /etc/rc.d/S12goshisen-boot /etc/rc.d/K10goshisen-boot
```

Uninstalling (`apk del goshisen`) is safe: the package prerm removes all
four rc.d links (verified).

## Install

```sh
# from the package artifact (signature checks: see "Signing" below)
wget -O /tmp/goshisen.apk https://example/goshisen-0.2.0-r1.apk
apk add --allow-untrusted /tmp/goshisen.apk
```

The package installs:

| Path | Purpose |
| --- | --- |
| `/usr/sbin/goshisen` | static daemon (CGO off) |
| `/etc/init.d/goshisen` | procd init script |
| `/etc/config/goshisen` | UCI configuration (disabled by default) |
| `/etc/goshisen/policy.yaml` | fail-closed default policy (`DROP`, no rules) |

Dependencies: `kmod-nfnetlink-queue`, `kmod-nft-queue`,
`kmod-nf-conntrack`, `kmod-nf-conntrack-netlink`, `nftables`.

**The service is disabled by default.** Installing the package does not
enable it and does not touch the firewall.

## UCI configuration (`/etc/config/goshisen`)

| Option | Default | Meaning |
| --- | --- | --- |
| `enabled` | `0` | master switch; the daemon only starts when `1` |
| `queue_num` | `100` | NFQUEUE number for IPv4 (IPv6 uses `queue_num+1`) |
| `hook_scope` | `openwrt` | `openwrt` = hook after fw4 (priority `filter + 1`); `forward-all` = standalone forward drop hook |
| `log_level` | `info` | `debug`, `info`, `warn`, `error` |
| `interface` | `br-lan` | kernel netdev used for host validation (a fw4 zone name like `lan` is rejected) |
| `policy_file` | `/etc/goshisen/policy.yaml` | policy YAML |
| `api_port` | `0` | optional REST API (loopback only) |
| `metrics_port` | `0` | optional Prometheus metrics (loopback only) |
| `queue_maxlen` | `1024` | kernel queue length |
| `offload_policy` | `warn` | flow-offload response: `warn` (log WARN + gauge), `refuse` (startup fails before hook install when offloading is detected), `readiness` (enforce but never become ready while offloading is active) |

Enable and start:

```sh
uci set goshisen.main.enabled='1'
uci commit goshisen
/etc/init.d/goshisen enable
/etc/init.d/goshisen start
```

Configure explicit ACCEPT rules **before** enabling: the shipped policy
drops everything.

## procd behavior

- `service goshisen start|stop|restart|reload|status` (or the init
  script) are the supported operations.
- `reload` sends SIGHUP: the new policy is compiled first and only then
  swapped; a policy that fails to compile keeps the previous policy and
  logs `policy reload rejected`.
- `uci commit` does **not** restart the service (no restart loop).
- Under procd, the daemon's stdout/stderr go to syslog (`logread`).
- `respawn` uses procd defaults (`threshold 0` = unlimited retries,
  `timeout 5`, `retry 5`): a crashed daemon is restarted automatically.

Boot behavior: the init script starts at `START=90` (after firewall).
The daemon installs the hook **before** binding the queue, so the
window between hook install and queue bind is fail-closed (queued
packets have no listener and the kernel drops them). Before the daemon
starts at all, only firewall4's own policy applies — the gap is bounded
by the late start order.

## Enforcement scope and gaps

| Item | Behavior |
| --- | --- |
| IPv4 forwarded TCP/UDP | queued (queue `100`), policy decides |
| IPv6 forwarded TCP/UDP | queued (queue `101`; `inet` table). Requires the daemon's AF_INET6 bind, otherwise the kernel drops (fail-closed) |
| Router-local traffic (SSH, LuCI, DNS, DHCP) | never queued: the hook is `forward` only |
| Non-TCP/UDP | not queued; firewall4 decides |
| Flow offloading | **detected and warned by default**: established flows bypass the queue. The daemon logs a WARN and exposes `goshisen_flow_offload_active 1`, re-checked every 10 s. `offload_policy` controls the response: `refuse` fails startup before the hook is installed (fail-closed by absence), `readiness` keeps enforcing but never becomes ready while offloading is active. Recommended: keep `flow_offloading` disabled on routers that rely on GoShiSen enforcement |

Stateful IPv6 note: on kernels whose `nfnetlink_queue` build does not
deliver conntrack attributes for IPv6, IPv6 packets arrive as INVALID
and are dropped (fail-closed, never allowed). Stateless IPv6 rules
(src/dst/proto/port without `state`) work. Verify with
`goshisen_packets_by_state_total{state="invalid"}`.

## Recovery (LAN→WAN blocked)

The hook is intentionally left in place when the daemon stops, so
forwarding stays blocked (fail-closed) rather than silently allowed.

Recovery options, in order:

```sh
# 1. Start/restart the service (preferred)
/etc/init.d/goshisen start

# 2. Remove the hook so firewall4 alone governs forwarding
goshisen hook cleanup

# 3. Disable the service permanently
uci set goshisen.main.enabled='0'; uci commit goshisen
/etc/init.d/goshisen stop
/etc/init.d/goshisen disable
```

Router-local access (serial console, SSH to the router, LuCI) is never
blocked by the forward hook, so these commands remain reachable.

## Upgrade and removal

- **Upgrade** (`apk add` a newer rN): the running daemon keeps its
  in-memory policy; the queue binding and hook stay in place, so there
  is no fail-open window. Restart the service to run the new binary.
  User config (`/etc/config/goshisen`, `/etc/goshisen/policy.yaml`) is
  preserved as a conffile; the package writes `.apk-new` files when the
  shipped default differs.
- **Removal** (`apk del goshisen`): `prerm` runs `goshisen hook cleanup`
  and stops the service, so the app-owned table is deleted and
  forwarding is not left blocked by an orphaned queue rule.

## Signing

Release `.apk` artifacts are built by the SDK and are **not signed with
a key trusted by a stock OpenWrt** unless you add one. Two options:

1. Add the release public key to `/etc/apk/keys/` (recommended; then
   `apk add` works without `--allow-untrusted`).
2. Install with `--allow-untrusted`. This skips signature verification:
   anyone who can serve the package can install arbitrary code. Only do
   this with an artifact whose sha256 you verified out of band.

## Verification commands

```sh
nft list chain inet goshisen forward        # hook at priority filter + 1
cat /proc/net/netfilter/nfnetlink_queue     # queues 100 (v4) and 101 (v6)
logread | grep goshisen                     # startup, reload, warnings
wget -qO- http://127.0.0.1:9090/metrics | grep goshisen_   # counters
```