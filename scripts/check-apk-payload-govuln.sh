#!/usr/bin/env bash
# check-apk-payload-govuln.sh — S-005 gate: fail-closed govulncheck
# (binary mode) on the OpenWrt apk payload daemon (`usr/sbin/goshisen`).
#
# Why: the apk payload is built by the OpenWrt SDK's golang feed, NOT
# by CI's Go, so CI's govulncheck run does not cover it. This gate
# scans the actual payload binary and fails on ANY reachable
# stdlib or module vulnerability, unless the finding is explicitly
# allowlisted in packaging/govulncheck-allowlist.txt (empty by
# default; adding an entry requires maintainer review).
#
# Binary mode requires the payload to carry Go buildinfo. The SDK
# recipe must therefore NOT strip it (do not pass -buildid= to the Go
# linker); a payload without readable buildinfo fails closed here.
#
# Usage: bash scripts/check-apk-payload-govuln.sh <apk> \
#          [--allowlist path/to/allowlist.txt]
#   exit 0: a fully parsed, non-empty stream with no reachable finding, or
#           with findings that are ALL on the reviewed allowlist (findings are
#           reported on stdout either way)
#   exit 1: payload missing/unreadable/empty, govulncheck missing or not
#           executable, stream unparsable or empty, a finding entry with no
#           OSV id, an un-allowlisted finding, or an exit code other than 0/3.
#           (Verdicts are decided by scripts/lib/govuln-verdict.sh, shared with
#           the CLI-archive gate. Note that `-format json` always exits 0, so
#           findings normally arrive on rc 0 — the verdict keys off the parsed
#           stream, not off rc.)
set -euo pipefail

apk="${1:?usage: check-apk-payload-govuln.sh <apk> [--allowlist path]}"
# Resolve to an absolute path: the APK_TOOL fallback runs from a temp
# cwd (cd "$tmp"), so a relative path (as the workflows pass) would
# break there.
case "$apk" in
  /*) ;;
  *) apk="$PWD/$apk" ;;
esac
allowlist="${GOVULN_ALLOWLIST:-$(cd "$(dirname "$0")/.." && pwd)/packaging/govulncheck-allowlist.txt}"
if [[ "${2:-}" == "--allowlist" ]]; then
  allowlist="${3:?usage: --allowlist needs a path}"
fi

fail() { echo "FAIL: $*" >&2; exit 1; }

# Shared verdict logic (one implementation for both S-005 govuln gates):
# scripts/lib/govuln-verdict.sh. It fails closed on every non-clean outcome.
ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=lib/govuln-verdict.sh
. "$ROOT_DIR/scripts/lib/govuln-verdict.sh"

command -v python3 >/dev/null 2>&1 || fail "'python3' not found (required to parse govulncheck output)"
govuln_require_tool || exit 1

[ -f "$apk" ] || fail "apk '$apk' not found"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

if ! tar -xzf "$apk" -C "$tmp" usr/sbin/goshisen 2>/dev/null; then
  if [[ -n "${APK_TOOL:-}" ]] && [ -x "$APK_TOOL" ]; then
    (cd "$tmp" && "$APK_TOOL" extract --allow-untrusted "$apk" >/dev/null 2>&1) \
      || fail "cannot extract payload from '$apk' (tar and APK_TOOL both failed)"
  else
    fail "cannot extract payload from '$apk' (tar failed; set APK_TOOL to apk-tools v3)"
  fi
fi

bin="$tmp/usr/sbin/goshisen"
[ -f "$bin" ] || fail "payload usr/sbin/goshisen not present in '$apk'"
[ -r "$bin" ] || fail "payload '$bin' is not readable"
[ -s "$bin" ] || fail "payload '$bin' is empty"

# Binary-mode scan. The verdict rule set lives in scripts/lib/govuln-verdict.sh
# and fails closed: an unparsable or empty stream, a finding entry without an
# OSV id, any un-allowlisted finding, and any exit code other than 0/3 all fail
# the gate. govulncheck exits 0 under `-format json` even when it found
# vulnerabilities, so the stream — not rc — carries the findings.
set +e
govulncheck -mode=binary -format json "$bin" >"$tmp/govuln.jsonl" 2>"$tmp/govuln.err"
rc=$?
set -e

govuln_parse_stream "$tmp/govuln.jsonl" "$tmp/parse.txt" \
  || fail "cannot parse the govulncheck output for '$apk' (python3 did not run)"
govuln_verdict "$rc" "$tmp/parse.txt" "$allowlist" "$(basename "$apk")" "$tmp/govuln.err" || exit 1

echo "S-005-APK-PAYLOAD-GOVULN-GATE-PASS"
