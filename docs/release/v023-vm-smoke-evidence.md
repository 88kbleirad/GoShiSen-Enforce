# v0.2.3 fresh-VM smoke evidence (production-signed apk)

Date: 2026-11-15. Artifact: `goshisen-0.2.3-r1.apk` (production-signed,
draft asset sha256 `948e2e1de454c159bed81bfe845bad160dcf9454238b532b2a87f2e335b0cc6a`).

## Environment

- Clean stock OpenWrt 25.12.6 x86_64 image (combined ext4), QEMU lab
  (slirp WAN 10.0.2.0/24, TAP LAN client ns 10.0.3.0/24).
- LAN configured as `eth1` static `10.0.3.1/24`; WAN `eth0` dhcp.
- Production anchor `/etc/apk/keys/goshisen-apk.pub.pem` (fingerprint
  `542bb7ae…`, verified from the repo anchor file).
- kmods `kmod-nfnetlink-queue`, `kmod-nft-queue`,
  `kmod-nf-conntrack-netlink` installed from stock apks
  (`--allow-untrusted` only for these unsigned stock kmods).

## Results

| Check | Result |
| --- | --- |
| `apk add /tmp/goshisen.apk` (NO `--allow-untrusted`) | **PASS** — `(1/1) Installing goshisen (0.2.3-r1)`, OK |
| Payload files (binary, init.d, config, policy, lifecycle) | all present |
| `goshisen --version` | `goshisen 0.2.3` |
| Installed binary sha256 | `57aef2cf558d80ed8d51c9e7f746021e101cde7a9427fc6432eaab69735b89c0` (matches dry-run payload) |
| Daemon start (procd) | `nftables hook installed table=goshisen scope=openwrt`; `NFQUEUE bound queue=100 maxlen=1024` |
| Queue 100 bound | queue 100 present in nfnetlink_queue |
| DROP control (deny policy) | client curl `28` (timed out); counters `packets_total{action="drop",protocol="tcp"} 7` |
| ACCEPT control (allow policy) | client HTTP `200`; counters accept 3368 (new 8, established 3367) |
| SIGKILL → procd respawn | new pid under `/usr/sbin/goshisen`; service list has goshisen; hook table present |
| `apk del goshisen` cleanup | `hook: table inet goshisen removed`; binary absent; only `inet fw4` table remains; 0 daemon processes |

## Notes

- DROP/ACCEPT controls use the same policies and metrics as the CI
  openwrt e2e (verified 6/6 on `8b55953` and on the source pin).
- The daemon's UI interface was set to `eth1` (the lab LAN) because the
  stock image's `br-lan` bridge is not instantiated by the lab's LAN
  config; `goshisen.main.interface=eth1` matches the validation path.
- The v6 queue (101) shows unbound in this run (IPv6 forwarding not in
  scope here; stateless/stateful IPv6 behavior unchanged from v0.2.2).
