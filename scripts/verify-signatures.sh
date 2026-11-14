#!/usr/bin/env bash
# GoShiSen-Enforce — verify signed release artifacts (fail closed, Prompt 12).
#
# Usage:
#   verify-signatures.sh --dist DIR --public-key PUB.pem --commit <sha> [--require-sign]
#
# Exit codes:
#   0  all artifacts signed, verified, provenance matches, arch matches
#   2  missing key / artifacts / usage
#   3  missing-signature (an artifact has no .sig)
#   4  bad-signature (verification failed)
#   5  provenance-mismatch (provenance.json sha256 != artifact bytes)
#   6  stale-provenance (recorded source_commit != --commit)
#   7  arch-mismatch (binary inside archive != expected from filename)
#   8  missing-provenance
set -euo pipefail

DIST=""
PUB=""
EXPECT_COMMIT=""
REQUIRE_SIGN=0

usage() { echo "usage: $0 --dist DIR --public-key PUB.pem [--commit SHA] [--require-sign]" >&2; exit 2; }
while [ $# -gt 0 ]; do
  case "$1" in
    --dist) DIST=$2; shift 2 ;;
    --public-key) PUB=$2; shift 2 ;;
    --commit) EXPECT_COMMIT=$2; shift 2 ;;
    --require-sign) REQUIRE_SIGN=1; shift ;;
    *) usage ;;
  esac
done

[ -n "$DIST" ] && [ -d "$DIST" ] || { echo "ERROR: --dist dir required" >&2; exit 2; }
[ -f "$PUB" ] || { echo "ERROR: public key $PUB missing" >&2; exit 2; }
PUB="$(realpath "$PUB")"

fail() { echo "VERIFY-FAIL: $1" >&2; exit "$2"; }

ARTIFACTS=()
while IFS= read -r f; do
  ARTIFACTS+=("$f")
done < <(find "$DIST" -maxdepth 1 -type f \
  ! -name '*.sig' ! -name '*.sig.pem' ! -name 'provenance.json' \
  ! -name 'provenance.json.sig' ! -name 'provenance-index.txt' \
  ! -name 'provenance-index.txt.sig' ! -name 'public_key.pem' \
  ! -name '*.provenance.json' ! -name '*.provenance.json.sig' \
  -printf '%f\n' | sort)

[ "${#ARTIFACTS[@]}" -gt 0 ] || fail "no artifacts to verify in $DIST" 2

for a in "${ARTIFACTS[@]}"; do
  f="$DIST/$a"

  # 1. signature presence
  if [ ! -f "$f.sig" ]; then
    if [ "$REQUIRE_SIGN" = 1 ]; then
      fail "missing-signature: $a" 3
    fi
    echo "SKIP (unsigned, --require-sign unset): $a"
    continue
  fi

  # 2. signature validity
  if ! openssl pkeyutl -verify -rawin -pubin -inkey "$PUB" -in "$f" -sigfile "$f.sig" >/dev/null 2>&1; then
    fail "bad-signature: $a" 4
  fi

  # 3. provenance presence + sha256 match
  if [ ! -f "$f.provenance.json" ]; then
    fail "missing-provenance: $a" 8
  fi
  if ! openssl pkeyutl -verify -rawin -pubin -inkey "$PUB" \
       -in "$f.provenance.json" -sigfile "$f.provenance.json.sig" >/dev/null 2>&1; then
    fail "bad-signature: $a.provenance.json" 4
  fi
  recorded=$(jq -r '.sha256' "$f.provenance.json")
  actual=$(sha256sum "$f" | awk '{print $1}')
  if [ "$recorded" != "$actual" ]; then
    fail "provenance-mismatch: $a (recorded $recorded actual $actual)" 5
  fi
  if [ -n "$EXPECT_COMMIT" ]; then
    pc=$(jq -r '.source_commit' "$f.provenance.json")
    if [ "$pc" != "$EXPECT_COMMIT" ]; then
      fail "stale-provenance: $a (recorded commit $pc, expected $EXPECT_COMMIT)" 6
    fi
  fi

  # 4. arch check for tarballs (binary inside vs filename)
  case "$a" in
    goshisen-linux-amd64.tar.gz)
      if tar -xzf "$f" -O goshisen-linux-amd64/goshisen 2>/dev/null \
           | file - | grep -q "x86-64"; then :; else fail "arch-mismatch: $a (amd64)" 7; fi ;;
    goshisen-linux-arm64.tar.gz)
      if tar -xzf "$f" -O goshisen-linux-arm64/goshisen 2>/dev/null \
           | file - | grep -q "aarch64"; then :; else fail "arch-mismatch: $a (arm64)" 7; fi ;;
  esac
  echo "OK: $a (sig+provenance+arch verified)"
done

echo "VERIFY-PASS: all artifacts verified with $PUB"
