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

# Shared clean-stream assertion (one implementation for both gate tests):
# fixture 1's PASS must come from a real, fully decoded scan, not from an
# object-less or finding-less stream.
. "$(dirname "$0")/lib/govuln-clean-stream.sh"

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
assert_clean_govuln_stream "$TMP/goodbin" "current-toolchain payload" \
  || fail "the clean payload's govulncheck stream does not prove a real scan"

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

# ---------------------------------------------------------------------------
# Fixture 6+: govulncheck exit-code / output-shape matrix (stubbed tool)
#
# These pin the fail-closed rule set of scripts/lib/govuln-verdict.sh without
# needing a vulnerable binary or the vuln DB: a stub `govulncheck` on PATH
# reproduces each outcome, and the gate must decide it correctly.
# ---------------------------------------------------------------------------
STUB="$TMP/stub-bin"
mkdir -p "$STUB"
cat > "$STUB/govulncheck" <<'EOS'
#!/usr/bin/env bash
# Fixture stub: prints $STUB_OUT to stdout, exits $STUB_RC. Ignores its args.
[ -n "${STUB_ERR:-}" ] && printf '%s\n' "$STUB_ERR" >&2
[ -n "${STUB_OUT:-}" ] && printf '%s' "$STUB_OUT"
exit "${STUB_RC:-0}"
EOS
chmod +x "$STUB/govulncheck"

mkdir -p "$TMP/matrix/usr/sbin"
cp "$TMP/goodbin" "$TMP/matrix/usr/sbin/goshisen"
(cd "$TMP/matrix" && tar -czf "$TMP/matrix.apk" usr/sbin/goshisen)

stub_gate() { # stub_gate <rc> <stdout> [allowlist-file]
  local rc="$1" out="$2" allow="${3:-$ALLOW}"
  STUB_RC="$rc" STUB_OUT="$out" PATH="$STUB:$PATH" \
    bash "$GATE" "$TMP/matrix.apk" --allowlist "$allow" 2>&1
}

expect_stub_pass() { # expect_stub_pass <desc> <rc> <stdout> [allowlist]
  local desc="$1" rc="$2" out="$3" allow="${4:-$ALLOW}" res
  if res="$(stub_gate "$rc" "$out" "$allow")"; then
    echo "  ok: $desc -> PASS"
  else
    fail "$desc: expected PASS, got: $res"
  fi
}

expect_stub_fail() { # expect_stub_fail <desc> <needle> <rc> <stdout> [allowlist]
  local desc="$1" needle="$2" rc="$3" out="$4" allow="${5:-$ALLOW}" res
  if res="$(stub_gate "$rc" "$out" "$allow")"; then
    fail "$desc: expected FAIL, but the gate PASSED (got: $res)"
  fi
  grep -qF "$needle" <<<"$res" || fail "$desc: failed for the wrong reason (wanted '$needle', got: $res)"
  echo "  ok: $desc -> FAIL ($needle)"
}

echo "== govulncheck outcome matrix (stub) =="
# rc 0, fully parsed, no findings -> clean PASS
expect_stub_pass "exit 0 clean stream" 0 '{"config":{"protocol_version":"v1.0.0"}}'
# rc 0 WITH findings is the real-tool case (`-format json` always exits 0):
# an unlisted finding must FAIL and report the ID — this is the fail-open the
# shared verdict closed (the old copy reported PASS here when the IDs could
# not be parsed).
expect_stub_fail "exit 0 unlisted finding" "NOT allowlisted" 0 \
  '{"finding":{"osv":"GO-2026-1111"}}'
# ... and the same finding allowlisted must PASS and report it.
printf 'GO-2026-1111\n' > "$TMP/allow-1111.txt"
if res="$(stub_gate 0 '{"finding":{"osv":"GO-2026-1111"}}' "$TMP/allow-1111.txt")"; then
  grep -q "are allowlisted" <<<"$res" || fail "allowlisted exit-0 pass did not report the findings"
  grep -q "GO-2026-1111" <<<"$res" || fail "allowlisted exit-0 pass did not list the finding ID"
  echo "  ok: exit 0 allowlisted finding -> PASS (reported)"
else
  fail "exit 0 with an allowlisted finding was REJECTED: $res"
fi
# rc 0, a finding entry that carries no OSV id -> FAIL (schema change; the
# old copy treated an unparsable ID set as "no findings")
expect_stub_fail "exit 0 finding without OSV id" "no OSV id" 0 \
  '{"finding":{"trace":[{"module":"stdlib"}]}}'
# rc 0, empty output -> FAIL (an empty stream is never clean)
expect_stub_fail "exit 0 empty output" "no JSON object" 0 ''
# rc 3, unlisted finding -> FAIL and report the ID
expect_stub_fail "exit 3 unlisted finding" "NOT allowlisted" 3 \
  '{"finding":{"osv":"GO-2026-2222"}}'
# rc 3, allowlisted finding -> PASS and report it
printf 'GO-2026-3333\n' > "$TMP/allow-3333.txt"
if res="$(stub_gate 3 '{"finding":{"osv":"GO-2026-3333"}}' "$TMP/allow-3333.txt")"; then
  grep -q "are allowlisted" <<<"$res" || fail "allowlisted exit-3 pass did not report the findings"
  grep -q "GO-2026-3333" <<<"$res" || fail "allowlisted exit-3 pass did not list the finding ID"
  echo "  ok: exit 3 allowlisted finding -> PASS (reported)"
else
  fail "exit 3 with an allowlisted finding was REJECTED: $res"
fi
# rc 3, unparsable stream -> FAIL (parse failure, never clean)
expect_stub_fail "exit 3 unparsable stream" "could not be parsed" 3 'not json at all'
# rc 3, empty output -> FAIL (no JSON object at all)
expect_stub_fail "exit 3 empty output" "no JSON object" 3 ''
# rc 3, a parsed stream that carries no finding entry -> FAIL (exit 3 claims
# findings; zero parsed IDs is a parse failure, never a clean result)
expect_stub_fail "exit 3 zero parsed findings" "no finding IDs could be parsed" 3 \
  '{"config":{"protocol_version":"v1.0.0"}}'
# rc 3, truncated JSON object -> FAIL
expect_stub_fail "exit 3 truncated JSON" "could not be parsed" 3 '{"finding":{"osv":"GO-2026-4'
# rc 0, unparsable stream -> FAIL
expect_stub_fail "exit 0 unparsable stream" "could not be parsed" 0 'nonsense'
# rc 1 -> FAIL (scan error)
expect_stub_fail "exit 1" "exited 1" 1 ''
# rc 2 -> FAIL (scan error)
expect_stub_fail "exit 2" "exited 2" 2 ''
# rc 130 (killed/signal) -> FAIL
expect_stub_fail "exit 130" "exited 130" 130 ''
# rc 3 with a *directory* of allowlist matches but an empty allowlist -> FAIL
expect_stub_fail "exit 3 empty allowlist file" "NOT allowlisted" 3 \
  '{"finding":{"osv":"GO-2026-4444"}}' /dev/null

# missing / not-executable govulncheck must FAIL. PATH is narrowed for both
# cases: a full PATH would let bash's `command -v` skip the broken stub and
# resolve the real govulncheck, so the case would test nothing.
echo "== govulncheck unavailable =="
mkdir -p "$TMP/nogv"
printf '#!/bin/sh\nexit 0\n' > "$TMP/nogv/govulncheck"
chmod -x "$TMP/nogv/govulncheck"
if res="$(PATH="$TMP/nogv:/usr/bin:/bin" bash "$GATE" "$TMP/matrix.apk" --allowlist "$ALLOW" 2>&1)"; then
  fail "a non-executable govulncheck was ACCEPTED"
fi
grep -qE "not found|not executable" <<<"$res" || fail "missing-tool failure message unexpected: $res"
echo "  ok: non-executable govulncheck -> FAIL"
if res="$(PATH="$(dirname "$(command -v python3)"):/usr/bin:/bin" bash "$GATE" "$TMP/matrix.apk" --allowlist "$ALLOW" 2>&1)"; then
  fail "a missing govulncheck was ACCEPTED"
fi
grep -q "not found" <<<"$res" || fail "missing govulncheck message unexpected: $res"
echo "  ok: missing govulncheck -> FAIL"

# unreadable / empty payload must FAIL
echo "== payload problems =="
: > "$TMP/emptybin"
mkdir -p "$TMP/empty/usr/sbin"
cp "$TMP/emptybin" "$TMP/empty/usr/sbin/goshisen"
(cd "$TMP/empty" && tar -czf "$TMP/empty.apk" usr/sbin/goshisen)
if PATH="$STUB:$PATH" STUB_RC=0 bash "$GATE" "$TMP/empty.apk" --allowlist "$ALLOW" >/dev/null 2>&1; then
  fail "an empty payload was ACCEPTED"
fi
echo "  ok: empty payload -> FAIL"
mkdir -p "$TMP/noperm/usr/sbin"
printf 'x' > "$TMP/noperm/usr/sbin/goshisen"
chmod 000 "$TMP/noperm/usr/sbin/goshisen"
(cd "$TMP/noperm" && tar -czf "$TMP/noperm.apk" usr/sbin/goshisen) 2>/dev/null || true
if [ -f "$TMP/noperm.apk" ] && [ "$(id -u)" -ne 0 ]; then
  if PATH="$STUB:$PATH" STUB_RC=0 bash "$GATE" "$TMP/noperm.apk" --allowlist "$ALLOW" >/dev/null 2>&1; then
    # tar preserves the mode; the gate must refuse an unreadable payload
    fail "an unreadable payload was ACCEPTED"
  fi
  echo "  ok: unreadable payload -> FAIL"
else
  echo "  note: unreadable-payload case skipped (running as root: -r always true)"
fi
[ -f "$TMP/notgo.apk" ] && if PATH="$STUB:$PATH" STUB_RC=0 bash "$GATE" "$TMP/notgo.apk" --allowlist "$ALLOW" >/dev/null 2>&1; then
  fail "a non-Go payload was ACCEPTED with a clean stub"
fi
echo "  ok: non-Go payload -> FAIL"

# positive control: the REAL govulncheck on the REAL current-toolchain payload
# must still PASS (fixture 1 above already asserts it; repeat here with the
# real tool explicitly, so a stub-only regression cannot hide)
if ! bash "$GATE" "$TMP/good.apk" --allowlist "$ALLOW" >/dev/null 2>&1; then
  fail "positive control (real govulncheck, current payload) was REJECTED"
fi
echo "  ok: real govulncheck on the current payload -> PASS"

echo "S-005-APK-PAYLOAD-GOVULN-GATE-GUARD-PASS"
