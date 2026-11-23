# OpenWrt Integration Hardening & Automated E2E (v0.2.0 prep)

Date: 2026-11-11. Scope: `phase10-openwrt` branch work.

## Summary

Manual NFQUEUE verdict verification (post-release-validation, `docs/release/post-release-validation.md`) converted into a **reproducible automated E2E test** for the OpenWrt QEMU lab, plus CI wiring and a reviewable PR.

## Deliverables

| Artifact | Purpose |
|---|---|
| `scripts/openwrt-e2e.sh` | self-asserting DROP/ACCEPT control test on one HTTP path |
| `scripts/openwrt-image-prep.sh` | pinned, checksum-verified OpenWrt image provisioning |
| `tests/openwrt_integration/e2e_test.go` | Go wrapper (CI entrypoint, skip semantics) |
| `tests/openwrt_integration/policies/{deny,allow}-lab-http.yaml` | test fixtures |
| `.github/workflows/ci.yml` | new `openwrt-integration` job |

All work on `phase10-openwrt`; released `v0.1.0` untouched.

## What the E2E proves (not netcat exit codes)

For one TCP HTTP path (LAN client `10.0.3.2` → OpenWrt router → WAN slirp `10.0.2.2` = host loopback):

1. **Explicit DROP**: policy has one NEW-state DROP rule for the tuple.
   - client curl times out and never sees the marker,
   - the server (host) logs no request,
   - kernel queue `id_sequence` advances, `queue_dropped`/`queue_user_dropped` stay 0,
   - daemon metrics show `action="drop"` verdicts, zero backend errors.
2. **Explicit ACCEPT**: only the policy changes (same queue, hook, topology).
   - curl exits 0, HTTP 200, body `GSH-ALLOW-PROOF`,
   - server logs the GET,
   - daemon metrics show `action="accept"` on both NEW and ESTABLISHED,
   - identity holds: daemon verdict delta == hook counter delta == queue sequence delta.

The script requires one console round trip per snapshot (nft counter + queue stats + metrics written to `/tmp` in-VM, then read in one call) and waits for each SIGHUP reload to be *observed in the daemon log* before generating traffic (avoids the reload-race pitfall documented in post-release-validation).

## Fixes in this phase

### NFQUEUE bind on kernels without conntrack glue (`internal/packet`)

The daemon unconditionally requested `NFQA_CFG_F_CONNTRACK` during queue
configuration. OpenWrt 25.12.6's stock kernel builds `nfnetlink_queue`
without the conntrack delivery glue, so the kernel rejects the flags
message with `EOPNOTSUPP` — the whole bind failed and the daemon could
not enforce anything on a stock image (the manual post-release lab had
worked because that image's kernel accepted the flag).

Fix: the conntrack-flags message is now optional. On `EOPNOTSUPP` the
queue binds without conntrack attributes; packets arrive untracked, the
engine classifies them INVALID, and the fail-closed default (DROP)
still applies. The fail-closed invariant (never `NFQA_CFG_F_FAIL_OPEN`)
is untouched. Regression tests: `TestCTFlagsTolerable`,
`TestConfigSequenceEndsWithCTGFlags`.

## Image reproducibility

- Base: official `openwrt-25.12.6-x86-64-generic-ext4-combined.img.gz`; sha256 pinned in-`sha256sums` before use (local base hash `f8538784…` matches the release manifest).
- Kernel queue modules: base image ships no `nfnetlink_queue`/`nft_queue` kmods, so the daemon's hook cannot install on a stock boot, and no `nf_conntrack_netlink` kmod, so stateful ACCEPT cannot work (queued packets would arrive untracked → INVALID → fail-closed DROP). `openwrt-image-prep.sh` downloads the three pinned kmod packages for kernel `6.12.112-1-…` and installs them offline into the rootfs (`apk add --no-network --root`, sha256-verified) plus `/etc/modules.d` entries:
  - `kmod-nfnetlink-queue` (`nfnetlink_queue.ko`)
  - `kmod-nft-queue` (`nft_queue.ko`)
  - `kmod-nf-conntrack-netlink` (`nf_conntrack_netlink.ko`).
- Provision: static CGO-disabled daemon binary, rendered config, packaged deny-all policy, `br-lan`/`eth1` LAN + DHCP `wan`/`eth0` network config, `rc.local` autostart (log at `/tmp/goshisen2.log`).
- Verified locally (fresh-boot of the provisioned image, daemon autostarted via rc.local, queue 100 bound, `ip goshisen` table installed):
  - DROP control: curl exit 28, daemon drop verdicts +7, hook counter +7, kernel `queue_dropped`/`queue_user_dropped` +0, server logged no request.
  - ACCEPT control (same path): curl exit 0, HTTP 200 + `GSH-ALLOW-PROOF`, daemon accept verdicts +10 (new +1, established +9), hook +10, identity holds (hook delta == verdict delta), server logged the GET.
  - Policy restored to the packaged deny-all (rules=0) after the run.

### Daemon readiness + failure diagnostics (2026-11-23)

A lost `rc.local` boot race must not be reported as a product failure:

- Readiness is polled from `/proc/net/netfilter/nfnetlink_queue` first (a bound
  queue has a non-zero peer portid; `/tmp/goshisen2.log` can be rotated away by a
  daemon respawn), with the `NFQUEUE bound` log line as the fallback.
- If neither is ready after 300 s (slow TCG cold boot), the script starts the
  daemon explicitly (`setsid … >/tmp/goshisen2.log 2>&1 < /dev/null &` — busybox
  ships no `nohup`) and re-polls for 120 s before failing `ENV`.
  `scripts/openwrt-e2e-v6.sh` always had this fallback;
  `scripts/openwrt-e2e.sh` gained it in PR #134 after CI run `45158649686` failed
  the v4 gate while the v6 job passed on the same commit.
- `scripts/e2e-runner.sh` collects `failure-diagnostics.txt` **before**
  `lab down` (the earlier teardown-first order produced an artifact with an empty
  serial tail and no guest state) and adds bounded guest probes: daemon log tail,
  `logread -e goshisen`, `dmesg`, `pidof goshisen` + queue kmods,
  `nfnetlink_queue` and the `goshisen` nft table.
- Rehearsal recipe for this path: provision the image, delete the
  `goshisen --config` autostart line from the mounted rootfs' `/etc/rc.local`
  (`losetup --find --show -P` + `partx -a` + `mount <loop>p2`), then boot and run
  `scripts/openwrt-e2e.sh` — the run must log
  `daemon not ready after 300s; starting it explicitly (rc.local autostart race)`
  and still pass both controls.

## Local validation

| Check | Result |
|---|---|
| `scripts/openwrt-e2e.sh` (live lab, full run) | PASS (DROP + ACCEPT controls) |
| `go test ./tests/openwrt_integration/` | PASS |
| `go vet ./...` | PASS |
| `gofmt -l .` | clean |
| `go test ./... -count=1` | PASS |
| `go test -race ./... -count=1` | PASS |
| `go build ./...` | PASS |
| `git diff --check` | clean |

## CI (new `openwrt-integration` job)

- `ubuntu-24.04`, checkout + setup-go + tooling (`qemu-system-x86`, `e2fsprogs`, etc.).
- Build static daemon → `openwrt-image-prep.sh --download` (verified checksum) → `openwrt-lab.sh up` → go-test wrapper (timeout 25m) → teardown (always).
- Skips rejected (same gate policy as the netns integration job).
- Initial CI run hit an image-download 404 (fixed in openwrt-ci-validation, `docs/release/openwrt-ci-validation.md`); all four jobs pass on the fixed PR #3 head (`309d153`).

## Notes / known limitations

- Serial console is the only VM channel (dropbear lacks host keys in the official image); the e2e only depends on `python3`/`curl`/`ip` on the host.
- The test asserts verdict-count **deltas** (baseline after policy reload), so pre-existing traffic does not break identity checks.
- Explicit `api_port: 0` / `metrics_port: 0` in the daemon config file disable the listeners (fixed after v0.2.6; v0.2.6 and earlier ignored file/flag zeros and fell back to 8080/9090).
- The lab fixture (`scripts/openwrt-image-prep.sh`) therefore enables metrics explicitly (`metrics_port: $LAB_METRICS_PORT`, 9090) because the e2e reads verdict counters from `127.0.0.1:9090/metrics`; the API stays disabled (`api_port: 0`, policy is applied via SIGHUP). A snapshot whose metrics fetch fails now fails as `ENV: metrics listener unreachable` instead of "no daemon verdicts recorded".
- PR/submission: branch `phase10-openwrt`, PR #3 open (see `docs/release/openwrt-ci-validation.md`); nothing merged or published here.
