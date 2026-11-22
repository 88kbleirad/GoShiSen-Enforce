#!/usr/bin/env bash
# check-backup-key-test.sh — regression tests for scripts/check-backup-key.sh
# (the offline, value-safe local check for the F-02 CLI signing key backup).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
CHECK="$ROOT/scripts/check-backup-key.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }

# scratch keypairs
openssl genpkey -algorithm ed25519 -out "$TMP/cli.pem" 2>/dev/null
openssl pkey -in "$TMP/cli.pem" -pubout -out "$TMP/cli.pub.pem" 2>/dev/null
openssl genpkey -algorithm ed25519 -out "$TMP/other.pem" 2>/dev/null
openssl pkey -in "$TMP/other.pem" -pubout -out "$TMP/other.pub.pem" 2>/dev/null
FP=$(openssl pkey -in "$TMP/cli.pem" -pubout -outform DER | sha256sum | awk '{print $1}')
mkdir -p "$TMP/anchors"
cp "$TMP/cli.pub.pem" "$TMP/anchors/goshisen-release.pub.pem"
cp "$TMP/other.pub.pem" "$TMP/anchors/goshisen-apk.pub.pem"

# 1) positive: key matches its expected fingerprint -> exit 0
"$CHECK" --key "$TMP/cli.pem" --expect "$FP" >/dev/null \
  || fail "a key matching its expected fingerprint was rejected"

# 2) negative: readable key, wrong fingerprint, but an anchors dir match -> 1
set +e
out="$("$CHECK" --key "$TMP/other.pem" --anchor "$TMP/anchors/goshisen-release.pub.pem" 2>&1)"
rc=$?
set -e
[ "$rc" -eq 1 ] || fail "wrong-key case exited $rc, expected 1: $out"
case "$out" in
  *"matches: goshisen-apk.pub.pem"*) ;;
  *) fail "wrong-key case did not name the matching anchor: $out" ;;
esac

# 3) negative: unreadable (quoted) file -> 3 with hints
{ printf '"'; cat "$TMP/cli.pem"; printf ' "'; } > "$TMP/quoted.pem"
set +e
out="$("$CHECK" --key "$TMP/quoted.pem" 2>&1)"
rc=$?
set -e
[ "$rc" -eq 3 ] || fail "quoted case exited $rc, expected 3"
case "$out" in
  *"not a readable PEM private key"*) ;;
  *) fail "quoted case: no unreadable diagnosis: $out" ;;
esac
case "$out" in
  *"quotes/extra whitespace"*) ;;
  *) fail "quoted case: no strip hint: $out" ;;
esac

# 4) negative: missing file -> 2
set +e
"$CHECK" --key "$TMP/nope.pem" >/dev/null 2>&1
rc=$?
set -e
[ "$rc" -ne 0 ] || fail "missing file accepted"
[ "$rc" -eq 2 ] || fail "missing-file case exited $rc, expected 2"

echo "CHECK-BACKUP-KEY-TEST-PASS"
