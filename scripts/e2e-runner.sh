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

# ---------------------------------------------------------------------------
# Preflight: typed, deterministic state machine.
#
# States (stable output schema, one line each):
#   [e2e-runner] CONFIG_ERROR: <reason>          (bad args/input)
#   [e2e-runner] BLOCKED: <reason>               (privilege/capability)
#   [e2e-runner] MISSING_DEPENDENCY: <reason>    (tool/module absent)
#   [e2e-runner] RESOURCE: <reason>              (port/filesystem busy)
#   [e2e-runner] READY: privileged e2e preflight passed
#
# Exit 3 is used for the whole environment class (CONFIG_ERROR/BLOCKED/
# MISSING_DEPENDENCY/RESOURCE) so existing callers keep working; the
# token in the output is the authoritative state.
#
# Gates run in this fixed order so the result is deterministic and does
# not depend on which tools happen to be installed on the test host.
# Each gate is injectable via E2E_MOCK_* for tests (see below).
# ---------------------------------------------------------------------------

# mock_state_out <state> <reason>  — emit the schema line and exit 3.
mock_state_out() {
  echo "[e2e-runner] $1: $2" >&2
  exit 3
}

preflight_privilege() {
  # root is sufficient; otherwise passwordless sudo is required. In tests
  # this is mocked via E2E_MOCK_SUDO=no/yes.
  if [ "$(id -u)" = 0 ]; then
    return 0
  fi
  if [ -n "${E2E_MOCK_SUDO:-}" ]; then
    if [ "$E2E_MOCK_SUDO" != "yes" ]; then
      mock_state_out BLOCKED "privileged execution requires sudo/capability (E2E_MOCK_SUDO=no)"
    fi
    return 0
  fi
  if command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
    return 0
  fi
  mock_state_out BLOCKED "privileged execution requires sudo/capability (netns/TAP/lab)"
}

preflight_dependencies() {
  # In tests, E2E_MOCK_MISSING_TOOLS lists tools to treat as absent.
  local missing_list="${E2E_MOCK_MISSING_TOOLS:-}"
  local tool p found
  for tool in curl wget python3 qemu-system-x86_64 nft iptables ip sudo; do
    found=0
    # enforced-missing mock takes precedence
    for t in $missing_list; do
      if [ "$t" = "$tool" ]; then found=1; break; fi
    done
    if [ "$found" = 1 ]; then
      mock_state_out MISSING_DEPENDENCY "required tool '$tool' missing (apt-get install ...)"
    fi
    found=0
    for p in "$PATH" /usr/local/sbin /usr/local/bin /usr/sbin /usr/bin /sbin /bin; do
      if [ -x "$p/$tool" ] && [ ! -d "$p/$tool" ]; then found=1; break; fi
    done
    if [ "$found" = 0 ]; then
      mock_state_out MISSING_DEPENDENCY "required tool '$tool' missing (apt-get install ...)"
    fi
  done
}

preflight_resources() {
  # NFQUEUE for the lab e2e is provided by the OpenWrt VM (kmod pins);
  # host-side absence is only a note, not a failure. Port checks are
  # early-warning only (the boot phase is authoritative).
  if [ -n "${E2E_MOCK_PORT_OCCUPIED:-}" ]; then
    mock_state_out RESOURCE "port ${E2E_MOCK_PORT_OCCUPIED} is already in use"
  fi
  if [ ! -e /proc/net/netfilter/nfnetlink_queue ]; then
    say "note: host NFQUEUE absent (lab VM provides it via kmods)"
  fi
}

preflight() {
  # 1. CONFIG gate
  if [ -n "${E2E_MOCK_CONFIG:-}" ]; then
    mock_state_out CONFIG_ERROR "invalid configuration (E2E_MOCK_CONFIG=${E2E_MOCK_CONFIG})"
  fi
  [ -n "$DAEMON" ] || mock_state_out CONFIG_ERROR "--daemon path is required"
  [ -n "$IMAGE" ] || mock_state_out CONFIG_ERROR "--image path is required"

  # 2. PRIVILEGE gate
  preflight_privilege

  # 3. DEPENDENCY gate
  preflight_dependencies

  # 4. RESOURCE gate
  preflight_resources

  echo "[e2e-runner] READY: privileged e2e preflight passed"
}
# --- run the typed preflight (ordered gates) ---
export PATH="$PATH:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
preflight
if [ -n "${E2E_PREFLIGHT_ONLY:-}" ]; then
  say "preflight-only mode: stopping (state READY)"
  exit 0
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
