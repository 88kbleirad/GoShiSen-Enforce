#!/usr/bin/env bash
# GoShiSen-Enforce — verify a private key matches a public trust anchor.
#
# Usage:
#   check-key-match.sh KEY.pem PUB.pem
#
# Exit codes:
#   0  public key derived from KEY.pem is byte-identical to PUB.pem
#   1  mismatch (different key pair) or openssl failure
#   2  usage error
#
# Used by the release workflow before signing to guarantee the secret key
# corresponds to the committed public key (fingerprint-equivalence check).
set -euo pipefail

[ $# -eq 2 ] || { echo "usage: $0 KEY.pem PUB.pem" >&2; exit 2; }
[ -f "$1" ] || { echo "ERROR: private key $1 missing" >&2; exit 2; }
[ -f "$2" ] || { echo "ERROR: public key $2 missing" >&2; exit 2; }

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

openssl pkey -in "$1" -pubout -out "$tmp/pub.pem" 2>/dev/null \
  || { echo "ERROR: cannot derive public key from $1" >&2; exit 1; }

cmp -s "$tmp/pub.pem" "$2" \
  || { echo "ERROR: key mismatch: $1 does not match $2" >&2; exit 1; }

echo "OK: private key matches public key $2"
