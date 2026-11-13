# Privileged NFQUEUE e2e Reproducibility — Design (Prompt 19, backlog #6)

**Status:** proposed (implementation follows).
**Applies to:** `scripts/e2e-runner.sh`, `scripts/e2e-env-manifest.sh`, CI
`openwrt-integration` job integration, docs. Release v0.2.1/assets/signing key
untouched.

---

## 1. Dependency inventory (what the e2e needs)

| Dep | Where | Role | Check |
| --- | --- | --- | --- |
| root + passwordless sudo | host | TAP/netns/loop-mount | `sudo -n true` |
| NFQUEUE kernel support | host VM kernel | `nfnetlink_queue` proc + module | `/proc/net/netfilter/nfnetlink_queue` |
| netns + TAP + bridge | host | LAN client + qemu side | `ip netns add`, `ip tuntap` |
| qemu-system-x86_64 | host | VM | `--version` |
| nft / iptables / iproute2 | host | hook + lab FORWARD accepts | `--version` |
| python3, curl, wget | host | console client, marker, provisioning | `--version` |
| OpenWrt image + kmods | network | boot + NFQUEUE in VM | SHA256 pins (`image-prep --print-config`) |
| ports | host | 5555 console, 2222 ssh fwd, 8088 marker | free / owned by this run |
| Go | CI/local | builds daemon | `go version` |

## 2. Failure taxonomy (distinct, never conflated)

| Category | Detect | Report |
| --- | --- | --- |
| host | missing tool / no sudo / no NFQUEUE proc | ENV exit 3, actionable |
| image-prep | `image-prep` non-zero (download/checksum/loop) | PROVISION exit 4 |
| boot/console | console TCP not ready in window; serial garbage | BOOT exit 5 |
| daemon readiness | `NFQUEUE bound` not in daemon log within window | BOOT exit 5 + serial/daemon tail |
| policy reload | e2e assertion on `policy published`/rejection | FAIL exit 1 |
| traffic assertion | DROP/ACCEPT verdict mismatch | FAIL exit 1 |
| environment blocked | unprivileged sandbox (no sudo or no NFQUEUE) | **BLOCKED** exit 3 — NOT pass/skip |

## 3. Reproducibility guarantees

- **One command** (`scripts/e2e-runner.sh`) runs the same path in CI and local
  privileged lab; CI's `openwrt-integration` job is replaced/backed by it.
- **Manifest** written before the run: OS/kernel/arch, tool versions, sudo,
  capabilities, NFQUEUE module, image/kmod pins, commit, timestamp.
- **Pinned artifacts**: `openwrt-image-prep.sh --download` verifies image + kmod
  SHA256 before use; daemon binary built with `-trimpath` and recorded.
- **Cleanup trap**: teardown is idempotent, uses recorded pidfile/netns/temp
  paths only (never host firewall/global state except the lab bridge's narrow
  FORWARD ACCEPT, removed on down).
- **Failure artifacts**: on non-zero run, serial tail, daemon log, nft state,
  nfnetlink proc, modules, processes, netns, routes, manifest are collected into
  `$E2E_ARTIFACT_DIR/failure-diagnostics.txt`.

## 4. Unprivileged sandbox behavior

The unprivileged sandbox (no passwordless sudo, or NFQUEUE missing) must report
`BLOCKED` (exit 3) with the exact missing prerequisite — never a silent pass or
a fake skip. The CI job is privileged (ubuntu-24.04 runner with sudo + QEMU +
kernel NFQUEUE), so it is the authoritative gate.

## 5. Tests

- missing dependency → exit 3 + message;
- no NFQUEUE proc (simulated) → exit 3 BLOCKED;
- occupied port (5555) → preflight/boot error handled as BOOT, not hang;
- stale qemu/netns → `lab up` idempotent cleanup first, rerun succeeds;
- malformed console (non-UTF8) → e2e sanitizes bytes (existing behavior), never
  wedges;
- timeout diagnostics → artifacts dir populated with failure-diagnostics;
- repeated run after failure → clean second run (no leftovers).

## 6. Limits

QEMU TCG only; hardware-capable environment records its own metadata if used
(none in this task). No production device touched.

## 7. Verified live findings

- **Missing tool preflight fired first**: instabox non-login PATH lacked
  `iptables`; runner exited 3 naming the tool. `need` now searches the full
  system PATH.
- **image-prep udev/partx race** (`rootfs partition /dev/loopNp2 not found`) is
  transient; the runner retries once after `losetup -D`.
- **Host NFQUEUE absent is NOT a blocker** for the QEMU lab (the VM image
  provides kmods; verified by image-prep checksums). Manifest records it; the
  native integration suite is the one that requires host NFQUEUE.
- **Unprivileged workstation** (no nopasswd sudo) → exit 3 BLOCKED, verified.


## 8. Typed preflight states (Prompt 20)

The preflight is a deterministic state machine with a stable output schema; the
privilege gate precedes the tool gate, so no-sudo reports BLOCKED even when other
tools are missing:

```
[e2e-runner] CONFIG_ERROR: <reason>
[e2e-runner] BLOCKED: <reason>
[e2e-runner] MISSING_DEPENDENCY: <reason>
[e2e-runner] RESOURCE: <reason>
[e2e-runner] READY: privileged e2e preflight passed
```

Gates and order: CONFIG (args/input) → PRIVILEGE (root, else passwordless sudo)
→ DEPENDENCY (curl/wget/python3/qemu/nft/iptables/ip/sudo) → RESOURCE (ports;
host NFQUEUE absence is a note) → READY. Exit 3 covers the whole environment class
(compatible with previous callers); the token line is authoritative.

Test injectability (no host tool dependency): `E2E_MOCK_SUDO=no|yes`,
`E2E_MOCK_MISSING_TOOLS="tool …"`, `E2E_MOCK_PORT_OCCUPIED=<port>`,
`E2E_MOCK_CONFIG=bad`, `E2E_PREFLIGHT_ONLY=1` (stop after READY).
