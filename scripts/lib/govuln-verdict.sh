# shellcheck shell=bash
# scripts/lib/govuln-verdict.sh — shared verdict logic for the S-005
# govulncheck gates.
#
#   scripts/check-apk-payload-govuln.sh   scans the OpenWrt apk payload
#   scripts/check-cli-archive-govuln.sh   scans a CLI/daemon release archive
#
# This file is SOURCED, never executed: it defines functions, sets no
# variables, and exits nowhere.
#
# Why it exists: both gates carried a copy of the same decision code, and that
# copy could report PASS on a scan that DID find vulnerabilities whenever the
# finding IDs could not be parsed. The mechanism: `"${arr[@]:-}"` over an EMPTY
# array expands to one empty word, so the allowlist comparison ran
# `[ "" = "" ]` and "matched"; and `mapfile` reading a stream that produced no
# IDs left one empty element, so the "zero findings" branch never matched
# either. One implementation, one set of regression fixtures — the outcome
# matrix in tests/ci/check-apk-payload-govuln-test.sh pins the shared rule set,
# tests/ci/check-cli-archive-govuln-test.sh covers the CLI path end to end.
#
# Exit codes (govulncheck v1.9.0, cmd/govulncheck/doc.go "Exit codes"): with
# `-format json` govulncheck exits 0 REGARDLESS of the number of detected
# vulnerabilities; exit 3 is only produced by the non-JSON formats. Both gates
# run `-format json`, so findings normally arrive with rc 0 — the verdict
# therefore keys off the PARSED stream, not off rc. rc 3 is still handled (in
# case a caller switches format); every other rc is a scan error.
#
# Verdict rule set (fail-closed):
#   rc not in {0, 3}                                    -> FAIL (scan error)
#   stream not parsed / no JSON object decoded          -> FAIL
#   finding objects that carry no OSV id                -> FAIL (schema change)
#   rc 0 + parsed + >= 1 object + zero findings          -> PASS (clean)
#   rc 0 + findings, EVERY one on the reviewed allowlist -> PASS (findings
#        reported on stdout either way)
#   rc 0 + findings, any one not allowlisted             -> FAIL
#   rc 3 + zero parsed findings                          -> FAIL (never clean)
#   unreadable/reported-empty govulncheck, unreadable payload
#   or unreadable stream file                            -> FAIL

# govuln_require_tool — fail closed when govulncheck is absent or unusable.
govuln_require_tool() {
  local gv
  gv="$(command -v govulncheck 2>/dev/null || true)"
  if [ -z "$gv" ]; then
    govuln_fail "'govulncheck' not found (install golang.org/x/vuln/cmd/govulncheck, pinned version; see ci.yml)"
    return 1
  fi
  if [ ! -x "$gv" ]; then
    govuln_fail "'govulncheck' at '$gv' is not executable"
    return 1
  fi
  return 0
}

govuln_fail() { echo "FAIL: $*" >&2; }
govuln_ok() { echo "OK: $*"; }

# govuln_parse_stream <stream-file> <out-file>
# Writes a line-oriented summary of the JSON stream govulncheck produced:
#   parsed yes|no
#   error <first parse error, if any>
#   objects <n>            number of decoded JSON objects (progress,
#                          config, SBOM, osv and finding entries alike)
#   finding-no-osv <n>     finding entries with no usable OSV id (only
#                          printed when > 0)
#   finding <GO-YYYY-NNNN> one line per distinct finding, sorted
# Returns non-zero only when python3 itself could not run.
govuln_parse_stream() {
  local stream="$1" out="$2"
  python3 - "$stream" >"$out" <<'PY'
import json
import sys

path = sys.argv[1]
try:
    with open(path, encoding="utf-8", errors="replace") as handle:
        buf = handle.read()
except OSError as exc:
    print("parsed no")
    print("error cannot read stream: %s" % exc)
    sys.exit(0)

dec = json.JSONDecoder()
idx = 0
objects = 0
no_osv = 0
findings = set()
parsed = True
err = ""
while True:
    while idx < len(buf) and buf[idx] in " \t\r\n":
        idx += 1
    if idx >= len(buf):
        break
    try:
        obj, idx = dec.raw_decode(buf, idx)
    except json.JSONDecodeError as exc:
        parsed = False
        err = "JSON decode error at byte %d: %s" % (idx, exc.msg)
        break
    objects += 1
    if isinstance(obj, dict) and isinstance(obj.get("finding"), dict):
        osv = obj["finding"].get("osv")
        if osv:
            findings.add(str(osv))
        else:
            no_osv += 1
print("parsed %s" % ("yes" if parsed else "no"))
if err:
    print("error %s" % err)
print("objects %d" % objects)
if no_osv:
    print("finding-no-osv %d" % no_osv)
for osv in sorted(findings):
    print("finding %s" % osv)
PY
}

# govuln_verdict <rc> <parse-file> <allowlist> <subject> [stderr-file]
# Prints the verdict and returns 0 only for a trustworthy PASS.
govuln_verdict() {
  local rc="$1" parse="$2" allow="$3" subject="$4" errfile="${5:-}"
  local parsed err_line objects no_osv detail=""
  local -a findings=() allowed=() blocked=()

  if [ ! -r "$parse" ]; then
    govuln_fail "cannot read the parsed govulncheck summary ('$parse') for $subject"
    return 1
  fi
  parsed="$(sed -n 's/^parsed //p' "$parse" | head -n1)"
  err_line="$(sed -n 's/^error //p' "$parse" | head -n1)"
  objects="$(sed -n 's/^objects //p' "$parse" | head -n1)"
  no_osv="$(sed -n 's/^finding-no-osv //p' "$parse" | head -n1)"
  mapfile -t findings < <(sed -n 's/^finding //p' "$parse" | sort -u)
  objects="${objects:-0}"
  no_osv="${no_osv:-0}"

  if [ "$rc" -ne 0 ] && [ "$rc" -ne 3 ]; then
    if [ -n "$errfile" ] && [ -s "$errfile" ]; then
      detail=": $(head -n1 "$errfile")"
    fi
    govuln_fail "govulncheck exited $rc for $subject (0 = scan completed, 3 = findings reported; treating $rc as a scan failure)$detail"
    return 1
  fi

  [ "$parsed" = yes ] \
    || { govuln_fail "govulncheck output for $subject could not be parsed (${err_line:-no error detail}) — refusing to call it clean"; return 1; }
  if [ "$objects" -eq 0 ]; then
    govuln_fail "govulncheck produced no JSON object for $subject (govulncheck exited $rc) — an empty stream is never a clean result"
    return 1
  fi
  if [ "$no_osv" -ne 0 ]; then
    govuln_fail "govulncheck reported $no_osv finding entry(ies) with no OSV id for $subject — the output schema changed; refusing to call it clean"
    return 1
  fi

  if [ "${#findings[@]}" -eq 0 ]; then
    if [ "$rc" -eq 3 ]; then
      govuln_fail "govulncheck exited 3 for $subject but no finding IDs could be parsed — treating as a parse failure (never as a clean result)"
      return 1
    fi
    govuln_ok "govulncheck binary-mode found no reachable vulnerabilities in $subject"
    return 0
  fi

  if [ -f "$allow" ]; then
    mapfile -t allowed < <(sed -E 's/[[:space:]]*#.*$//' "$allow" | grep -oE 'GO-[0-9]+-[0-9]+' | sort -u || true)
  fi

  local f a keep
  for f in "${findings[@]}"; do
    keep=1
    if [ "${#allowed[@]}" -gt 0 ]; then
      for a in "${allowed[@]}"; do
        if [ "$f" = "$a" ]; then keep=0; break; fi
      done
    fi
    if [ "$keep" = 1 ]; then blocked+=("$f"); fi
  done

  if [ "${#blocked[@]}" -eq 0 ]; then
    govuln_ok "govulncheck binary-mode findings are allowlisted in $allow:"
    printf '  %s\n' "${findings[@]}"
    return 0
  fi

  govuln_fail "govulncheck binary-mode found reachable vulnerabilities NOT allowlisted in $subject:"
  printf '  %s\n' "${blocked[@]}"
  echo "(allowlist: $allow; findings IDs are GO-<year>-<n>, see pkg.go.dev/vuln/<id>)" >&2
  return 1
}
