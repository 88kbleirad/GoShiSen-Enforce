#!/usr/bin/env bash
# check-backup-key.sh — verify a LOCAL signing-key file against the committed
# trust anchors, on your own machine, without CI and without ever printing key
# material (only sizes, a label, and public fingerprints).
#
# Usage:
#   scripts/check-backup-key.sh --key <private-key.pem> [--anchor <pub.pem>] [-q]
#
#   --key FILE      the key you are about to paste into GH_RELEASE_SIGNING_KEY
#   --anchor FILE   expected public anchor
#                   (default: keys/goshisen-release.pub.pem)
#   --expect HEX    expected SHA-256(DER SPKI) of the key's public half
#                   (default: the production CLI fingerprint 24215e01...)
#   -q, --quiet     print nothing on success (exit status only)
#
# Exit codes:
#   0  the key matches the expected anchor (safe to set as the secret)
#   1  readable key, but it is NOT the expected key (names the anchor it matches)
#   2  usage error / file missing
#   3  not a readable PEM private key (quoted/escaped/corrupted/encrypted)
#
# Nothing here talks to the network and nothing writes the key anywhere.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
KEY=""
ANCHOR="$ROOT/keys/goshisen-release.pub.pem"
QUIET=0
# Single source of truth for the expected CLI fingerprint (rotation = replace
# this data file + the anchor, no code edit).
FP_FILE="$ROOT/keys/release-anchor-fingerprint.txt"
if [ -s "$FP_FILE" ]; then
  EXPECTED=$(tr -d '[:space:]' < "$FP_FILE")
else
  echo "FAIL: expected-fingerprint file '$FP_FILE' missing/empty (pass --expect)" >&2
  exit 2
fi

usage() { sed -n '2,20p' "$0"; }

while [ $# -gt 0 ]; do
  case "$1" in
    --key) KEY=${2:-}; shift 2 ;;
    --anchor) ANCHOR=${2:-}; shift 2 ;;
    --expect) EXPECTED=${2:-}; shift 2 ;;
    -q|--quiet) QUIET=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "usage: $0 --key FILE [--anchor FILE] [-q]" >&2; exit 2 ;;
  esac
done

[ -n "$KEY" ] || { echo "usage: $0 --key FILE [--anchor FILE] [-q]" >&2; exit 2; }
[ -f "$KEY" ] || { echo "FAIL: key file '$KEY' not found" >&2; exit 2; }
[ -f "$ANCHOR" ] || { echo "FAIL: anchor '$ANCHOR' not found" >&2; exit 2; }

say() { [ "$QUIET" = 1 ] || printf '%s\n' "$*"; }

# --- structure of the file (counts and a label only, never contents) ---
bytes=$(wc -c < "$KEY" | tr -d ' ')
lines=$(wc -l < "$KEY" | tr -d ' ')
cr=$(grep -c $'\r' "$KEY" || true)
label=$(sed -nE 's/^["'"'"']?-----BEGIN ([A-Z0-9 ]+)-----.*/\1/p' "$KEY" | head -n1 || true)
quoted=no
case "$(head -c 1 "$KEY")" in '"'|"'") quoted=yes ;; esac
# trailing whitespace = the file does not end with a newline but ends with
# a space/tab/CR (a clean PEM always ends with a newline)
lastc=$(tail -c 1 "$KEY" | tr -d '\n')
trailing=no
if [ -n "$lastc" ]; then
  case "$lastc" in ' '|"$(printf '\t')"|"$(printf '\r')") trailing=yes ;; esac
fi

say "file:     $KEY"
say "size:     ${bytes} bytes, ${lines} newline(s), CR=${cr}, quoted=$quoted, trailing-whitespace=$trailing"
say "header:   ${label:-<none>}"

if ! openssl pkey -in "$KEY" -noout >/dev/null 2>&1; then
  reason=$(openssl pkey -in "$KEY" -noout 2>&1 | head -n1 || true)
  echo "FAIL: '$KEY' is not a readable PEM private key" >&2
  [ -n "$reason" ] && echo "      openssl says: $reason" >&2
  if [ "$quoted" = yes ] || [ "$trailing" = yes ]; then
    echo "      The file itself carries quotes/extra whitespace — strip them before using it" >&2
    echo "      (the release path tolerates this, but the file should be clean)." >&2
  fi
  say "      For reference: an Ed25519 PKCS#8 PEM is 119 bytes over 3 lines."

  if printf '%s' "$(cat "$KEY")" | grep -q -- '-----BEGIN ENCRYPTED PRIVATE KEY-----'; then
    echo "      This is a PASSPHRASE-ENCRYPTED key; export it unencrypted." >&2
  fi
  exit 3
fi

# --- public fingerprint and anchor comparison (public data only) ---
fp=$(openssl pkey -in "$KEY" -pubout -outform DER | sha256sum | awk '{print $1}')
say "pubkey:   sha256(DER SPKI) = $fp"

if [ "$fp" = "$EXPECTED" ]; then
  say "MATCH:    this is the production CLI release key (24215e01...)"
  say "          next: gh secret set GH_RELEASE_SIGNING_KEY --env release-cli --repo 88kbleirad/GoShiSen-Enforce < <file>"
  exit 0
fi

derived="$(mktemp)"
trap 'rm -f "$derived"' EXIT
openssl pkey -in "$KEY" -pubout -out "$derived" 2>/dev/null
match=""
for a in "$(dirname "$ANCHOR")"/*.pub.pem; do
  [ -f "$a" ] || continue
  if cmp -s "$derived" "$a"; then match="${match:+$match, }$(basename "$a")"; fi
done

if [ -n "$match" ]; then
  echo "FAIL: readable key, but it is NOT the CLI release key — its public half matches: $match" >&2
else
  echo "FAIL: readable key, but its public half matches none of the anchors in $(dirname "$ANCHOR")" >&2
fi
echo "      expected fingerprint $EXPECTED ($(basename "$ANCHOR"))" >&2
echo "      Do not set this value as GH_RELEASE_SIGNING_KEY; find the CLI key backup." >&2
exit 1
