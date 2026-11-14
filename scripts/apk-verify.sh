#!/usr/bin/env bash
# GoShiSen-Enforce — verify OpenWrt .apk packages against a trust store.
#
# Fail-closed verification of v3 APK signatures using apk-tools `verify`
# with a keys directory (the package-manager trust store analogue of
# /etc/apk/keys). Unsigned or wrong-key packages are rejected.
#
# Exit codes:
#   0  all apks verified (trusted signature + optional sha256 match)
#   1  usage / missing trust anchor
#   2  an apk file missing
#   4  an apk is UNTRUSTED or fails the optional recorded-sha256 check
#
# `apk verify` validates the embedded v3 package signature and the
# package's internal hashes. It does NOT compare the full file bytes
# against a recorded build hash, so appends after the data stream can go
# unnoticed. When --sha256 is passed (release/provenance flow), a full
# file hash comparison is enforced too (fail-closed on tampering).
#
# Usage:
#   apk-verify.sh --keys-dir /etc/apk/keys goshisen-0.2.0-r5.apk [...]
#   apk-verify.sh --key ANCHOR.pub goshisen-0.2.0-r5.apk
#   apk-verify.sh --sha256 <file>=<hash> [...]  (repeatable)
set -euo pipefail

APK="${APK_TOOL:-apk}"
KEYS_DIR=""
KEY=""
APKS=()
declare -A EXPECT_SHA256

usage() {
  echo "usage: $0 (--keys-dir DIR|--key FILE) [--sha256 file=hash] FILE.apk [...]" >&2
  exit 1
}

while [ $# -gt 0 ]; do
  case "$1" in
    --keys-dir) KEYS_DIR="$2"; shift 2 ;;
    --key) KEY="$2"; shift 2 ;;
    --sha256)
      pair="$2"; f="${pair%%=*}"; h="${pair#*=}"
      [ -n "$f" ] && [ -n "$h" ] || usage
      EXPECT_SHA256["$f"]="$h"
      shift 2 ;;
    -*) usage ;;
    *) APKS+=("$1"); shift ;;
  esac
done

[ "${#APKS[@]}" -gt 0 ] || usage

tmpdir=""
if [ -n "$KEYS_DIR" ]; then
  [ -d "$KEYS_DIR" ] || { echo "ERROR: keys dir $KEYS_DIR missing" >&2; exit 1; }
  kd="$KEYS_DIR"
elif [ -n "$KEY" ]; then
  [ -f "$KEY" ] || { echo "ERROR: key $KEY missing" >&2; exit 1; }
  tmpdir="$(mktemp -d)"; trap 'rm -rf "$tmpdir"' EXIT
  cp "$KEY" "$tmpdir/anchor.pub"
  kd="$tmpdir"
else
  usage
fi

rc=0
for a in "${APKS[@]}"; do
  [ -f "$a" ] || { echo "ERROR: apk $a not found" >&2; exit 2; }
  out="$("$APK" verify --keys-dir "$kd" "$a" 2>&1 || true)"
  if echo "$out" | grep -q "UNTRUSTED"; then
    echo "VERIFY-FAIL: $a: $out" >&2
    rc=4
  elif echo "$out" | grep -q ": OK"; then
    if [ -n "${EXPECT_SHA256["$a"]:-}" ]; then
      got=$(sha256sum "$a" | awk '{print $1}')
      if [ "$got" != "${EXPECT_SHA256["$a"]}" ]; then
        echo "VERIFY-FAIL: $a: sha256 mismatch (recorded ${EXPECT_SHA256["$a"]}, got $got)" >&2
        rc=4
      else
        echo "OK: $a (trusted signature + sha256 match)"
      fi
    else
      echo "OK: $a (trusted signature)"
    fi
  else
    echo "VERIFY-FAIL: $a: unexpected output: $out" >&2
    rc=4
  fi
done
exit "$rc"
