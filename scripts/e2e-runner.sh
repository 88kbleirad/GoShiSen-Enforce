#!/usr/bin/env bash
# GoShiSen-Enforce — privileged OpenWrt e2e runner (CI + local).
#
# Single entrypoint for the same test path in CI and on a local
# privileged lab. Guarantees:
#   - dependency preflight (fail early, actionable);
#   - environment manifest written before the run;
#   - pinned image/provisioning via openwrt-image-prep.sh;
#   - lab up -> e2e -> teardown with a trap (cleanup is idempotent and
#     bounded to recorded PID/netns/temp paths);
#   - if a significant failure occurs, diagnostics are collected into
#     $E2E_ARTIFACT_DIR (serial/daemon log, nft state, routes,
#     processes, modules, manifest).
#
# Usage:
#   E2E_ARTIFACT_DIR=/tmp/e2e-artifacts \
#   scripts/e2e-runner.sh [--image /tmp/owrt.img] [--daemon /tmp/goshisen-static]
#
# Exit codes:
#   0  PASS
#   1  FAIL (behavior or evidence mismatch)
#   3  ENV (missing deps / blocked environment)
#   4  PROVISION (image-prep failed)
#   5  BOOT (console/readiness timeout)
set -euo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

IMAGE=${E2E_IMAGE:-/tmp/owrt.img}
DAEMON=${E2E_DAEMON:-/tmp/goshisen-static}
ARTIFACTS=${E2E_ARTIFACT_DIR:-/tmp/goshisen-e2e-artifacts}
LAB_LOG=${OPENWRT_LOG:-/tmp/owrt-console.log}
PIDFILE=${OPENWRT_PIDFILE:-/tmp/owrt-qemu.pid}

while [ $# -gt 0 ]; do
  case "$1" in
    --image) IMAGE=$2; shift 2 ;;
    --daemon) DAEMON=$2; shift 2 ;;
    --artifacts) ARTIFACTS=$2; shift 2 ;;
    *) echo "usage: $0 [--image PATH] [--daemon PATH] [--artifacts DIR]" >&2; exit 2 ;;
  esac
done

say() { echo "[e2e-runner] $*"; }
die() { echo "[e2e-runner] ERROR: $*" >&2; exit "${2:-1}"; }

# --- dependency preflight (fail early, actionable) ---
need() {
  # search the standard system PATH even when the caller's PATH is sparse
  # (e.g. non-login shells without /usr/sbin).
  local p tool
  for p in "$PATH" /usr/local/sbin /usr/local/bin /usr/sbin /usr/bin /sbin /bin; do
    [ -n "$p" ] || continue
    for tool in "$1"; do
      if [ -x "$p/$tool" ] && [ ! -d "$p/$tool" ]; then
        return 0
      fi
    done
  done
  die "required tool '$1' missing (apt-get install ...)" 3
}
need curl; need wget; need python3; need qemu-system-x86_64; need nft
need iptables; need ip; need sudo
# Tests may override the required-tool set (e.g. to simulate a missing tool).
# shellcheck disable=SC2153
if [ -n "${E2E_REQUIRED_TOOLS:-}" ]; then
  for t in $E2E_REQUIRED_TOOLS; do need "$t"; done
fi

# Ensure child lab/e2e invocations see a full PATH (non-login shells often
# omit /usr/sbin, which holds iptables/ip/qemu).
export PATH="$PATH:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

if ! sudo -n true 2>/dev/null; then
  die "passwordless sudo required (netns/TAP/lab); environment is BLOCKED" 3
fi
# NFQUEUE for the e2e is provided by the OpenWrt VM (kmod pins verified by
# image-prep). Host-side NFQUEUE is only needed for the NATIVE integration
# suite, not this QEMU lab. Log the host state for diagnostics.
if [ ! -e /proc/net/netfilter/nfnetlink_queue ]; then
  say "note: host NFQUEUE absent (lab VM provides it via kmods)"
fi

# --- environment manifest (traceability) ---
mkdir -p "$ARTIFACTS"
MANIFEST="$ARTIFACTS/environment-manifest.txt"
(bash "$ROOT/scripts/e2e-env-manifest.sh" --label "$(hostname)" 2>&1) > "$MANIFEST" || true
say "environment manifest: $MANIFEST"

# --- cleanup trap (idempotent, bounded to recorded resources) ---
cleanup() {
  local rc=$?
  say "cleanup (rc=$rc)"
  # teardown the lab (kills qemu via pidfile, removes netns/tap/bridge)
  (cd "$ROOT" && bash scripts/openwrt-lab.sh down >/dev/null 2>&1 || true)
  # collect diagnostics on significant failure
  if [ "$rc" -ne 0 ]; then
    collect_artifacts
  fi
  return "$rc"
}
trap cleanup EXIT

collect_artifacts() {
  {
    echo "### serial console tail ($$LAB_LOG)"
    tail -n 80 "$LAB_LOG" 2>/dev/null || echo "(no lab log)"
    echo "### qemu/processes"
    pgrep -af 'qemu-system' 2>/dev/null | head -5 || true
    echo "### netns"
    ip netns list 2>/dev/null || true
    echo "### lab interfaces"
    ip -o link show 2>/dev/null | grep -E 'gsh-owrt' || true
    echo "### nft ruleset (goshisen/fw4)"
    nft list ruleset 2>/dev/null | grep -E 'goshisen|fw4' | head -40 || true
    echo "### nfnetlink_queue proc"
    cat /proc/net/netfilter/nfnetlink_queue 2>/dev/null || true
    echo "### modules"
    grep -E 'nfnetlink|nf_conntrack|nft_' /proc/modules 2>/dev/null | head -10 || true
  } > "$ARTIFACTS/failure-diagnostics.txt" 2>&1 || true
  cp "$MANIFEST" "$ARTIFACTS/" 2>/dev/null || true
  say "diagnostics written to $ARTIFACTS/failure-diagnostics.txt"
}

# --- provision image (pinned checksum path) ---
say "provisioning image (checksum-pinned) -> $IMAGE"
provision_image() {
  (cd "$ROOT" && sudo -E bash scripts/openwrt-image-prep.sh \
    --download --daemon "$DAEMON" --output "$IMAGE")
}
if ! provision_image; then
  # The udev/partx race (rootfs partition not found) is transient: a
  # single retry after releasing the loop device is the documented fix.
  say "image-prep failed; retrying once after loop cleanup"
  sudo losetup -D 2>/dev/null || true
  sleep 2
  provision_image || die "image-prep failed after retry (PROVISION)" 4
fi

# --- boot lab ---
say "booting lab"
(cd "$ROOT" && sudo -E bash scripts/openwrt-lab.sh up) \
  || die "lab up failed" 5

# Wait for serial console.
for i in $(seq 1 60); do
  if (echo > /dev/tcp/127.0.0.1/5555) 2>/dev/null; then
    say "console up after $((i*2))s"; break
  fi
  sleep 2
done

# --- run e2e (same command/path as CI) ---
say "running openwrt e2e"
set -o pipefail
sudo -E env "PATH=$PATH" go test ./tests/openwrt_integration/ \
  -v -count=1 -timeout 25m | tee "$ARTIFACTS/e2e.log"
rc=${PIPESTATUS[0]}
if grep -q -- '--- SKIP' "$ARTIFACTS/e2e.log"; then
  die "openwrt e2e SKIPPED; gate cannot pass (BLOCKED)" 3
fi
if [ "$rc" -ne 0 ]; then
  die "openwrt e2e FAILED (rc=$rc)" 1
fi
say "PASS"
exit 0
