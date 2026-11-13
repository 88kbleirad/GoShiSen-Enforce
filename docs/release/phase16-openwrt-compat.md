# Phase 16 — OpenWrt Compatibility Verification (Prompt 04)

**Status: complete.** Builds and runtime verification on the current OpenWrt
target: OpenWrt 25.12.6 x86/64, kernel 6.12.112, QEMU dual-bridge lab
(real IPv4 + IPv6 LAN/WAN peers). All behavior verified with paired
positive/negative controls; every claim below is backed by recorded
traffic, logread, kernel queue counters, nftables counters, and package/
binary SHA256 (see `docs/release/phase16-openwrt-compat.md` evidence +
`/data/phase16-openwrt-compat-artifacts/` on the build host).

## 0. Package built from a tested commit

The apk is built by the OpenWrt SDK from commit **f5f211a** — a local
assembly (merge) of the three open, CI-green OpenWrt PRs:
PR #13 (boot guard), PR #14 (flow-offload policy), PR #15 (v0.2.1
notes). `PKG_RELEASE=4`, mirror hash pinned to the SDK-verified tar.zst
(`f922e983…`).

| Artifact | SHA256 |
| --- | --- |
| `goshisen-0.2.0-r4.apk` | `5020b86157e775fb8c706b71ec4b9c2b40ecc0346680d8f3528837c243922754` |
| `/usr/sbin/goshisen` (r4) | `6055cc16a87bcc801bdcf915c8fa8ac81a1fd9f935d9930e842fc77337a906c1` |
| `goshisen-0.2.0-r2.apk` (published v0.2.0) | `b4925a60a594159809bd03c10f73843ca3be8ba6748e6343dcc0e1af00a9c2af` |
| `/usr/sbin/goshisen` (r2) | `a00e4a52626a1f5203255c01ce92c8b94dcb04eaef3c5310f9c27a00136cf6d1` |

Reproducibility: two clean SDK builds (dl/ emptied, build_dir cleared,
same commit) produced **byte-identical apk and binary** (sha256 above).

## 1. Paired controls (per protocol family)

Dual-bridge topology: LAN namespace `10.0.3.0/24` + `fdcd:ea48:12a4::/64`,
WAN namespace `10.0.2.0/24` + `fd00::/64`; the guest forwards between
them. Marker HTTP servers on the WAN peer (v4 `10.0.2.2:8088`, v6
`fd00::2:8089`). Each control asserts both behavior and the kernel/queue
counters.

| Case | r2 baseline | r4 post-upgrade | r2 post-rollback |
| --- | :-: | :-: | :-: |
| v4 deny blocks request | PASS | PASS | PASS |
| v4 allow passes (NEW) | PASS | PASS | PASS |
| v4 established reply passes | PASS | PASS | PASS |
| v6 deny blocks request | PASS | PASS | PASS |
| v6 allow passes (NEW) | PASS | PASS | PASS |
| v6 established reply passes | PASS | PASS | PASS |

v6 ESTABLISHED-reply is exercised with real IPv6 peers (the WAN ns
return route required a static route via the guest; with it, kernel
conntrack sees both directions and the daemon's accepted-set grants
reply matching — metrics show `state="established"` accepts).

## 2. Upgrade v0.2.0 (r2) → r4

- `apk add --allow-untrusted goshisen-0.2.0-r4.apk` over the published
  r2: **PASS** — binary swapped to r4, `goshisen-boot` added, UCI
  conffile preserved (the packaged default config is written as
  `policy.yaml.apk-new`, never overwriting user config).
- **Old config compatibility:** the r2-era UCI (no `offload_policy`
  knob) loads fine on r4; daemon starts, binds queue 100/101, installs
  hook; `offload_policy` defaults to `warn`.
- **No fail-open window:** while `apk add` runs, the running daemon
  keeps its old inode; after the swap a restart picks up the new
  binary. Hook/queue remain live throughout (kernel keeps queue bound).
- **Init ordering (package):** `S12goshisen-boot` (boot guard) →
  `S19firewall` → `S90goshisen`; `K10*` teardown links present.

## 3. Boot guard (pre-firewall fail-closed)

With the main service enabled and the daemon *not yet started*
(simulating the S12→S90 window):

- Guard alone installs `table inet goshisen` with only a `forward`
  chain: `type filter hook forward priority filter; policy drop;` —
  dual-stack (inet), no input/output chains, no accept/bypass.
- Daemon `Install` atomically replaces the table with the enforcement
  table: `priority filter + 1; policy accept;` plus queue rules to
  100 (v4) and 101 (v6). Verified via `nft list chain inet goshisen
  forward` before/after.
- With the service disabled, the guard is a no-op (no table created);
  forwarding is governed solely by firewall4. Failure-safe.

## 4. Flow-offload policy (UCI `offload_policy`)

Enabled firewall4 `flow_offloading=1` (creates `flowtable ft` + `flow
add @ft`), then exercised each mode:

| Mode | Result |
| --- | --- |
| `warn` (default) | daemon runs; WARN log "flow offloading detected …"; gauge `goshisen_flow_offload_active 1` |
| `refuse` | startup fails **before hook install** with actionable error; table untouched (fail-closed by absence) |
| `readiness` | daemon runs but `/readyz` returns `503 not_ready` while offload active; after clearing flowtable (firewall reload), `/readyz` returns `200 ready` |

Verified by logread + metrics + raw HTTP readyz body.

## 5. Reload failure atomicity

With an allow policy active (traffic passing), SIGHUP-reload a
malformed policy (`dst_port: not-a-port`, `src_port: 99999`):
- daemon logs `policy reload rejected; keeping previous policy` (both
  validation errors named);
- the previously loaded policy stays active; traffic continues to pass.

## 6. Daemon crash / restart

- `kill -9` the daemon → procd respawns it (init script `respawn`);
  during the down window forwarding is fail-closed (queue unbound →
  kernel drops); after respawn the policy and queues are restored and
  controls pass. Verified by fetch timeout during the window and 200
  after recovery.
- Crash-loop behavior: if the on-disk policy is invalid, procd retries
  and the daemon keeps refusing at startup (fail-closed), as logged.

## 7. Rollback r4 → r2

- `apk add --allow-untrusted goshisen-0.2.0-r2.apk` over r4: **PASS**
  — binary sha returns to r2; service starts; all controls still pass.
- **Caveat (documented):** the downgrade leaves dangling
  `/etc/rc.d/S12goshisen-boot` + `K10goshisen-boot` symlinks because
  r2 does not ship `goshisen-boot` and the package has no hook to
  remove init-script symlinks on downgrade. A cold boot still completes
  normally (rc.common skips missing scripts with no fatal error); the
  guard simply does not run on the rolled-back version. Mitigation:
  run `/etc/init.d/goshisen-boot disable` before downgrading, or
  `rm /etc/rc.d/S12goshisen-boot /etc/rc.d/K10goshisen-boot`.

## 8. Environment

OpenWrt 25.12.6 x86/64 (kernel 6.12.112), QEMU TCG (`-smp 2 -m 512`),
serial console 127.0.0.1:5555, dual TAP bridges (LAN/WAN namespaces).
The host build environment is the instabox sandbox (Go 1.27.x, SDK
feed). Evidence artifacts: `/data/phase16-openwrt-compat-artifacts/`
(sha256, summary, policies, logs).
