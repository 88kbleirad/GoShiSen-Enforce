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
#   scripts/e2e-runner.sh --list-requirements     # print the preflight contract
#
# Preflight modes:
#   real (default)                — probes observe this host (PATH, uid/sudo,
#                                   kernel modules, ports);
#   hermetic (E2E_PREFLIGHT_FIXTURE=<file>) — the fixture *is* the
#                                   environment; nothing outside it is read
#                                   or executed (see the abstraction below).
#
# Exit codes:
#   0  PASS
#   1  FAIL (behavior or evidence mismatch)
#   2  usage
#   3  ENV (CONFIG_ERROR / BLOCKED / MISSING_DEPENDENCY / RESOURCE)
#   4  PROVISION (image-prep failed)
#   5  BOOT (console/readiness timeout)
set -euo pipefail
# ROOT without external tools (dirname(1)): the preflight must stay
# runnable in a hermetic environment with an empty PATH.
_bashsrc=${BASH_SOURCE[0]}
case $_bashsrc in
  */*) _bashsrc=${_bashsrc%/*} ;;
  *) _bashsrc=. ;;
esac
ROOT=$(cd "$_bashsrc/.." && pwd)
unset _bashsrc

IMAGE=${E2E_IMAGE:-/tmp/owrt.img}
DAEMON=${E2E_DAEMON:-/tmp/goshisen-static}
ARTIFACTS=${E2E_ARTIFACT_DIR:-/tmp/goshisen-e2e-artifacts}
LAB_LOG=${OPENWRT_LOG:-/tmp/owrt-console.log}
PIDFILE=${OPENWRT_PIDFILE:-/tmp/owrt-qemu.pid}
FIXTURE=${E2E_PREFLIGHT_FIXTURE:-}

# Single source of truth for the fixture contract (see preflight section).
REQUIRED_FIXTURE_KEYS="tool curl|tool wget|tool python3|tool qemu-system-x86_64|tool nft|tool iptables|tool ip|tool sudo|uid|module nf_conntrack|nfqueue_proc|port 5555|port 2222"

while [ $# -gt 0 ]; do
  case "$1" in
    --image) IMAGE=$2; shift 2 ;;
    --daemon) DAEMON=$2; shift 2 ;;
    --artifacts) ARTIFACTS=$2; shift 2 ;;
    --fixture) FIXTURE=$2; shift 2 ;;
    --list-requirements)
      # Print the fixture-key contract from its single source of truth.
      req_list=$REQUIRED_FIXTURE_KEYS
      while [ -n "$req_list" ]; do
        req=${req_list%%|*}
        [ "$req" = "$req_list" ] && req_list="" || req_list=${req_list#*|}
        printf '%s\n' "$req"
      done
      exit 0 ;;
    *) echo "usage: $0 [--image PATH] [--daemon PATH] [--artifacts DIR] [--fixture FILE]" >&2; exit 2 ;;
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
# All host observations are injectable via E2E_PREFLIGHT_FIXTURE (see
# below) for hermetic tests; malformed input is exercised with real args.
# ---------------------------------------------------------------------------

# ---------------------------------------------------------------------------
# Environment abstraction.
#
# The preflight's gates are pure: they consume a *view* of the environment
# and never touch the host directly. Two implementations exist:
#
#   real_env (default)  — probes this host: PATH tool lookup, uid/sudo,
#                          kernel module loadability, /proc NFQUEUE node,
#                          ports.
#   fixture_env          — E2E_PREFLIGHT_FIXTURE points at a file that
#                          *is* the environment. Implementation uses only
#                          bash builtins (read/case/[[ ]]), so nothing
#                          outside the file is read, and no external
#                          binary is ever executed: tests are hermetic.
#
# Fixture format (key: value lines; `#` comments and blank lines are
# allowed):
#   tool <name>: present|absent
#   uid: 0|nonroot
#   sudo: yes|no            (passwordless sudo available)
#   module <name>: present|absent
#   nfqueue_proc: present|absent
#   port <n>: free|occupied
#
# A fixture is invalid (CONFIG_ERROR) if a required key is missing or has
# an unknown value, so a typo cannot silently produce a wrong state.
# `--list-requirements` prints the required keys.
# ---------------------------------------------------------------------------

# load_fixture reads the fixture file into FIXTURE_MAP (associative array)
# using pure bash builtins.
load_fixture() {
  local k v line
  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in
      ''|'#'*) continue ;;
      *': '*)
        k=${line%%:*}
        v=${line#*: }
        FIXTURE_MAP["$k"]=$v
        ;;
    esac
  done < "$FIXTURE"
}

real_env() {
  # ENV_HANDLE: returns the "observed value" for a capability.
  #   tool <name>   -> present|absent   (search the full system PATH)
  #   uid           -> 0|nonroot        (effective uid)
  #   sudo          -> yes|no           (passwordless sudo)
  #   module <name> -> present|absent   (loaded, builtin, or loadable)
  #   nfqueue_proc  -> present|absent   (/proc NFQUEUE node)
  #   port <n>      -> free|occupied    (listening socket)
  local what=$1
  local name=${2-}
  case "$what" in
    tool)
      # Search PATH then the standard dirs; the runner's exported PATH is
      # the same one CI uses when it invokes the script with sudo -E env
      # PATH=$PATH.
      local p found=0
      for p in "$PATH" /usr/local/sbin /usr/local/bin /usr/sbin /usr/bin /sbin /bin; do
        if [ -x "$p/$name" ] && [ ! -d "$p/$name" ]; then found=1; break; fi
      done
      [ "$found" = 1 ] && echo present || echo absent
      ;;
    uid)
      [ "$(id -u)" = 0 ] && echo 0 || echo nonroot
      ;;
    sudo)
      if command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
        echo yes
      else
        echo no
      fi
      ;;
    module)
      # Loaded (sysfs), builtin, or loadable via modprobe dry-run all
      # count as present. modprobe --dry-run needs no privileges and is
      # exit-0 for both builtin modules and loadable modules.
      if [ -e "/sys/module/$name" ] || modprobe --dry-run --quiet "$name" 2>/dev/null; then
        echo present
      else
        echo absent
      fi
      ;;
    nfqueue_proc)
      [ -e /proc/net/netfilter/nfnetlink_queue ] && echo present || echo absent
      ;;
    port)
      # A listening socket on the given port (any address). Uses bash's
      # built-in /dev/tcp; the probe is a connection attempt, which is
      # enough to detect an occupied port without requesting root.
      if (exec 3<>/dev/tcp/127.0.0.1/$name) 2>/dev/null; then
        exec 3>&- 3<&- 2>/dev/null || true
        echo occupied
      else
        echo free
      fi
      ;;
    *)
      echo absent
      ;;
  esac
}

fixture_env() {
  # ENV_HANDLE for a hermetic fixture file: the in-memory map IS the
  # environment. Only bash builtins are used.
  local what=$1
  local name=${2-}
  local key="$what"
  local v
  case "$what" in
    tool|module|port)
      key="$what $name"
      v=${FIXTURE_MAP["$key"]:-}
      ;;
    uid|sudo|nfqueue_proc)
      v=${FIXTURE_MAP["$what"]:-}
      ;;
    *)
      echo absent
      return 0
      ;;
  esac
  case "$what" in
    tool|module)
      [ "$v" = present ] && echo present || echo absent
      ;;
    uid)
      [ "$v" = 0 ] && echo 0 || echo nonroot
      ;;
    nfqueue_proc)
      [ "$v" = present ] && echo present || echo absent
      ;;
    sudo)
      [ "$v" = yes ] && echo yes || echo no
      ;;
    port)
      [ "$v" = occupied ] && echo occupied || echo free
      ;;
  esac
}

ENV_HANDLE=real_env
if [ -n "$FIXTURE" ]; then
  if [ ! -f "$FIXTURE" ]; then
    die "preflight fixture not found: $FIXTURE" 3
  fi
  declare -A FIXTURE_MAP
  load_fixture
  # Validate the fixture: every required key must be present with a known
  # value. A malformed fixture is a CONFIG_ERROR, not a guess.
  req_list=$REQUIRED_FIXTURE_KEYS
  while [ -n "$req_list" ]; do
    req=${req_list%%|*}
    [ "$req" = "$req_list" ] && req_list="" || req_list=${req_list#*|}
    key=${req%% *}
    name=${req#* }
    [ "$key" = "$name" ] && name="" || name=" $name"
    v=${FIXTURE_MAP["$key$name"]:-}
    ok=1
    case "$key" in
      tool|module|nfqueue_proc)
        [ "$v" = present ] || [ "$v" = absent ] || ok=0
        ;;
      uid)
        [ "$v" = 0 ] || [ "$v" = nonroot ] || ok=0
        ;;
      sudo)
        [ "$v" = yes ] || [ "$v" = no ] || ok=0
        ;;
      port)
        [ "$v" = free ] || [ "$v" = occupied ] || ok=0
        ;;
      *)
        ok=0
        ;;
    esac
    if [ "$ok" = 0 ]; then
      echo "[e2e-runner] CONFIG_ERROR: fixture missing/invalid key '$key$name'" >&2
      exit 3
    fi
  done
  ENV_HANDLE=fixture_env
fi

# mock_state_out <state> <reason>  — emit the schema line and exit 3.
mock_state_out() {
  echo "[e2e-runner] $1: $2" >&2
  exit 3
}

preflight_privilege() {
  # Root is sufficient; otherwise passwordless sudo is required. If we
  # are already root the uid probe is authoritative regardless of the
  # sudo probe.
  if [ "$("$ENV_HANDLE" uid)" = "0" ]; then
    return 0
  fi
  if [ "$("$ENV_HANDLE" sudo)" != "yes" ]; then
    mock_state_out BLOCKED "privileged execution requires sudo/capability (netns/TAP/lab)"
  fi
  return 0
}

preflight_dependencies() {
  local tool
  for tool in curl wget python3 qemu-system-x86_64 nft iptables ip sudo; do
    if [ "$("$ENV_HANDLE" tool "$tool")" != "present" ]; then
      mock_state_out MISSING_DEPENDENCY "required tool '$tool' missing (apt-get install ...)"
    fi
  done
  # Host kernel modules the lab uses. A missing nfnetlink_queue is only a
  # note (the OpenWrt VM provides NFQUEUE via its kmods); a missing
  # nf_conntrack is a hard dependency for the integration phase.
  if [ "$("$ENV_HANDLE" module nf_conntrack)" != "present" ]; then
    mock_state_out MISSING_DEPENDENCY "kernel module 'nf_conntrack' missing (modprobe nf_conntrack)"
  fi
}

preflight_resources() {
  # NFQUEUE for the lab e2e is provided by the OpenWrt VM (kmod pins);
  # host-side absence is only a note, not a failure. Port checks are
  # early-warning only (the boot phase is authoritative).
  if [ "$("$ENV_HANDLE" nfqueue_proc)" != "present" ]; then
    say "note: host NFQUEUE absent (lab VM provides it via kmods)"
  fi
  local port
  for port in 5555 2222; do
    if [ "$("$ENV_HANDLE" port "$port")" = "occupied" ]; then
      mock_state_out RESOURCE "port ${port} is already in use"
    fi
  done
}

preflight() {
  # 1. CONFIG gate (malformed args/input)
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
# --- environment manifest (traceability) ---
# Written before the preflight so a blocked run still records the
# environment (the prompt requires diagnostics to land when integration
# is blocked). Best-effort: a missing tool only loses the manifest, not
# the run. In hermetic fixture mode the manifest is skipped entirely: a
# unit test must not execute host probes.
if [ -z "$FIXTURE" ]; then
  mkdir -p "$ARTIFACTS"
  MANIFEST="$ARTIFACTS/environment-manifest.txt"
  (bash "$ROOT/scripts/e2e-env-manifest.sh" --label "$(hostname)" 2>&1) > "$MANIFEST" || true
  say "environment manifest: $MANIFEST"
fi

# --- run the typed preflight (ordered gates) ---
export PATH="$PATH:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
preflight
if [ -n "${E2E_PREFLIGHT_ONLY:-}" ]; then
  say "preflight-only mode: stopping (state READY)"
  exit 0
fi

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
