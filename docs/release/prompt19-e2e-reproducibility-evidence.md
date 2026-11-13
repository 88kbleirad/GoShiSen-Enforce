# Privileged NFQUEUE e2e Reproducibility — Evidence (Prompt 19, backlog #6)

**Date:** 2026-11-14 (UTC)
**Status:** PASS — one runner command executes the same path on CI and a local
privileged lab; unprivileged environment reported BLOCKED; artifacts and
manifest captured.

---

## 1. Deliverables

- `scripts/e2e-env-manifest.sh` — read-only environment manifest (OS/kernel/arch,
  uid, sudo, capabilities, NFQUEUE module/proc, tool versions, image/kmod pins via
  `openwrt-image-prep.sh --print-config`, commit/branch, UTC timestamp; `--json`).
- `scripts/e2e-runner.sh` — single entrypoint: preflight (tools + sudo) → manifest
  → pinned provisioning (with one retry for the udev/partx race) → boot →
  `go test ./tests/openwrt_integration/` → trap teardown → failure artifacts.
  Exit codes: 0 PASS, 1 FAIL, 3 ENV/BLOCKED, 4 PROVISION, 5 BOOT.
- `scripts/openwrt-lab.sh` — hardened `down` (kills ALL lab QEMU, not just pidfile;
  removes netns/tap/bridge/veth; bracket-safe patterns) and `up` (stale-pidfile
  guard via `/proc/<pid>/cmdline` + idempotent pre-clean).
- CI `openwrt-integration` job now invokes the runner and uploads
  `/tmp/e2e-artifacts/**` on failure.
- Tests `tests/package/e2e_reproducibility_test.go`.
- Design `docs/design/e2e-reproducibility-phase.md`.

## 2. Matrix

| Environment | Command | Result |
| --- | --- | --- |
| Local unprivileged (workstation, no nopasswd sudo) | `E2E_ARTIFACT_DIR=... scripts/e2e-runner.sh` | **BLOCKED** exit 3: `passwordless sudo required ... environment is BLOCKED` |
| Instabox privileged (sudo nopasswd, QEMU TCG) | `sudo -E bash scripts/e2e-runner.sh` | **PASS** — manifest → pinned provisioning (checksum `f8538784…`) → boot → e2e PASS → teardown rc=0 |
| CI privileged (ubuntu-24.04) | job `openwrt-integration` (runner) | pending CI run on the PR |
| Hardware-capable | — | not available (recorded limit) |

## 3. Manifest (instabox privileged run)

```
kernel: 6.18.60-instabox; arch: x86_64; uid: 1000
sudo_nopasswd: yes
nfqueue_proc: absent              # host; the lab VM provides NFQUEUE via kmods
nfqueue_module: unknown
qemu-system-x86_64: QEMU 10.0.13; nft: v1.1.7; iptables: 1.8.11 (nf_tables)
image_pin: openwrt-25.12.6-x86-64-generic-ext4-combined.img.gz (downloads.openwrt.org)
kmod_pins: KMOD_KERNEL=6.12.112; KMOD_PKGREL=r1
sha256_owrt.img: 27d6a99117a6d9c2a8eedbfda0f8baa7a3149f7ddfa168f4bb0ae13815130162
sha256_goshisen-static: 86806dc6a88e133f4ecc6fbc8f12f326bc1365fa27d63f5e6bb268dee3f591cb
```

## 4. e2e result (privileged)

`TestOpenWrtE2E` PASS: DROP blocks the request (client timeout, no server-side
request), ACCEPT permits it (HTTP 200 + marker), daemon verdicts across NEW and
ESTABLISHED (`accept +13 = new +1, established +12`), nft hook counter in
lockstep, kernel queue drops flat. Gate unit tests (18) all PASS.

## 5. Failure taxonomy verified live

- **Missing tool**: non-login PATH lacked `iptables` → runner exited 3 naming the
  tool (before touching anything).
- **image-prep udev/partx race**: `rootfs partition /dev/loopNp2 not found` →
  runner retried once after `losetup -D` → succeeded.
- **Stale QEMU/netns**: earlier crashed run left a second QEMU + netns; hardened
  `down` removed all lab resources (0 netns, 0 lab links, pidfile gone), and
  `up` no longer short-circuits on a stale-but-alive pidfile. Follow-up run was
  clean.
- **Host NFQUEUE absent** → note only (lab VM provides it); not a blocker.
- **Unprivileged** → exit 3 BLOCKED (never a fake pass/skip).

## 6. Local verification

- `make check` + `go test ./tests/... -count=1` green; new tests: manifest text +
  JSON, missing-tool → 3, no-sudo → BLOCKED, pins captured, lab-script cleanup
  safety, script syntax.
- CI YAML validated.

## 7. Limits

QEMU TCG only; no hardware-capable environment in this task (recorded). Tag
`v0.2.1`, release assets and production signing key untouched.
