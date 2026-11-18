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
#   exit 0: no reachable vulnerabilities, or only allowlisted ones
#           (allowlisted findings are reported on stdout)
#   exit 1: archive missing/unreadable, govulncheck failed, or
#           un-allowlisted reachable findings exist
set -euo pipefail

archive="${1:?usage: check-cli-archive-govuln.sh <archive.tar.gz> [--allowlist path]}"
allowlist="${GOVULN_ALLOWLIST:-$(cd "$(dirname "$0")/.." && pwd)/packaging/govulncheck-allowlist.txt}"
if [[ "${2:-}" == "--allowlist" ]]; then
  allowlist="${3:?usage: --allowlist needs a path}"
fi

fail() { echo "FAIL: $*" >&2; exit 1; }

command -v govulncheck >/dev/null 2>&1 || fail "'govulncheck' not found (install golang.org/x/vuln/cmd/govulncheck, pinned version; see ci.yml)"
command -v jq >/dev/null 2>&1 || fail "'jq' not found (required to parse govulncheck JSON)"

[ -f "$archive" ] || fail "archive '$archive' not found"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

tar -xzf "$archive" -C "$tmp" || fail "cannot extract '$archive'"
bin="$(find "$tmp" -type f -name goshisen | head -1)"
[ -n "$bin" ] && [ -f "$bin" ] || fail "no goshisen binary in '$archive'"

# Binary-mode scan; JSON output is a newline-delimited stream.
# govulncheck exit codes: 0 = no vulns, 3 = vulns found, anything
# else = scan failure (fail closed).
set +e
govulncheck -mode=binary -format json "$bin" >"$tmp/govuln.jsonl" 2>"$tmp/govuln.err"
rc=$?
set -e
[ "$rc" -ne 1 ] || fail "govulncheck could not read the binary's Go buildinfo: $(head -1 "$tmp/govuln.err")"

if [ -s "$tmp/govuln.jsonl" ]; then
  mapfile -t findings < <(jq -r 'select(has("finding")) | .finding.osv' "$tmp/govuln.jsonl" 2>/dev/null | sort -u || true)
else
  findings=()
fi

if [ -f "$allowlist" ]; then
  mapfile -t allowed < <(sed -E 's/[[:space:]]*#.*$//' "$allowlist" | grep -oE 'GO-[0-9]+-[0-9]+' | sort -u || true)
else
  allowed=()
fi

blocked=()
for f in "${findings[@]:-}"; do
  keep=1
  for a in "${allowed[@]:-}"; do
    if [ "$f" = "$a" ]; then keep=0; break; fi
  done
  if [ "$keep" = 1 ]; then blocked+=("$f"); fi
done

if [ "$rc" = 0 ] && [ "${#findings[@]}" = 0 ]; then
  echo "OK: govulncheck binary-mode found no reachable vulnerabilities in $(basename "$archive")"
elif [ "${#blocked[@]}" = 0 ]; then
  echo "OK: govulncheck binary-mode findings are allowlisted in $allowlist:"
  printf '  %s\n' "${findings[@]:-}"
else
  echo "FAIL: govulncheck binary-mode found reachable vulnerabilities NOT allowlisted in $(basename "$archive"):"
  printf '  %s\n' "${blocked[@]:-}"
  echo "(allowlist: $allowlist; findings IDs are GO-<year>-<n>, see pkg.go.dev/vuln/<id>)"
  exit 1
fi

echo "S-005-CLI-ARCHIVE-GOVULN-GATE-PASS"
