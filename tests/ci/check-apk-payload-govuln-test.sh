#!/usr/bin/env bash
# check-apk-payload-govuln-test.sh — S-005 regression test for the
# payload govulncheck gate (scripts/check-apk-payload-govuln.sh).
#
# Fixtures:
#   good.apk    — payload built with the current (>= MIN_GO_VERSION)
#                 toolchain, -s -w, buildid kept → must PASS.
#   oldgo.apk   — payload built with go1.27.1 (the published v0.2.5
#                 apk toolchain; 11 reachable stdlib vulns at that
#                 version) → must FAIL with the blocked-findings list.
#   allow.apk   — same old-go payload, allowlist containing the
#                 findings' GO IDs → must PASS and REPORT them.
# For the old-go fixture the test fetches the go1.27.1 toolchain via
# GOTOOLCHAIN (as the release toolchain does through setup-go). The
# scan itself is then fully offline (vuln DB is a local cache under
# $TMP, seeded by a preceding online DB download — see docs).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
GATE="$ROOT/scripts/check-apk-payload-govuln.sh"
ALLOW="$ROOT/packaging/govulncheck-allowlist.txt"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }

command -v govulncheck >/dev/null 2>&1 || fail "govulncheck not installed (needed for S-005 payload gate tests)"
command -v python3 >/dev/null 2>&1 || fail "python3 not installed (needed for S-005 payload gate tests)"

# ---------------------------------------------------------------------------
# Fixture 1: current-toolchain payload (must pass)
# ---------------------------------------------------------------------------
go build -trimpath -buildvcs=false -ldflags "-s -w" \
  -o "$TMP/goodbin" "$ROOT/cmd/goshisen"
mkdir -p "$TMP/good/usr/sbin"
cp "$TMP/goodbin" "$TMP/good/usr/sbin/goshisen"
(cd "$TMP/good" && tar -czf "$TMP/good.apk" usr/sbin/goshisen)
if ! bash "$GATE" "$TMP/good.apk" --allowlist "$ALLOW" >/dev/null 2>&1; then
  fail "current-toolchain payload was REJECTED"
fi

# ---------------------------------------------------------------------------
# Fixture 2: go1.27.1 payload (must fail; this is the S-005 finding)
# ---------------------------------------------------------------------------
GOTOOLCHAIN=go1.27.1 CGO_ENABLED=0 \
  go build -trimpath -buildvcs=false -ldflags "-s -w" \
  -o "$TMP/oldgobin" ./cmd/goshisen \
  >"$TMP/oldgo-build.log" 2>&1 \
  || fail "could not build go1.27.1 fixture (see $TMP/oldgo-build.log)"
mkdir -p "$TMP/oldgo/usr/sbin"
cp "$TMP/oldgobin" "$TMP/oldgo/usr/sbin/goshisen"
(cd "$TMP/oldgo" && tar -czf "$TMP/oldgo.apk" usr/sbin/goshisen)

if bash "$GATE" "$TMP/oldgo.apk" --allowlist "$ALLOW" >/dev/null 2>&1; then
  fail "go1.27.1 payload was ACCEPTED (must fail closed)"
fi
out="$(bash "$GATE" "$TMP/oldgo.apk" --allowlist "$ALLOW" 2>&1 || true)"
echo "$out" | grep -q "NOT allowlisted" \
  || fail "old-go failure did not report the blocked findings"

# ---------------------------------------------------------------------------
# Fixture 3: allowlisted findings (must pass AND be reported)
# ---------------------------------------------------------------------------
govulncheck -mode=binary -format json "$TMP/oldgobin" 2>/dev/null \
  | python3 -c '
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
' | sort -u > "$TMP/allow.txt"
if [ ! -s "$TMP/allow.txt" ]; then
  fail "old-go fixture unexpectedly produced no finding IDs"
fi
out="$(bash "$GATE" "$TMP/oldgo.apk" --allowlist "$TMP/allow.txt" 2>&1)" \
  || fail "allowlisted findings were not accepted"
echo "$out" | grep -q "are allowlisted" || fail "allowlist pass did not report the findings"

# ---------------------------------------------------------------------------
# Fixture 4: non-Go payload (must fail; no buildinfo)
# ---------------------------------------------------------------------------
echo "not a go binary" > "$TMP/notgobin"
mkdir -p "$TMP/notgo/usr/sbin"
cp "$TMP/notgobin" "$TMP/notgo/usr/sbin/goshisen"
(cd "$TMP/notgo" && tar -czf "$TMP/notgo.apk" usr/sbin/goshisen)
if bash "$GATE" "$TMP/notgo.apk" --allowlist "$ALLOW" >/dev/null 2>&1; then
  fail "non-Go payload was ACCEPTED"
fi

# ---------------------------------------------------------------------------
# Fixture 5: payload-less apk (must fail)
# ---------------------------------------------------------------------------
mkdir -p "$TMP/nopayload/usr/share"
echo x > "$TMP/nopayload/usr/share/other"
(cd "$TMP/nopayload" && tar -czf "$TMP/nopayload.apk" usr/share/other)
if bash "$GATE" "$TMP/nopayload.apk" --allowlist "$ALLOW" >/dev/null 2>&1; then
  fail "payload-less apk was ACCEPTED"
fi

echo "S-005-APK-PAYLOAD-GOVULN-GATE-GUARD-PASS"
