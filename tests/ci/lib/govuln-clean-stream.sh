#!/usr/bin/env bash
# tests/ci/lib/govuln-clean-stream.sh — shared assertion for the S-005 gate
# tests: the "current toolchain" control must pass the gate because a REAL
# scan found nothing reachable, not because the JSON stream was empty, was
# truncated, or carried no finding records at all (an object-less stream
# already fails the gate itself, but a control that never looks at the stream
# cannot tell the two apart).
#
# SOURCED, never executed: defines one function, sets no variables.
#
# assert_clean_govuln_stream <binary> <label>
#   Runs `govulncheck -mode=binary -format json` on <binary>, stores the stream
#   in "<binary>.govulncheck.jsonl", and requires it to decode cleanly with at
#   least one `osv` record and zero `finding` records. Prints the counts and
#   returns non-zero with the reason on stderr otherwise.
assert_clean_govuln_stream() {
  local bin="$1" label="$2" json="$1.govulncheck.jsonl"
  govulncheck -mode=binary -format json "$bin" >"$json" 2>"$json.err" \
    || { echo "FAIL: govulncheck could not scan the $label: $(head -n1 "$json.err")" >&2; return 1; }
  python3 - "$json" "$label" <<'PY'
import json
import sys

path, label = sys.argv[1], sys.argv[2]
dec = json.JSONDecoder()
with open(path, encoding="utf-8", errors="replace") as handle:
    buf = handle.read()
idx = osv = findings = 0
while idx < len(buf):
    while idx < len(buf) and buf[idx] in " \t\r\n":
        idx += 1
    if idx >= len(buf):
        break
    try:
        obj, idx = dec.raw_decode(buf, idx)
    except json.JSONDecodeError:
        print("FAIL: %s: govulncheck JSON stream truncated at byte %d" % (label, idx), file=sys.stderr)
        sys.exit(1)
    if isinstance(obj, dict):
        if isinstance(obj.get("finding"), dict):
            findings += 1
        elif obj.get("osv"):
            osv += 1
print("  clean-scan stream (%s): osv=%d findings=%d" % (label, osv, findings))
if osv < 1:
    print("FAIL: %s: the clean-scan stream carries no `osv` record — a PASS on it would not prove a real scan ran" % label, file=sys.stderr)
    sys.exit(1)
if findings != 0:
    print("FAIL: %s: the clean-scan stream carries %d `finding` record(s)" % (label, findings), file=sys.stderr)
    sys.exit(1)
PY
}
