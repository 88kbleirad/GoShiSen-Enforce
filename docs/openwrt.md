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
(r2 does not ship the guard); remove them with
`/etc/init.d/goshisen-boot disable` before downgrading.

## Install

```sh
# from the package artifact (signature checks: see "Signing" below)
wget -O /tmp/goshisen.apk https://example/goshisen-0.2.0-r1.apk
wget -O /tmp/goshisen.apk.sig https://example/goshisen-0.2.0-r1.apk.sig
wget -O /tmp/goshisen-release.pub.pem \
  https://raw.githubusercontent.com/88kbleirad/GoShiSen-Enforce/main/keys/goshisen-release.pub.pem
openssl pkeyutl -verify -rawin -pubin -inkey /tmp/goshisen-release.pub.pem \
  -in /tmp/goshisen.apk -sigfile /tmp/goshisen.apk.sig
apk add --allow-untrusted /tmp/goshisen.apk
```

**Signature verification is mandatory** — the `apk add` above must only be run
after `openssl ... -verify` prints `Signature Verified Successfully`. The
`--allow-untrusted` flag is a **transition exception** (see "Signing" below)
and is never a substitute for the Ed25519 check; it will be removed once native
`usign` apk verification ships.

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
| `offload_policy` | `warn` | flow-offload response: `warn` (log WARN + gauge), `refuse` (startup fails before hook install when offloading is detected), `readiness` (enforce but never become ready while offloading is active), `enforce` (actively disable offloading and verify; fail closed if disable fails or state is unknown) |
| `allow_bypass` | `0` | diagnostic/development escape hatch, **only** valid with `offload_policy=enforce`: `1` downgrades enforce to warn (loud warning). Never default, never silent |

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
| Flow offloading | **detected and warned by default**: established flows bypass the queue. The daemon logs a WARN and exposes `goshisen_flow_offload_state` (0 disabled, 1 enabled, 2 unknown), re-checked every 10 s. `offload_policy` controls the response: `refuse` fails startup before the hook is installed (fail-closed by absence), `readiness` keeps enforcing but never becomes ready while offloading is active, `enforce` actively disables the offload and verifies it, failing closed if the disable fails or state is unknown (unless the explicit `allow_bypass` diagnostic hatch is set). Recommended: keep `flow_offloading` disabled on routers that rely on GoShiSen enforcement |

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

Release `.apk` artifacts are signed with the release Ed25519 key (same key as
the release archives; signatures attach as `<artifact>.sig`, verified with the
committed public key `keys/goshisen-release.pub.pem`). Native `usign` apk
verification (key under `/etc/apk/keys/`) is a follow-up; until it ships, install
the apk in two steps:

1. **Verify the signature first** (mandatory, see Install above):

   ```sh
   openssl pkeyutl -verify -rawin -pubin -inkey goshisen-release.pub.pem \
     -in goshisen.apk -sigfile goshisen.apk.sig
   # must print: Signature Verified Successfully
   ```

2. `apk add --allow-untrusted goshisen.apk` — **transition exception**:
   `--allow-untrusted` skips apk's *own* trust check, so the Ed25519
   verification in step 1 is REQUIRED and MUST have succeeded first. The flag
   stays only until native `usign` verification lands (backlog #1) and will be
   removed then; there is no silent fallback — the install fails (human
   interrupt) if step 1 fails. Do not install an artifact whose signature you
   have not verified, even with this exception.

The public key fingerprint (SHA-256 of DER SPKI) is
`24215e016621925215dec0ad6ea14ac74253a4e97249a973e89d649a2217cca0`
(key created 2026-11-13; verify it out-of-band).

## Verification commands

```sh
nft list chain inet goshisen forward        # hook at priority filter + 1
cat /proc/net/netfilter/nfnetlink_queue     # queues 100 (v4) and 101 (v6)
logread | grep goshisen                     # startup, reload, warnings
wget -qO- http://127.0.0.1:9090/metrics | grep goshisen_   # counters
```