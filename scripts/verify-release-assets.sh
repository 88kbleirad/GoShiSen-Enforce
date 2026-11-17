#!/usr/bin/env bash
# GoShiSen-Enforce — verify a full release's assets, fail closed.
#
# The release carries two independent trust domains:
#   - CLI assets (tarballs, checksums.txt, sbom.spdx.json, provenance):
#     Ed25519 release key (public_key.pem asset), verified by
#     scripts/verify-signatures.sh.
#   - OpenWrt apk: EC P-256 anchor (keys/goshisen-apk.pub.pem, fingerprint
#     542bb7ae...), verified by apk-tools via scripts/apk-verify.sh.
#
# checksums.txt deliberately lists only the CLI tarballs (the apk is
# signed with a different key and is not CLI-signed). This script closes
# the resulting gap — "apk not in checksums.txt" — by verifying every
# release asset in one place: CLI signatures, checksums vs tarballs, and
# the apk signature + recorded sha256.
#
# Usage:
#   verify-release-assets.sh --dir DIR \
#       --cli-key public_key.pem \
#       --apk-anchor keys/goshisen-apk.pub.pem \
#       [--commit SHA] [--apk-sha256 <hash>]
#
# Exit codes (mirror verify-signatures.sh / apk-verify.sh):
#   0  all assets verified
#   2  usage / missing key / missing artifact
#   3  missing signature (verify-signatures.sh)
#   4  bad signature / apk UNTRUSTED / apk sha256 mismatch
#   5  provenance mismatch
#   6  stale provenance (--commit mismatch)
#   7  arch mismatch (binary inside archive != filename)
#   8  missing provenance
set -euo pipefail

DIR=""
CLI_KEY=""
APK_ANCHOR=""
EXPECT_COMMIT=""
EXPECT_APK_SHA=""

usage() {
  echo "usage: $0 --dir DIR --cli-key PUB.pem --apk-anchor PUB.pem [--commit SHA] [--apk-sha256 HASH]" >&2
  exit 2
}
while [ $# -gt 0 ]; do
  case "$1" in
    --dir) DIR=$2; shift 2 ;;
    --cli-key) CLI_KEY=$2; shift 2 ;;
    --apk-anchor) APK_ANCHOR=$2; shift 2 ;;
    --commit) EXPECT_COMMIT=$2; shift 2 ;;
    --apk-sha256) EXPECT_APK_SHA=$2; shift 2 ;;
    *) usage ;;
  esac
done

[ -n "$DIR" ] && [ -d "$DIR" ] || usage
[ -n "$CLI_KEY" ] && [ -f "$CLI_KEY" ] || { echo "ERROR: --cli-key file required" >&2; exit 2; }
[ -n "$APK_ANCHOR" ] && [ -f "$APK_ANCHOR" ] || { echo "ERROR: --apk-anchor file required" >&2; exit 2; }

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# 1. CLI trust domain: signatures + provenance + arch, fail closed.
#    The apk is NOT a CLI artifact (different key, different trust
#    domain); stage the CLI assets in a temp dir so verify-signatures
#    never sees it.
CLI_TMP="$(mktemp -d)"; trap 'rm -rf "$CLI_TMP"' EXIT
cp -a "$DIR"/. "$CLI_TMP"/
# The apk is NOT a CLI artifact: remove it from the staged copy only.
find "$CLI_TMP" -maxdepth 1 -type f -name 'goshisen-*.apk' -delete
CLI_ARGS=(--dist "$CLI_TMP" --public-key "$CLI_KEY")
[ -n "$EXPECT_COMMIT" ] && CLI_ARGS+=(--commit "$EXPECT_COMMIT")
"$HERE/verify-signatures.sh" "${CLI_ARGS[@]}" --require-sign

# 2. checksums.txt matches the CLI tarballs (checksums.txt is CLI-signed,
#    so a mismatch here means a broken or tampered release).
if [ -f "$DIR/checksums.txt" ]; then
  (cd "$DIR" && grep -E 'goshisen-linux-(amd64|arm64)\.tar\.gz' checksums.txt | sha256sum -c -)
fi

# 3. APK trust domain: signature vs anchor + optional recorded sha256.
APK=$(find "$DIR" -maxdepth 1 -name 'goshisen-*.apk' -type f | head -n1 || true)
if [ -n "$APK" ]; then
  APK_ARGS=(--key "$APK_ANCHOR")
  [ -n "$EXPECT_APK_SHA" ] && APK_ARGS+=(--sha256 "$APK=$EXPECT_APK_SHA")
  APK_TOOL="${APK_TOOL:-apk}" "$HERE/apk-verify.sh" "${APK_ARGS[@]}" "$APK"
else
  echo "note: no goshisen-*.apk in $DIR — CLI-only release" >&2
fi

echo "RELEASE-VERIFY-PASS: all assets verified ($DIR)"
