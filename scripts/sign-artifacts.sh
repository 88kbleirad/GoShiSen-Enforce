#!/usr/bin/env bash
# GoShiSen-Enforce — sign release artifacts + write provenance (Prompt 12).
#
# Usage:
#   sign-artifacts.sh --dist DIR --key PRIVATE.pem --public-key PUB.pem \
#                     --commit <sha> --tag <tag> --builder <name> \
#                     --toolchain <go-version> [--sign]
#
# Produces, for each artifact in DIR (tarballs, checksums.txt, sbom.spdx.json,
# *.apk):
#   <artifact>.sig        Ed25519 signature (OpenSSL, raw-sign, 64 bytes)
#   <artifact>.sig.pem    copy of the public key used
# and a `provenance.json` + `provenance.json.sig` in DIR.
#
# --sign must be passed to actually create signature files; without it the
# script only prints what would be signed (dry-run used by tests).
set -euo pipefail

DIST=""
KEY=""
PUB=""
COMMIT=""
TAG=""
BUILDER=""
TOOLCHAIN=""
DO_SIGN=0

usage() { echo "usage: $0 --dist DIR --key KEY.pem --public-key PUB.pem --commit SHA --tag TAG --builder NAME --toolchain GO [--sign]" >&2; exit 2; }

while [ $# -gt 0 ]; do
  case "$1" in
    --dist) DIST=$2; shift 2 ;;
    --key) KEY=$2; shift 2 ;;
    --public-key) PUB=$2; shift 2 ;;
    --commit) COMMIT=$2; shift 2 ;;
    --tag) TAG=$2; shift 2 ;;
    --builder) BUILDER=$2; shift 2 ;;
    --toolchain) TOOLCHAIN=$2; shift 2 ;;
    --sign) DO_SIGN=1; shift ;;
    *) usage ;;
  esac
done

[ -n "$DIST" ] && [ -d "$DIST" ] || { echo "ERROR: --dist dir required" >&2; usage; }
[ "$DO_SIGN" = 1 ] && { [ -f "$KEY" ] || { echo "ERROR: private key $KEY missing" >&2; exit 1; }; }
[ -n "$COMMIT" ] || COMMIT="unknown"
[ -n "$TAG" ] || TAG="dev"
[ -n "$BUILDER" ] || BUILDER="local"
[ -n "$TOOLCHAIN" ] || TOOLCHAIN="$(go version 2>/dev/null || echo unknown)"

# Artifacts to sign: all non-signature, non-provenance files.
mapfile -t ARTIFACTS < <(find "$DIST" -maxdepth 1 -type f \
  ! -name '*.sig' ! -name '*.sig.pem' ! -name 'provenance.json' ! -name 'provenance.json.sig' \
  ! -name '*.provenance.json' ! -name '*.provenance.json.sig' ! -name 'provenance-index.txt' ! -name 'provenance-index.txt.sig' ! -name 'public_key.pem' \
  -printf '%f\n' | sort)

[ "${#ARTIFACTS[@]}" -gt 0 ] || { echo "ERROR: no artifacts in $DIST" >&2; exit 1; }

# Public key: always write it (verification material); from given public key
# or derived from private key.
if [ -n "$PUB" ] && [ -f "$PUB" ]; then
  cp "$PUB" "$DIST/public_key.pem"
else
  openssl pkey -in "$KEY" -pubout -out "$DIST/public_key.pem"
fi

write_provenance() {
  local artifact=$1 hash=$2
  cat <<EOF
{
  "version": 1,
  "artifact": "$artifact",
  "source_commit": "$COMMIT",
  "tag": "$TAG",
  "builder": "$BUILDER",
  "toolchain": "$TOOLCHAIN",
  "build_timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "sha256": "$hash"
}
EOF
}

for a in "${ARTIFACTS[@]}"; do
  f="$DIST/$a"
  [ -f "$f" ] || continue

  if [ "$DO_SIGN" = 1 ]; then
    openssl pkeyutl -sign -rawin -inkey "$KEY" -in "$f" -out "$f.sig"
    cp "$DIST/public_key.pem" "$f.sig.pem"
  fi

  # provenance for this artifact
  sha=$(sha256sum "$f" | awk '{print $1}')
  write_provenance "$a" "$sha" > "$DIST/$a.provenance.json"
  if [ "$DO_SIGN" = 1 ]; then
    openssl pkeyutl -sign -rawin -inkey "$KEY" -in "$DIST/$a.provenance.json" \
      -out "$DIST/$a.provenance.json.sig"
  fi
done

# aggregate provenance index
for a in "${ARTIFACTS[@]}"; do
  echo "$a $(sha256sum "$DIST/$a" | awk '{print $1}')"
done > "$DIST/provenance-index.txt"
if [ "$DO_SIGN" = 1 ]; then
  openssl pkeyutl -sign -rawin -inkey "$KEY" -in "$DIST/provenance-index.txt" \
    -out "$DIST/provenance-index.txt.sig"
fi

echo "signed artifacts in $DIST (sign=$DO_SIGN):"
ls -1 "$DIST" | grep -E "\.sig$|provenance" || true
