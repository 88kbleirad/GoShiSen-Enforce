#!/usr/bin/env bash
# GoShiSen-Enforce — sign OpenWrt .apk packages (v3 ADB embedded signature).
#
# Uses the OpenWrt SDK host `apk` (apk-tools 3) `adbsign` applet with a
# PEM/`--sign-key` secret. The secret is injected by path or env; never
# hardcoded, never logged, never exported.
#
# Fail-closed:
#   - missing key path or empty env secret -> exit 1 before signing
#   - missing/not-a-file apk -> exit 2
#   - signing failure (e.g. wrong key format) -> exit 3
#
# Usage:
#   apk-sign.sh --apk FILE.apk [--apk FILE2.apk ...] \
#       --key KEY.pem | --key-file FILE | --key-env VAR
#   apk-sign.sh --apk FILE.apk --sign-key /path/to/key.pem
#
# Environment:
#   APK_TOOL     SDK host apk binary (default: apk)
#   APK_SIGN_KEYS_DIR  dir of trust keys (for self-check after signing)
set -euo pipefail

APK="${APK_TOOL:-apk}"
SIGN_KEY=""
APKS=()

usage() {
  echo "usage: $0 --apk FILE.apk [--apk ...] (--key KEY.pem|--key-file FILE|--key-env VAR)" >&2
  exit 2
}

while [ $# -gt 0 ]; do
  case "$1" in
    --apk) APKS+=("$2"); shift 2 ;;
    --key|--sign-key) SIGN_KEY="$2"; shift 2 ;;
    --key-file)
      [ -f "$2" ] || { echo "ERROR: key file $2 missing" >&2; exit 1; }
      SIGN_KEY="$2"; shift 2 ;;
    --key-env)
      v="${!2:-}"
      [ -n "$v" ] || { echo "ERROR: env $2 is empty/missing" >&2; exit 1; }
      tmp="$(mktemp)"; trap 'rm -f "$tmp"' EXIT
      printf '%s' "$v" > "$tmp"; chmod 600 "$tmp"
      SIGN_KEY="$tmp"; shift 2 ;;
    *) usage ;;
  esac
done

[ "${#APKS[@]}" -gt 0 ] || usage
[ -n "$SIGN_KEY" ] || { echo "ERROR: signing key not provided" >&2; exit 1; }
[ -f "$SIGN_KEY" ] || { echo "ERROR: key $SIGN_KEY not found" >&2; exit 1; }

for a in "${APKS[@]}"; do
  [ -f "$a" ] || { echo "ERROR: apk $a not found" >&2; exit 2; }
  if ! "$APK" adbsign --allow-untrusted --sign-key "$SIGN_KEY" "$a" >/dev/null 2>&1; then
    echo "ERROR: signing $a failed" >&2
    exit 3
  fi
  echo "OK: signed $a"
done
