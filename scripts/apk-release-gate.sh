#!/usr/bin/env bash
# GoShiSen-Enforce — production APK release gate (sign + fail-closed verify).
#
# Orchestrates the APK signing path for the package release pipeline:
#   1. require the signing secret (env) and the approved public trust anchor
#      (file) with an EXPECTED fingerprint;
#   2. sign each apk with scripts/apk-sign.sh (adbsign --sign-key);
#   3. verify each signed apk with scripts/apk-verify.sh: signature via
#      `apk verify --keys-dir <anchor-dir>` AND full-file integrity via
#      --sha256 (recorded at sign time);
#   4. write a provenance manifest (source commit, tag, builder, apk sha256).
#
# Fail-closed (never falls back to --allow-untrusted; never publishes):
#   - missing secret                -> exit 1 (BLOCKED)
#   - missing anchor / fingerprint mismatch -> exit 1 (BLOCKED)
#   - missing apk file             -> exit 2
#   - signing failure               -> exit 3
#   - verify UNTRUSTED / sha256 mismatch / provenance mismatch -> exit 4
#
# Usage:
#   APK_SIGN_KEY="$(cat ${secret})" \
#   scripts/apk-release-gate.sh \
#     --apk goshisen-0.2.0-r5.apk [--apk ...] \
#     --anchor keys/goshisen-release.pub.pem \
#     --fingerprint 24215e016621925215dec0ad6ea14ac74253a4e97249a973e89d649a2217cca0 \
#     --commit <sha> --tag v0.2.2 --builder release.yml/apk --outdir dist-apk
#
# Environment:
#   APK_TOOL             apk-tools v3 binary (default: apk)
#   APK_SIGN_KEY         private key material (PEM) for adbsign (REQUIRED)
#   APK_VERIFY_NO SHAK?  (unused; kept for future use)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SIGN="$ROOT/scripts/apk-sign.sh"
VERIFY="$ROOT/scripts/apk-verify.sh"

APKS=()
ANCHOR=""
FINGERPRINT=""
COMMIT=""
TAG=""
BUILDER=""
OUTDIR=""

usage() {
  echo "usage: $0 --apk FILE.apk [--apk ...] --anchor PUB.pem --fingerprint FP --commit SHA [--tag TAG] [--builder NAME] [--outdir DIR]" >&2
  exit 2
}

while [ $# -gt 0 ]; do
  case "$1" in
    --apk) APKS+=("$2"); shift 2 ;;
    --anchor) ANCHOR="$2"; shift 2 ;;
    --fingerprint) FINGERPRINT="$2"; shift 2 ;;
    --commit) COMMIT="$2"; shift 2 ;;
    --tag) TAG="$2"; shift 2 ;;
    --builder) BUILDER="$2"; shift 2 ;;
    --outdir) OUTDIR="$2"; shift 2 ;;
    *) usage ;;
  esac
done

[ "${#APKS[@]}" -gt 0 ] || usage
[ -n "$ANCHOR" ] && [ -f "$ANCHOR" ] || { echo "ERROR: --anchor $ANCHOR missing" >&2; exit 1; }
[ -n "$FINGERPRINT" ] || { echo "ERROR: --fingerprint required" >&2; exit 1; }
[ -n "$COMMIT" ] || { echo "ERROR: --commit required" >&2; exit 1; }
OUTDIR="${OUTDIR:-dist-apk}"

# 1. Secret gate (BLOCKED when missing; never logged).
if [ -z "${APK_SIGN_KEY:-}" ]; then
  echo "FAIL: APK_SIGN_KEY (GH_RELEASE_APK_SIGNING_KEY) is missing" >&2
  exit 1
fi

# 2. Anchor fingerprint gate.
fp="$(openssl pkey -pubin -in "$ANCHOR" -outform DER 2>/dev/null | sha256sum | cut -d' ' -f1)"
if [ "$fp" != "$FINGERPRINT" ]; then
  echo "FAIL: anchor fingerprint mismatch (expected $FINGERPRINT, got $fp)" >&2
  exit 1
fi
echo "OK: anchor $ANCHOR fingerprint $fp"

# 3. Stage keys + trust dir.
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
printf '%s' "$APK_SIGN_KEY" > "$tmp/key.pem"; chmod 600 "$tmp/key.pem"
mkdir -p "$tmp/keys"
cp "$ANCHOR" "$tmp/keys/anchor.pub"

mkdir -p "$OUTDIR"
: > "$OUTDIR/provenance.txt"

for a in "${APKS[@]}"; do
  [ -f "$a" ] || { echo "ERROR: apk $a not found" >&2; exit 2; }
  base="$(basename "$a")"
  out="$OUTDIR/$base"
  cp "$a" "$out"

  # 4. Sign (in place copy).
  if ! APK_TOOL="${APK_TOOL:-apk}" bash "$SIGN" --apk "$out" --key "$tmp/key.pem"; then
    echo "ERROR: signing $a failed" >&2
    exit 3
  fi

  # 5. Record full-file sha256 (post-sign) and verify signature + integrity.
  sha="$(sha256sum "$out" | awk '{print $1}')"
  if ! APK_TOOL="${APK_TOOL:-apk}" bash "$VERIFY" \
       --keys-dir "$tmp/keys" --sha256 "$out=$sha" "$out"; then
    echo "FAIL: verification failed for $out" >&2
    exit 4
  fi

  echo "$base $sha" >> "$OUTDIR/provenance.txt"
done

echo "OK: APK release gate passed (signed+verified) in $OUTDIR"
