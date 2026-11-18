#!/usr/bin/env bash
# check-apk-payload-go.sh — S-005 gate: fail-closed check that an
# OpenWrt apk's payload daemon (`usr/sbin/goshisen`) was built with a
# Go toolchain >= the required minimum version (default 1.27.4).
#
# Background (S-005, docs/security-assessment-20261117.md): the apk
# payload is built by the OpenWrt SDK's `golang/host` feed, NOT by CI's
# Go. The published v0.2.5 apk embedded go1.27.1, whose stdlib has 11
# reachable vulnerabilities (GO-2026-6859, 6610/6611/6612/6613/6617,
# 6603/6605/6607/6608, 6809). The feed now pins go1.27.4. This gate
# verifies the actual payload's embedded buildinfo before the apk can
# be signed, so a rebuilt-with-old-Go apk cannot pass the signing path.
#
# The pre-signing apk is unsigned (no APK Sig Block), so the payload is
# extracted with plain `tar`. If that fails (e.g. a signed apk), a
# `APK_TOOL` (apk-tools v3) fallback is tried; either way failure fails
# closed.
#
# Usage: bash scripts/check-apk-payload-go.sh <apk> [--min 1.27.4]
#   exit 0: payload Go version >= min (and readable)
#   exit 1: payload missing, unreadable, or built with an older Go
set -euo pipefail

apk="${1:?usage: check-apk-payload-go.sh <apk> [--min 1.27.4]}"
# Resolve to an absolute path: the APK_TOOL fallback runs from a temp
# cwd (cd "$tmp"), so a relative path (as the workflows pass) would
# break there.
case "$apk" in
  /*) ;;
  *) apk="$PWD/$apk" ;;
esac
min="1.27.4"
# Single source of truth for the minimum: packaging/go-toolchain.env
# (S-005 gate policy). An explicit --min still overrides.
policy="$(cd "$(dirname "$0")/.." && pwd)/packaging/go-toolchain.env"
if [ -f "$policy" ]; then
  v="$(grep -E '^MIN_GO_VERSION=' "$policy" | head -1 | cut -d= -f2- || true)"
  if [[ "$v" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    min="$v"
  fi
fi
if [[ "${2:-}" == "--min" ]]; then
  min="${3:?usage: --min needs a version like 1.27.4}"
fi

fail() { echo "FAIL: $*" >&2; exit 1; }

command -v go >/dev/null 2>&1 || fail "'go' toolchain required (install Go >= $min)"

[ -f "$apk" ] || fail "apk '$apk' not found"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

if ! tar -xzf "$apk" -C "$tmp" usr/sbin/goshisen 2>/dev/null; then
  if [[ -n "${APK_TOOL:-}" ]] && [ -x "$APK_TOOL" ]; then
    # Same invocation as apk-sign.yml's approval-summary extract
    # (apk-tools v3, unsigned staged apk: no signature to distrust).
    (cd "$tmp" && "$APK_TOOL" extract --allow-untrusted "$apk" >/dev/null 2>&1) \
      || fail "cannot extract payload from '$apk' (tar and APK_TOOL both failed)"
  else
    fail "cannot extract payload from '$apk' (tar failed; set APK_TOOL to apk-tools v3)"
  fi
fi

bin="$tmp/usr/sbin/goshisen"
[ -f "$bin" ] || fail "payload usr/sbin/goshisen not present in '$apk'"

# Primary read: go version -m. Fallback: embedded Go buildinfo block
# (stripped static binaries built with -buildid= can defeat `go
# version -m`; `strings` reliably exposes "Go buildinf:" + version).
ver=""
if out="$(go version -m "$bin" 2>&1)"; then
  ver="$(grep -oE 'go[0-9]+\.[0-9]+\.[0-9]+' <<<"$out" | head -1)"
fi
if [[ -z "$ver" ]]; then
  ver="$(strings -n 8 "$bin" 2>/dev/null \
    | grep -A1 'Go buildinf:' | tail -1 \
    | grep -oE 'go[0-9]+\.[0-9]+\.[0-9]+' || true)"
fi
[[ "$ver" =~ ^go[0-9]+\.[0-9]+\.[0-9]+$ ]] \
  || fail "payload '$apk' has no readable Go buildinfo (not a Go binary?): $out"

# The minimum version, prefixed the same way (goX.Y.Z), sorts at or
# before the payload version iff payload >= min (sort -V semantics).
if [[ "$(printf '%s\n' "go$min" "$ver" | sort -V | head -1)" != "go$min" ]]; then
  fail "payload '$apk' built with $ver, but S-005 requires Go >= $min"
fi

echo "OK: payload '$apk' built with $ver (>= $min)"
echo "S-005-APK-PAYLOAD-GO-GATE-PASS"
