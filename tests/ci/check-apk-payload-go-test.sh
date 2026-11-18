#!/usr/bin/env bash
# check-apk-payload-go-test.sh — S-005 regression test for the payload
# Go-version gate (scripts/check-apk-payload-go.sh).
#
# Fixtures are built with the host Go toolchain (1.27.4 in CI). The
# positive case must pass; the negative cases (older toolchain version,
# Go buildinfo, missing payload) must each exit non-zero fail-closed.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
GATE="$ROOT/scripts/check-apk-payload-go.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }

# Build a real static Go binary (the release build recipe: -trimpath
# -buildvcs=false, stripped, -buildid= as in openwrt/Makefile).
go build -trimpath -buildvcs=false \
  -ldflags "-s -w -buildid= -X github.com/88kbleirad/GoShiSen-Enforce/internal/cli.Version=0.2.5-test" \
  -o "$TMP/payload" "$ROOT/cmd/goshisen"

mkdir -p "$TMP/usr/sbin"
cp "$TMP/payload" "$TMP/usr/sbin/goshisen"

# --- positive: current toolchain (1.27.4 in CI) must pass ---
(cd "$TMP" && tar -czf good.apk usr/sbin/goshisen)
if ! bash "$GATE" "$TMP/good.apk" >/dev/null 2>&1; then
  fail "current-toolchain payload was REJECTED"
fi

# --- negative: the gate must reject a payload whose buildinfo claims an
# --- older toolchain (go1.27.1 — the published v0.2.5 apk's toolchain)
printf 'Go buildinf:\ngo1.27.1\npath\tgithub.com/88kbleirad/GoShiSen-Enforce\n' \
  > "$TMP/usr/sbin/goshisen"
(cd "$TMP" && tar -czf oldgo.apk usr/sbin/goshisen)
if bash "$GATE" "$TMP/oldgo.apk" >/dev/null 2>&1; then
  fail "go1.27.1 payload was ACCEPTED"
fi

# --- negative: a non-Go payload (no buildinfo) must be rejected ---
echo "not a go binary" > "$TMP/usr/sbin/goshisen"
(cd "$TMP" && tar -czf notgo.apk usr/sbin/goshisen)
if bash "$GATE" "$TMP/notgo.apk" >/dev/null 2>&1; then
  fail "non-Go payload was ACCEPTED"
fi

# --- negative: apk with no usr/sbin/goshisen must be rejected ---
rm -rf "$TMP/usr"
mkdir -p "$TMP/usr/share"
echo x > "$TMP/usr/share/other"
(cd "$TMP" && tar -czf nopayload.apk usr/share/other)
if bash "$GATE" "$TMP/nopayload.apk" >/dev/null 2>&1; then
  fail "payload-less apk was ACCEPTED"
fi

# --- negative: missing apk file must be rejected ---
if bash "$GATE" "$TMP/nope.apk" >/dev/null 2>&1; then
  fail "missing apk was ACCEPTED"
fi

echo "S-005-APK-PAYLOAD-GO-GATE-GUARD-PASS"
