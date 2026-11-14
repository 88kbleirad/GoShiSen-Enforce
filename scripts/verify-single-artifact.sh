#!/usr/bin/env bash
# GoShiSen-Enforce — verify one artifact's Ed25519 signature (fail closed).
#
# Usage:
#   verify-single-artifact.sh ARTIFACT ARTIFACT.sig PUB.pem
#
# Exit codes:
#   0  signature present and valid for the artifact
#   1  missing signature file
#   2  usage / missing key
#   4  bad signature (openSSL verification failed)
#
# Used by the production install path (tarballs and .apk) before installing:
# the install MUST NOT proceed unless this exits 0.
set -euo pipefail

[ $# -eq 3 ] || { echo "usage: $0 ARTIFACT SIG PUB.pem" >&2; exit 2; }
ART=$1; SIG=$2; PUB=$3

[ -f "$ART" ] || { echo "ERROR: artifact $ART missing" >&2; exit 2; }
[ -f "$PUB" ] || { echo "ERROR: public key $PUB missing" >&2; exit 2; }
[ -f "$SIG" ] || { echo "FAIL: missing signature $SIG (unsigned artifact)" >&2; exit 1; }

if openssl pkeyutl -verify -rawin -pubin -inkey "$PUB" \
     -in "$ART" -sigfile "$SIG" >/dev/null 2>&1; then
  echo "OK: $ART signature verified"
else
  echo "FAIL: bad signature for $ART" >&2
  exit 4
fi
