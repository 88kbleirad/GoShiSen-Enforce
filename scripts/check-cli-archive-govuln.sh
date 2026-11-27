#!/usr/bin/env bash
# check-cli-archive-govuln.sh — S-005 gate: fail-closed govulncheck
# (binary mode) on each CLI/daemon release archive binary
# (`goshisen-linux-<arch>/goshisen` inside `goshisen-linux-<arch>.tar.gz`).
#
# Why: the CLI archives are built by release.yml's `make package` with
# the pinned Go (go-version 1.27.4, GOTOOLCHAIN=local), but nothing
# enforces the RESULT. This gate scans the actual shipped binaries and
# fails on ANY reachable stdlib or module vulnerability unless the
# finding is explicitly allowlisted in
# packaging/govulncheck-allowlist.txt (empty by default; adding an
# entry requires maintainer review).
#
# Usage: bash scripts/check-cli-archive-govuln.sh <archive.tar.gz> [--allowlist path]
#   exit 0: a fully parsed, non-empty stream with no reachable finding, or
#           with findings that are ALL on the reviewed allowlist (findings are
#           reported on stdout either way)
#   exit 1: archive/binary missing/unreadable/empty, govulncheck missing or not
#           executable, stream unparsable or empty, a finding entry with no
#           OSV id, an un-allowlisted finding, or an exit code other than 0/3.
#           (Verdicts are decided by scripts/lib/govuln-verdict.sh, shared with
#           the apk payload gate. Note that `-format json` always exits 0, so
#           findings normally arrive on rc 0 — the verdict keys off the parsed
#           stream, not off rc.)
set -euo pipefail

archive="${1:?usage: check-cli-archive-govuln.sh <archive.tar.gz> [--allowlist path]}"
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

[ -f "$archive" ] || fail "archive '$archive' not found"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

tar -xzf "$archive" -C "$tmp" || fail "cannot extract '$archive'"
bin="$(find "$tmp" -type f -name goshisen | head -1)"
[ -n "$bin" ] && [ -f "$bin" ] || fail "no goshisen binary in '$archive'"
[ -r "$bin" ] || fail "binary '$bin' is not readable"
[ -s "$bin" ] || fail "binary '$bin' is empty"

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
  || fail "cannot parse the govulncheck output for '$archive' (python3 did not run)"
govuln_verdict "$rc" "$tmp/parse.txt" "$allowlist" "$(basename "$archive")" "$tmp/govuln.err" || exit 1

echo "S-005-CLI-ARCHIVE-GOVULN-GATE-PASS"
