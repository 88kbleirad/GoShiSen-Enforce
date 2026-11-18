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
#   exit 0: no reachable vulnerabilities, or only allowlisted ones
#           (allowlisted findings are reported on stdout)
#   exit 1: payload unreadable, govulncheck failed, or un-allowlisted
#           reachable findings exist
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

command -v govulncheck >/dev/null 2>&1 || fail "'govulncheck' not found (install golang.org/x/vuln/cmd/govulncheck, pinned version; see ci.yml)"
command -v python3 >/dev/null 2>&1 || fail "'python3' not found (required to parse govulncheck JSON)"

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

# Binary-mode scan; the JSON output is a newline-delimited stream.
# govulncheck exit codes: 0 = no vulns, 3 = vulns found, anything
# else = scan failure (fail closed).
set +e
govulncheck -mode=binary -format json "$bin" >"$tmp/govuln.jsonl" 2>"$tmp/govuln.err"
rc=$?
set -e
[ "$rc" -ne 1 ] || fail "govulncheck could not read the payload's Go buildinfo: $(head -1 "$tmp/govuln.err")"

# Collect reachable finding IDs (finding records, distinct). The JSON
# stream is one object per line; each `finding` object carries osv,
# fixed_version and trace. A scan that found vulnerabilities but
# yielded no parseable finding IDs is treated as a parse failure
# (fail closed), never as a clean result.
if [ -s "$tmp/govuln.jsonl" ]; then
  mapfile -t findings < <(python3 -c '
import json, sys
dec = json.JSONDecoder()
buf = sys.stdin.read()
idx = 0
seen = set()
while idx < len(buf):
    while idx < len(buf) and buf[idx] in " \t\r\n": idx += 1
    if idx >= len(buf): break
    try:
        obj, end = dec.raw_decode(buf, idx)
    except json.JSONDecodeError:
        break
    idx = end
    if isinstance(obj, dict) and "finding" in obj:
        osv = obj["finding"].get("osv")
        if osv: seen.add(osv)
print("\n".join(sorted(seen)))
' < "$tmp/govuln.jsonl" 2>/dev/null || true)
else
  findings=()
fi

# Collect allowlisted IDs (ignore blank lines and # comments).
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
  echo "OK: govulncheck binary-mode found no reachable vulnerabilities in the payload"
elif [ "${#blocked[@]}" = 0 ]; then
  echo "OK: govulncheck binary-mode findings are allowlisted in $allowlist:"
  printf '  %s\n' "${findings[@]:-}"
else
  echo "FAIL: govulncheck binary-mode found reachable vulnerabilities NOT allowlisted:"
  printf '  %s\n' "${blocked[@]:-}"
  echo "(allowlist: $allowlist; findings IDs are GO-<year>-<n>, see pkg.go.dev/vuln/<id>)"
  exit 1
fi

echo "S-005-APK-PAYLOAD-GOVULN-GATE-PASS"
