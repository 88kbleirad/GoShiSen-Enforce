# Phase 10.3 — OpenWrt CI Recovery & E2E Validation

Date: 2026-11-11. Branch: `phase10-openwrt`. PR: #3.

## Summary

The first GitHub Actions run for PR #3 (`42954931262`) failed the
`openwrt e2e` job during image provisioning with **HTTP 404** before any
DROP/ACCEPT control ran. Root cause and fix are below; the local
provisioning, fresh-boot, and full E2E were re-run successfully with the
fixed scripts.

## Original CI run

- Run: `42954931262` (PR #3, head `d012a34`).
- build/vet/test: PASS; package: PASS; privileged netns integration: IN
  PROGRESS; openwrt e2e: FAIL (image download 404).

## A. Root cause

`scripts/openwrt-image-prep.sh` pinned the image URL with a nonexistent
path segment:

```bash
# broken
PINNED_URL="https://downloads.openwrt.org/releases/25.12.6/targets/x86/64/generic/$PINNED_IMAGE"
```

The official index keeps images directly under `targets/x86/64/` —
there is no `generic/` subdirectory, so the download returned HTTP 404
and the job failed before provisioning. **Not an enforcement defect.**

## B. Fixes

### Corrected image URL (verified against the live manifest)

```bash
IMAGE_URL="https://downloads.openwrt.org/releases/25.12.6/targets/x86/64/$PINNED_IMAGE"
MANIFEST_URL="https://downloads.openwrt.org/releases/25.12.6/targets/x86/64/sha256sums"
```

Verified 2026-11-11: manifest reached (HTTP 200) and contains

```
f8538784d0288b4133a99ac2a68443e4922ffde6343e80df56a5882364b9d499 *openwrt-25.12.6-x86-64-generic-ext4-combined.img.gz
```

The script downloads the manifest, greps the exact filename, and runs
`sha256sum -c --strict`. Corrupt/missing downloads fail with nonzero
curl status (curl `--fail`) and clear messages. Downloads use a
uniquely named temp directory (`mktemp -d -t gsh-image-prep-XXXXXXXX`)
with bounded retries (3, 2s delay).

### Pin verification (kernel ABI)

- ABI: `6.12.112-1-b4125196ecec75391093e1e2edeba490` (kernel 6.12.112,
  package release r1).
- The kmod directory exists (HTTP 200) and contains:
  - `kmod-nfnetlink-queue-6.12.112-r1.apk` (sha256 `5ba9879f…`)
  - `kmod-nft-queue-6.12.112-r1.apk` (sha256 `0fd1adc9…`)
  - `kmod-nf-conntrack-netlink-6.12.112-r1.apk` (sha256 `a22d2782…`)
- All three hashes match the pinned constants exactly. Filenames embed
  the pinned kernel/ABI. Install is offline via `apk add --no-network
  --root`, checksum-verified before install.

### Loop-device cleanup

Replaced global `losetup -D` (could detach unrelated loop devices) with
a cleanup function that:

- unmounts only the mountpoint this script created,
- detaches only the script-owned device via `losetup -d "$LO"`,
- removes only its own temp dirs,
- reports cleanup errors as warnings,
- is registered once via a trap; successful and failure paths both go
  through it. Verified: a pre-existing unrelated loop device survives a
  failed provision; no loop devices or temp dirs leak.

## C. Code changes

- `scripts/openwrt-image-prep.sh`: URL fix, `--print-config`, base-URL
  override, unique temp dirs, bounded retries, scoped loop cleanup,
  ABI-derived kmod filenames.
- `tests/package/provision_test.go`: non-root checks (URL/image/
  checksum constants/ABI consistency).
- `tests/package/provision_shell_test.go`: root + non-root local HTTP
  server tests (404 download, corrupt checksum), scoped loop cleanup
  and no-global-teardown assertions.

Commits (on `phase10-openwrt`):

- `3fd9f17` fix(lab): correct OpenWrt image URL and scope loop-device cleanup
- `ae173a5` test: cover OpenWrt image provisioning failures
- `603822a` fix(lab): read e2e snapshots via targeted greps

### E2E snapshot-read hardening

The first post-fix local E2E run exposed a second issue: the ACCEPT
control intermittently failed with "no daemon ACCEPT verdicts recorded"
even though the daemon metrics showed accepts. Root cause: the e2e read
the full metrics blob (>1.5 KiB) over the serial console; under QEMU
TCG the read truncates, so the ACCEPT snapshot appeared stale/empty.
Fixed by emitting only the small tagged counter lines
(`goshisen_packets_total` / `_by_state` / `backend_errors`) which fit
the console. Confirmed by a full passing DROP+ACCEPT run on a fresh
boot of the provisioned image.

## D. Local regression (all PASS)

| Check | Result |
|---|---|
| `go mod verify` | PASS |
| `gofmt -l .` | clean |
| `go vet ./...` | PASS |
| `go test ./... -count=1` | PASS (11/11 packages) |
| `go test -race ./... -count=1` | PASS (11/11 packages) |
| `go build ./...` | PASS |
| `git diff --check` | clean |
| bash syntax (`bash -n` on lab scripts) | PASS |
| GitHub Actions YAML parse | PASS |
| Static daemon build (`CGO_ENABLED=0`) | static ELF, version stamp OK |

## E. Image provisioning verified (fresh, offline kmods)

| Check | Result |
|---|---|
| Pinned image download (fixed URL) | PASS |
| SHA-256 vs official manifest | PASS (`f8538784…`) |
| Offline kmod install (3 packages) | PASS (checksums OK) |
| Daemon binary in rootfs | PASS (static, 7.8 MiB) |
| Config + fail-closed policy | PASS |
| Network (br-lan 10.0.3.1/24 + DHCP wan) | PASS |
| rc.local autostart | PASS |
| `nfnetlink_queue.ko`/`nft_queue.ko`/`nf_conntrack_netlink.ko` | PASS |
| Loop/temp cleanup after provision | PASS (no leftovers) |

## F. OpenWrt E2E (fresh boot of provisioned image)

QEMU TCG boot, daemon autostarted, queue 100 bound:

| Validation | Result |
|---|---|
| Image download | PASS |
| SHA-256 verification | PASS |
| Offline kmod installation | PASS |
| QEMU boot | PASS |
| NFQUEUE queue 100 | PASS (bound, id_sequence advances) |
| DROP control | PASS (curl exit 28, no server request, drop verdicts +7, hook +7, queue_dropped/+user_dropped +0) |
| ACCEPT HTTP 200 | PASS (curl exit 0, 200, body `GSH-ALLOW-PROOF`) |
| Conntrack | PASS (state=new +1, state=established +9; NFQA_CT delivered) |
| Counter identity | PASS (hook delta == daemon verdict delta; queue drops flat) |
| Cleanup | PASS (policy restored deny-all rules=0, lab down, no leftover namespaces/servers) |

## G. GitHub CI

- Local head committed/pushed to `phase10-openwrt` after fixes (new head
  `603822a`).
- Original failing run: `42954931262` (head `d012a34`) — image 404.
- CI runs for fixed head — **all required jobs PASS on both**:
  | Run | build/vet/test | package | privileged integration | openwrt e2e |
  |---|---|---|---|---|
  | push `42960015562` | PASS | PASS | PASS | PASS (3m18s) |
  | PR `42960021862` | PASS | PASS | PASS | PASS (3m16s) |
- The openwrt job log shows the actual controls: `DROP control PASS
  (curl exit=28, drop verdicts +7, hook +7, queue drops +0)` then
  `ACCEPT control PASS (curl exit=0, HTTP 200, marker body, accept
  verdicts +14, hook +14)`; `--- PASS: TestOpenWrtE2E (157.96s)`,
  zero skips.

## H. Security review

- Fail-closed preserved: `NFQA_CFG_F_FAIL_OPEN` never requested
  (unit-tested). Unsupported CT glue is tolerated (queue binds, packets
  untracked → INVALID → DROP) — no bypass.
- fw4 rules untouched; hook is a separate `ip goshisen` table at
  priority filter+1 with policy accept, no NAT chains.
- No host firewall flush; no global loop teardown; no secrets or debug
  binaries committed (only static daemon binary in images built by the
  script, never committed).
- v0.1.0 release untouched.

## I. Remaining blockers

None. All four CI jobs pass on the fixed head (push and PR runs),
zero mandatory skips; local regression and the full OpenWrt E2E pass.

## J. Verdict

**READY_TO_MERGE** — all phase 10.3 gates green. Merge itself requires
explicit authorization (not performed here).
