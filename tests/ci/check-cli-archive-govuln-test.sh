#!/usr/bin/env bash
# check-cli-archive-govuln-test.sh — S-005 regression test for the CLI
# archive govulncheck gate (scripts/check-cli-archive-govuln.sh).
#
# Fixtures:
#   good.tar.gz   — binary built with the current (>= MIN_GO_VERSION)
#                   toolchain → must PASS.
#   oldgo.tar.gz  — binary built with go1.25.14 (the published CLI
#                   toolchain; 23 binary-mode findings) → must FAIL with
#                   the blocked-findings list.
#   allow.tar.gz  — same old-go binary, allowlist containing the
#                   findings' GO IDs → must PASS and REPORT them.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
GATE="$ROOT/scripts/check-cli-archive-govuln.sh"
ALLOW="$ROOT/packaging/govulncheck-allowlist.txt"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }

command -v govulncheck >/dev/null 2>&1 || fail "govulncheck not installed (needed for S-005 CLI gate tests)"
command -v jq >/dev/null 2>&1 || fail "jq not installed (needed for S-005 CLI gate tests)"

# ---------------------------------------------------------------------------
# Fixture 1: current-toolchain binary (must pass)
# ---------------------------------------------------------------------------
go build -trimpath -buildvcs=false -ldflags "-s -w" \
  -o "$TMP/goodbin" "$ROOT/cmd/goshisen"
mkdir -p "$TMP/good/goshisen-linux-amd64"
cp "$TMP/goodbin" "$TMP/good/goshisen-linux-amd64/goshisen"
(cd "$TMP/good" && tar -czf "$TMP/good.tar.gz" goshisen-linux-amd64)
if ! bash "$GATE" "$TMP/good.tar.gz" --allowlist "$ALLOW" >/dev/null 2>&1; then
  fail "current-toolchain CLI archive was REJECTED"
fi

# ---------------------------------------------------------------------------
# Fixture 2: go1.25.14 binary (must fail; this is the S-005 CLI finding)
# ---------------------------------------------------------------------------
mkdir -p "$TMP/oldgo-src" && cd "$TMP/oldgo-src"
cat > go.mod <<'EOF'
module oldgo-fixture

go 1.25.0
EOF
cat > main.go <<'EOF'
package main
func main() {}
EOF
GOTOOLCHAIN=go1.25.14 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$TMP/oldgobin" . \
  >"$TMP/oldgo-build.log" 2>&1 || fail "could not build go1.25.14 fixture (see $TMP/oldgo-build.log)"
cd "$TMP"
mkdir -p "$TMP/oldgo/goshisen-linux-amd64"
cp "$TMP/oldgobin" "$TMP/oldgo/goshisen-linux-amd64/goshisen"
(cd "$TMP/oldgo" && tar -czf "$TMP/oldgo.tar.gz" goshisen-linux-amd64)

if bash "$GATE" "$TMP/oldgo.tar.gz" --allowlist "$ALLOW" >/dev/null 2>&1; then
  fail "go1.25.14 CLI archive was ACCEPTED (must fail closed)"
fi
out="$(bash "$GATE" "$TMP/oldgo.tar.gz" --allowlist "$ALLOW" 2>&1 || true)"
echo "$out" | grep -q "NOT allowlisted" \
  || fail "old-go failure did not report the blocked findings"

# ---------------------------------------------------------------------------
# Fixture 3: allowlisted findings (must pass AND be reported)
# ---------------------------------------------------------------------------
govulncheck -mode=binary -format json "$TMP/oldgobin" 2>/dev/null \
  | jq -r 'select(has("finding")) | .finding.osv' | sort -u > "$TMP/allow.txt"
if [ ! -s "$TMP/allow.txt" ]; then
  fail "old-go fixture unexpectedly produced no finding IDs"
fi
out="$(bash "$GATE" "$TMP/oldgo.tar.gz" --allowlist "$TMP/allow.txt" 2>&1)" \
  || fail "allowlisted findings were not accepted"
echo "$out" | grep -q "are allowlisted" || fail "allowlist pass did not report the findings"

echo "S-005-CLI-ARCHIVE-GOVULN-GATE-GUARD-PASS"
