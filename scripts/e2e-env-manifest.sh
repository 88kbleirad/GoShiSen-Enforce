#!/usr/bin/env bash
# GoShiSen-Enforce — e2e environment manifest.
#
# Prints a reproducible, machine-readable record of the environment the
# OpenWrt privileged e2e runs in: OS/kernel/arch, tool versions, tools
# capabilities, NFQUEUE module names/versions, pinned image/package
# checksums (from openwrt-image-prep.sh --print-config), repository
# commit, test command, and UTC timestamp.
#
# Usage:  bash scripts/e2e-env-manifest.sh [--json] [--label NAME]
#
# Exit codes: 0 on success (even when some optional probes are absent —
# those lines are marked "unavailable"); 2 when required tools used by
# the manifest itself are missing.
set -euo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

JSON=0
LABEL=""
while [ $# -gt 0 ]; do
  case "$1" in
    --json) JSON=1; shift ;;
    --label) LABEL=$2; shift 2 ;;
    *) echo "usage: $0 [--json] [--label NAME]" >&2; exit 2 ;;
  esac
done

have() { command -v "$1" >/dev/null 2>&1; }

# kv prints key: value (or JSON when --json).
kv() {
  local k=$1 v=$2
  if [ "$JSON" = 1 ]; then
    # minimal JSON string escaping (double quote, backslash)
    v=${v//\\/\\\\}
    v=${v//\"/\\\"}
    printf '  "%s": "%s",\n' "$k" "$v"
  else
    printf '%s: %s\n' "$k" "$v"
  fi
}

emit_header() {
  if [ "$JSON" = 1 ]; then
    echo "{"
  fi
}

emit_footer() {
  if [ "$JSON" = 1 ]; then
    # strip trailing comma and close
    echo "}"
  fi
}

need() { command -v "$1" >/dev/null 2>&1 || { echo "ERROR: $1 required for manifest" >&2; exit 2; }; }
need date; need uname; need git; need sha256sum

emit_header

if [ -n "$LABEL" ]; then kv label "$LABEL"; fi
kv timestamp "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
kv repo_commit "$(git -C "$ROOT" rev-parse HEAD 2>/dev/null || echo unavailable)"
kv repo_branch "$(git -C "$ROOT" branch --show-current 2>/dev/null || echo unavailable)"
kv test_command "${E2E_TEST_COMMAND:-scripts/e2e-runner.sh}"

kv os "$(uname -o 2>/dev/null || echo unavailable)"
kv kernel "$(uname -r 2>/dev/null || echo unavailable)"
kv arch "$(uname -m 2>/dev/null || echo unavailable)"
kv uid "$(id -u 2>/dev/null || echo unavailable)"

# Tools (version or unavailable).
for t in go qemu-system-x86_64 nft iptables ip python3 curl wget; do
  if have "$t"; then
    case "$t" in
      go) v=$(go version 2>&1 | head -1) ;;
      ip) v=$(ip -V 2>&1 | head -1) ;;
      python3) v=$(python3 --version 2>&1 | head -1) ;;
      *) v=$("$t" --version 2>&1 | head -1) ;;
    esac
    kv "${t}_version" "$v"
  else
    kv "${t}_version" unavailable
  fi
done
kv sudo_present "$(have sudo && echo yes || echo no)"
if have sudo; then
  if sudo -n true 2>/dev/null; then kv sudo_nopasswd yes; else kv sudo_nopasswd no; fi
fi

# Effective capabilities (when available); may be empty without capsh.
if have capsh; then
  kv capabilities "$(capsh --print 2>/dev/null | grep -E '^Current:' | head -1 || echo unavailable)"
else
  kv capabilities "capsh-unavailable"
fi

# NFQUEUE kernel support.
if [ -d /proc/net/netfilter ] && ls /proc/net/netfilter/nfnetlink_queue >/dev/null 2>&1; then
  kv nfqueue_proc present
else
  kv nfqueue_proc absent
fi
if [ -f /proc/modules ]; then
  mod=$(grep -E '^nfnetlink_queue ' /proc/modules 2>/dev/null | awk '{print $1, $3}' || true)
  if [ -n "$mod" ]; then kv nfqueue_module "$mod"; else kv nfqueue_module not-loaded; fi
else
  kv nfqueue_module unknown
fi

# Pinned image/package checksums from image-prep (no root, no network).
if [ -f "$ROOT/scripts/openwrt-image-prep.sh" ]; then
  printcfg=$("$ROOT/scripts/openwrt-image-prep.sh" --print-config 2>/dev/null || true)
  if [ -n "$printcfg" ]; then
    kv image_pin "$(printf '%s' "$printcfg" | grep -iE 'image|base' | head -2 | tr '\n' ';')"
    kv kmod_pins "$(printf '%s' "$printcfg" | grep -iE 'kmod|sha' | head -4 | tr '\n' ';')"
  else
    kv image_pin unavailable
  fi
else
  kv image_pin unavailable
fi

# Notable files (binary/image/apk) if present — traceability.
for f in /tmp/owrt.img /tmp/goshisen-static /tmp/goshisen-*.apk; do
  if [ -f "$f" ]; then
    kv "sha256_$(basename "$f")" "$(sha256sum "$f" 2>/dev/null | awk '{print $1}')"
  fi
done

emit_footer
