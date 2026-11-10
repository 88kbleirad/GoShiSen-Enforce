# Installing GoShiSen-Enforce

GoShiSen-Enforce is a fail-closed, stateful nftables/NFQUEUE firewall
daemon. This guide covers installing from official release binaries.
The daemon manages only its own nftables table (`ip goshisen`); it
never touches other firewall rules.

## Supported platforms

The following combinations are built and validated in CI:

| Architecture | Build | Runtime validation | Notes |
|---|---|---|---|
| `linux/amd64` | PASS | PASS (CI + local) | Primary target |
| `linux/arm64` | PASS | PASS (CI) | Build is statically linked; runtime validation on arm64 is limited by availability of arm64 runners — see Compatibility notes |

- Go 1.25+ toolchain is required to build from source (the release
  binaries are pre-built and need no toolchain).
- The daemon is a static binary: no shared libraries are required.
- The `nft` binary must be present at runtime.

### Kernel requirements

The daemon verifies these at startup and refuses to run (fail loudly)
if missing:

- NFQUEUE support (`CONFIG_NETFILTER_NETLINK_QUEUE`, or the
  `nfnetlink_queue` module).
- nftables (`CONFIG_NF_TABLES`).
- conntrack (`CONFIG_NF_CONNTRACK`; the `nf_conntrack`,
  `nf_conntrack_netlink` modules).
- Network namespaces only needed for the integration lab, not the
  daemon itself.
- `/proc/net/netfilter/nfnetlink_queue` (queue-overflow telemetry).

### Required utilities

- `nft` (nftables userspace) — the daemon executes it to install/list/
  clean its table.
- For `--interface` validation, the OS network interface list
  (no extra package).

## Install from release binaries

1. Download the archive for your architecture and the checksums file:

   ```bash
   cd /tmp
   curl -LO https://github.com/88kbleirad/GoShiSen-Enforce/releases/download/v0.1.0/goshisen-linux-amd64.tar.gz
   curl -LO https://github.com/88kbleirad/GoShiSen-Enforce/releases/download/v0.1.0/checksums.txt
   ```

2. Verify the SHA-256 checksum:

   ```bash
   grep goshisen-linux-amd64.tar.gz checksums.txt | sha256sum -c -
   ```

3. Extract and install:

   ```bash
   tar xzf goshisen-linux-amd64.tar.gz
   sudo install -m 0755 goshisen-linux-amd64/goshisen /usr/local/bin/goshisen
   sudo install -d -m 0755 /etc/goshisen /var/lib/goshisen
   sudo install -m 0644 goshisen-linux-amd64/INSTALL.md /usr/share/doc/goshisen/
   ```

4. Verify the installed binary:

   ```bash
   goshisen --version     # must print goshisen v0.1.0
   goshisen --help
   ```

## Configuration

A minimal policy file:

```yaml
version: 1
default_action: DROP
rules:
  - id: allow-ssh
    action: ACCEPT
    protocol: tcp
    dst_port: 22
    state: [NEW]
  - id: allow-estab
    action: ACCEPT
    state: [ESTABLISHED, RELATED]
```

Run:

```bash
sudo goshisen --config /etc/goshisen/goshisen.yaml
```

See `docs/user-guide.md` for the full configuration reference.

## Required Linux capabilities

The daemon needs:

- `CAP_NET_ADMIN` — nftables rule management and NFQUEUE binding.
- `CAP_NET_RAW` — raw packet sockets (NFQUEUE).

Run as root or grant exactly these capabilities:

```bash
sudo setcap cap_net_admin,cap_net_raw+ep /usr/local/bin/goshisen
```

Run as an unprivileged user with the binary granted the capabilities
above. Do not run with broader capabilities.

## Service startup (systemd)

A hardened unit file ships at `deployments/systemd/goshisen.service`.
Install it, create the policy file, and start:

```bash
sudo cp deployments/systemd/goshisen.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now goshisen
```

The service uses `Restart=always` — after a crash the daemon restarts
and reinstalls its hook (fail-closed by design).

## Safe firewall activation

The daemon is fail-closed by default: an empty policy or an unstarted
daemon drops all traffic. To activate safely:

1. Start with a permissive policy first (e.g. ACCEPT all) and verify
   the API is reachable, then apply the restrictive policy via the API
   (`PUT /v1/policy`) or reload.
2. On any error, the previous policy stays active; the daemon never
   silently fails open.

## Upgrade

1. Download and verify the new release binaries as above.
2. Stop the service: `sudo systemctl stop goshisen`.
3. Replace the binary: `sudo install -m 0755 ... /usr/local/bin/goshisen`.
4. Start and verify: `sudo systemctl start goshisen`; check
   `goshisen --version` and the health API.

The durable policy store (`/var/lib/goshisen`) survives upgrades; the
current active policy is reapplied on restart.

## Rollback

1. Keep the previous release archive (checksums verified).
2. Stop, install the previous binary, start — same as upgrade.
3. Verify version output matches the rolled-back release.

## Troubleshooting

- **`hook: nft binary not found`** — install `nftables` (`apt install
  nftables` / `dnf install nftables`).
- **`cannot bind queue: operation not permitted`** — the process lacks
  `CAP_NET_ADMIN`; grant capabilities or run as root.
- **`config: interface ... not found`** — the configured interface name
  is wrong or not up; use `--interface` with a real interface.
- **Queue overflow warnings** — the daemon logs rate-limited warnings
  when kernel queue drops occur; see `docs/user-guide.md` for settings.
- **Firewall appears to block everything** — verify the daemon is
  running (`systemctl status goshisen`), the hook is installed
  (`nft list table ip goshisen`), and the policy default action is
  ACCEPT if that is intended.

## Safe removal

```bash
sudo systemctl stop goshisen
sudo rm /usr/local/bin/goshisen
sudo rm -rf /etc/goshisen /var/lib/goshisen
sudo systemctl disable goshisen 2>/dev/null || true
sudo rm /etc/systemd/system/goshisen.service
sudo systemctl daemon-reload
```

The hook (nftables table) is removed on daemon shutdown? no — by
design the daemon leaves the hook in place for fail-closed behavior.
Remove it explicitly with:

```bash
sudo goshisen hook cleanup
```

## Compatibility notes

- Binaries are built with `CGO_ENABLED=0`; no libc dependency for the
  daemon itself.
- The `nft` userspace binary must match the target kernel's nftables
  feature set; the daemon verifies the queue rule installs.
- arm64 runtime validation currently runs in CI where arm64 runners are
  available; if you use arm64 in production without CI validation,
  run `sudo make integration` on an arm64 host first.
- No other platforms are currently built or advertised.
