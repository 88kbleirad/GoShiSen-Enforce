# OpenWrt Integration

GoShiSen-Enforce adds a fail-closed NFQUEUE enforcement layer **after**
firewall4 on the forward path. It never replaces firewall4, never
modifies fw4's tables, and never NATs.

Status: validated on OpenWrt 25.12.6 (x86/64, kernel 6.12.112) in QEMU.
x86_64 runtime is fully verified; aarch64 (armsr/armv8) is built and
APK-verified but its live QEMU runtime lab is environment-blocked (see
`docs/release/openwrt-v022-package-validation.md` §6); other
architectures are untested.

Release **r5 (0.2.0-r5, pin `c5de6e8` = v0.2.2)** is verified end-to-end:
fresh install, default-disabled, enable/start, queues 100+101,
DROP/ACCEPT controls, offload `enforce` disable (verified), lifecycle
(stop/start/restart/reload), kill -9 fail-closed + respawn, reboot with
boot guard, upgrade r4→r5 (config preserved), downgrade r5→r4 (no
dangling guard links), uninstall cleanup — on x86_64 and aarch64
(live armsr/armv8 QEMU, see `docs/release/arm64-runtime-validation.md`).
See `docs/release/openwrt-v022-package-validation.md`.

Downgrade r5→r4 caveat: the r4 daemon (v0.2.1-era binary, pin
`f5f211a`) does not understand the `allow_bypass` YAML field that the r5
init script renders. After downgrading, **restart the service once**
(`/etc/init.d/goshisen restart`) so the r4 init re-renders the runtime
config; if a stale r5-rendered `/var/run/goshisen/config.yaml` is read
first, the r4 daemon refuses to start (remove that file as a clean
alternative). The downgrade path verified in the report uses this
procedure.

Release r4 (0.2.0-r4, pin `f5f211a`) was verified end-to-end earlier:
upgrade from v0.2.0-r2, old-config compatibility, boot guard (S12)
ordering, flow-offload policy modes (warn/refuse/readiness), stateful
IPv6 (allow/deny/established replies with real v6 peers), reload-failure
atomicity, crash/restart recovery, and rollback to r2. See
`docs/release/openwrt-compat.md`. Note: rolling back from r4 to
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

## Install (signed package, v0.2.3+)

Since v0.2.3 the OpenWrt package (`goshisen-0.2.3-r1.apk`) is
**production-signed** (apk v3). Install the public anchor and the
package without `--allow-untrusted`:

```sh
# on the router/VM (as root)
mkdir -p /etc/apk/keys
wget -O /etc/apk/keys/goshisen-apk.pub.pem \
  https://raw.githubusercontent.com/88kbleirad/GoShiSen-Enforce/v0.2.3/keys/goshisen-apk.pub.pem
openssl pkey -pubin -in /etc/apk/keys/goshisen-apk.pub.pem -outform DER | sha256sum
# must print: 542bb7ae9058fee9b4d37ed30b0af44f234ea7cb8866daa1ffc71a7bc37c839d
wget -O /tmp/goshisen.apk \
  https://github.com/88kbleirad/GoShiSen-Enforce/releases/download/v0.2.3/goshisen-0.2.3-r1.apk
sha256sum /tmp/goshisen.apk    # 948e2e1de454c159bed81bfe845bad160dcf9454238b532b2a87f2e335b0cc6a
apk add /tmp/goshisen.apk       # signature verified by apk; no --allow-untrusted
```

The APK anchor is an EC P-256 public key
(`keys/goshisen-apk.pub.pem`, fingerprint `542bb7ae...`) and is **not**
the CLI release key (the `public_key.pem` release asset, Ed25519,
fingerprint `e9b4bf11...` since the 2026-11-28 rotation, verifies the
tarballs/checksums/SBOM only; releases up to v0.2.9 are signed with the
pre-rotation key `24215e01...`, archived as
`keys/goshisen-release-2026-11-13.pub.pem`).
Package installs disabled by default; enable with `uci set
goshisen.main.enabled=1; uci commit goshisen; /etc/init.d/goshisen
enable && start`.

APK signature verification is enforced by the package manager: without
the anchor, `apk add` of the signed package fails closed (UNTRUSTED).
The old `--allow-untrusted` transition path was removed with the
signing release (v0.2.3).

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

**Listener defaults (measured, not assumed).** The daemon's built-in
defaults are API `127.0.0.1:8080` and metrics `127.0.0.1:9090`, and
they are **on** whenever the daemon runs: measured on OpenWrt 25.12.6
x86/64 (kernel 6.12.112) for a fresh install of the signed 0.2.5-r1 and
the 0.2.6-r1 apk with the packaged config untouched, and for an upgrade
0.2.3-r1 → 0.2.6-r1 while running — in every case `netstat -ltnp` shows
`127.0.0.1:8080` and `127.0.0.1:9090` owned by `goshisen` (GET
`/healthz` → 200, GET `/metrics` → 200, `POST /v1/policy` without a
token → 401). Releases ≤ v0.2.6 **ignore** the packaged
`option api_port '0'` / `option metrics_port '0'` (the file layer
treated `0` as "not set"), so the UCI option cannot turn the listeners
off there; use `GOSHISEN_API_PORT=0` / `GOSHISEN_METRICS_PORT=0` in the
service environment, or leave `goshisen.main.enabled=0`. A config
without those options (or with them empty) makes the init script abort
before starting the daemon (`goshisen: api_port '' out of range`), so
those cases end with no listener at all. Full evidence:
`docs/security/advisory-v0.2.5-go-toolchain.md` § "Measured listener
defaults".

## UCI configuration (`/etc/config/goshisen`)

| Option | Default | Meaning |
| --- | --- | --- |
| `enabled` | `0` | master switch; the daemon only starts when `1` |
| `queue_num` | `100` | NFQUEUE number for IPv4 (IPv6 uses `queue_num+1`) |
| `hook_scope` | `openwrt` | `openwrt` = hook after fw4 (priority `filter + 1`); `forward-all` = standalone forward drop hook |
| `log_level` | `info` | `debug`, `info`, `warn`, `error` |
| `interface` | `br-lan` | kernel netdev used for host validation (a fw4 zone name like `lan` is rejected) |
| `policy_file` | `/etc/goshisen/policy.yaml` | policy YAML |
| `api_host` | `127.0.0.1` | management API bind host; loopback only (`127.0.0.1`, any `127.x`, `::1`, `localhost`). The daemon refuses to start with a non-loopback host unless `allow_non_loopback_api_host` is enabled (see below). |
| `api_port` | `0` | REST API (loopback only). An explicit `0` — this option, `GOSHISEN_API_PORT=0`, or `--api-port 0` — disables the API listener; an unset value keeps the built-in default 8080. The init script renders the option explicitly and treats an absent/empty option as `0` (disabled) with a logread warning; an invalid value aborts the start naming the option. ⚠️ Releases ≤ v0.2.6 ignore a file/flag `0` (falls back to 8080); there only `GOSHISEN_API_PORT=0` / `enabled=0` disable. |
| `metrics_port` | `0` | Prometheus metrics (loopback only). Same explicit-`0`/absent-empty semantics as `api_port`; unset keeps the built-in default 9090. ⚠️ Same ≤ v0.2.6 caveat as `api_port`. |
| `allow_non_loopback_api_host` | unset (`0`) | opt-in that permits a non-loopback `api_host`. Enabling it exposes the API GET endpoints (unauthenticated) to the network — keep it off unless the network path is trusted and firewalled. Same knob as `allow_non_loopback_api_host` (YAML), `GOSHISEN_ALLOW_NON_LOOPBACK_API_HOST` (env) and `--allow-non-loopback-api-host` (flag). |
| `queue_maxlen` | `1024` | kernel queue length |
| `api_token` | unset | optional bearer token for API **mutations** (`POST /v1/policy`, `/v1/policy/validate`, `/v1/policy/versions/{v}/activate`). Unset (or empty) means every mutation answers `401` — the API is read-only; the `GET` endpoints stay unauthenticated either way. The init script renders it into `/var/run/goshisen/config.yaml` and **chmod 600**s that file (and the UCI file itself), so the token is not world-readable. Token characters are restricted to `A-Za-z0-9._-` (a start attempt with anything else aborts naming the option — the value is rendered into YAML verbatim). |
| `require_token_for_reads` | `0` | D1 hardening opt-in (requires `api_token`): when `1`, `GET /v1/policy` and `GET /v1/policy/versions` require the bearer token. `/healthz` and `/readyz` stay open. Setting `1` without `api_token` is a configuration error (start aborts). |
| `metrics_token_required` | `0` | D1 hardening opt-in (requires `api_token`): when `1`, `GET /metrics` requires the bearer token. Off by default so Prometheus scraping keeps working. |
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

Stateful IPv6 note: stateful IPv6 ACCEPT requires the **canonical
bidirectional rule pattern** — allow `NEW` per service in one direction,
and a separate `state: [ESTABLISHED, RELATED]` ACCEPT rule with no
src/dst that matches reply traffic in either direction. A
direction-specific established rule (e.g. reply-only `src=... dst=...`)
drops the client's original-direction established ACK/data and the
handshake never completes — this is policy semantics, not an IPv6
limitation. Verified on kernel 6.12.112 (OpenWrt 25.12.6 x86/64):
stateful IPv6 completes end-to-end with the canonical pattern (see
`docs/release/issue-5-investigation.md`). The packaged default policy
(`openwrt/files/etc/goshisen/policy.yaml`) is `default_action: DROP`
with no rules. On kernels whose `nfnetlink_queue` build genuinely does
not deliver conntrack attributes for IPv6, IPv6 packets arrive as
INVALID and are dropped (fail-closed, never allowed) — no kernel has
been observed in this state; older/different kernels remain untested.
Verify with `goshisen_packets_by_state_total{state="invalid"}`.

## Recovery (LAN→WAN blocked)

The hook is intentionally left in place when the daemon stops, so
forwarding stays blocked (fail-closed) rather than silently allowed.

**Config missing (`/etc/config/goshisen` absent/deleted) blocks
forwarding.** The init script treats a missing config as "service
disabled" and refuses to start; the hook that is already installed (boot
guard, or the previous daemon's table left on stop) **stays in place**,
so LAN→WAN continues to be blocked until either:

- the config is restored (`apk reinstall goshisen` re-ships the packaged
  `/etc/config/goshisen` if you removed it; a real factory reset of the
  router's UCI does the same), then `uci set goshisen.main.enabled='1'`,
  `uci commit`, `/etc/init.d/goshisen start`, or
- `goshisen hook cleanup` is run explicitly (removes the app-owned
  `inet goshisen` table; firewall4 alone then governs forwarding).

Router-local access (serial console, SSH to the router, LuCI) is never
blocked by the forward hook, so these commands remain reachable — even
from the WAN side, the router's own management plane is not queued,
so the operator can always recover remotely.

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

**Status: APK package signing — production-signed since v0.2.3.**
The release workflow signs the CLI release archives (tarballs, checksums,
SBOM) with the Ed25519 production key (fingerprint
`e9b4bf1194ce90d2377720102748460404eacbc7aba9aa8ad48532cd63294fb9`
since the 2026-11-28 rotation; `24215e01...` for v0.2.1–v0.2.9).
The OpenWrt `.apk` is production-signed (apk v3) with the APK key whose
public anchor is `keys/goshisen-apk.pub.pem` (EC P-256, fingerprint
`542bb7ae9058fee9b4d37ed30b0af44f234ea7cb8866daa1ffc71a7bc37c839d`).
The two keys are distinct and never interchanged. Install per the
signed-package steps in the Install section; `--allow-untrusted` is no
longer needed and the package-manager trust check is enforced.

## Verification commands

```sh
nft list chain inet goshisen forward        # hook at priority filter + 1
cat /proc/net/netfilter/nfnetlink_queue     # queues 100 (v4) and 101 (v6)
logread | grep goshisen                     # startup, reload, warnings
wget -qO- http://127.0.0.1:9090/metrics | grep goshisen_   # counters
```