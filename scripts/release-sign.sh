#!/usr/bin/env bash
# GoShiSen-Enforce — tag/release signing step (production key, fail closed).
#
# Extracted from the release workflow so the fail-closed behavior is testable:
#   * missing secret            -> exit 1 before any signing
#   * secret key != public key  -> exit 1 (check-key-match.sh)
#   * signature/provenance fail -> exit non-zero (verify-signatures.sh)
#   * private key always removed (trap) — never left on the runner
#
# Usage (env):
#   RELEASE_SIGN_KEY     contents of the GH_RELEASE_SIGNING_KEY secret (required)
#   RELEASE_PUB_KEY      path to the committed public trust anchor
#                        (default: keys/goshisen-release.pub.pem)
#   RUNNER_TEMP          scratch dir for key material (default: mktemp -d)
#
# Args (passed through to sign-artifacts.sh):
#   --dist DIR --commit SHA --tag TAG --builder NAME --toolchain GO
set -euo pipefail

DIST=""
COMMIT=""
TAG=""
BUILDER=""
TOOLCHAIN=""

while [ $# -gt 0 ]; do
  case "$1" in
    --dist) DIST=$2; shift 2 ;;
    --commit) COMMIT=$2; shift 2 ;;
    --tag) TAG=$2; shift 2 ;;
    --builder) BUILDER=$2; shift 2 ;;
    --toolchain) TOOLCHAIN=$2; shift 2 ;;
    *) echo "usage: release-sign.sh --dist DIR --commit SHA --tag TAG --builder NAME --toolchain GO" >&2; exit 2 ;;
  esac
done

[ -n "$DIST" ] && [ -d "$DIST" ] || { echo "ERROR: --dist dir required" >&2; exit 2; }
PUB="${RELEASE_PUB_KEY:-$(cd "$(dirname "$0")/.." && pwd)/keys/goshisen-release.pub.pem}"
[ -f "$PUB" ] || { echo "ERROR: public key $PUB missing" >&2; exit 2; }

# Fail closed when the secret is absent (e.g. secret not yet stored, or a
# workflow running outside the trusted repository context).
if [ -z "${RELEASE_SIGN_KEY:-}" ]; then
  echo "FAIL: RELEASE_SIGN_KEY (GH_RELEASE_SIGNING_KEY) is missing" >&2
  exit 1
fi

# A passphrase-encrypted key cannot be used by this unattended path: say so
# explicitly instead of letting openssl report "cannot derive public key".
if printf '%s' "$RELEASE_SIGN_KEY" | grep -q -- '-----BEGIN ENCRYPTED PRIVATE KEY-----'; then
  echo "FAIL: RELEASE_SIGN_KEY is a PASSPHRASE-ENCRYPTED key; this path cannot unlock it" >&2
  echo "      Export the CLI release key UNENCRYPTED for the GH_RELEASE_SIGNING_KEY secret" >&2
  echo "      (the runner has no passphrase), or treat this as revocation + rotation." >&2
  exit 1
fi

SCRATCH="${RUNNER_TEMP:-$(mktemp -d)}"
KEY="$SCRATCH/goshisen-release-key.pem"
trap 'rm -f "$KEY"' EXIT

printf '%s' "$RELEASE_SIGN_KEY" > "$KEY"
chmod 600 "$KEY"

# Value-safe file metadata for diagnostics (counts/booleans only — never
# contents): the caller (dry run / release) can use these to tell a
# mis-pasted secret apart from a malformed one. Printed only when the
# shape check already failed, i.e. exactly when the value is not a PEM
# private key (a single-line value would otherwise become the raw secret
# in the log).
meta_lc=$(wc -l < "$KEY")
meta_cr=$(grep -c $'\r' "$KEY" || true)
meta_openssl="ok"
if ! openssl pkey -in "$KEY" -noout >/dev/null 2>&1; then
  meta_openssl="parse-failed"
fi

# The stored value must be a PEM private key. Otherwise openssl reports
# only an opaque "cannot derive public key", which does not say what was
# wrong (observed in the first live dry run: the environment copy of the
# secret did not parse). Name the SHAPE here — never the value: this
# output goes to the run log, so only a length and a category are printed.
key_chars=$(printf '%s' "$RELEASE_SIGN_KEY" | wc -c | tr -d ' ')
if ! printf '%s' "$RELEASE_SIGN_KEY" | grep -q -- '-----BEGIN .*PRIVATE KEY-----'; then
  if printf '%s' "$RELEASE_SIGN_KEY" | grep -q -- '-----BEGIN PUBLIC KEY-----'; then
    echo "FAIL: RELEASE_SIGN_KEY holds a PUBLIC key PEM, not the private key (${key_chars} chars)" >&2
  elif printf '%s' "$RELEASE_SIGN_KEY" | grep -q -- '-----BEGIN'; then
    echo "FAIL: RELEASE_SIGN_KEY holds a PEM block that is not a private key (${key_chars} chars)" >&2
  else
    echo "FAIL: RELEASE_SIGN_KEY is not a PEM private key (${key_chars} chars; no '-----BEGIN ...' line)" >&2
  fi
  echo "      Expected the raw PEM private key text. Common mis-pastes: base64-encoded" >&2
  echo "      PEM, the public key, a bare base64 body, an encrypted key, or a trimmed" >&2
  echo "      value. The value is never printed; re-set the secret from the offline backup." >&2
  [ -f "$KEY" ] && {
    echo "      metadata: ${key_chars} chars, ${meta_lc} line(s), CR=${meta_cr}, openssl=${meta_openssl}" >&2
  }
  exit 1
fi

# A PEM private-key header is present but openssl cannot load the key (distinct
# failure class, dry runs 44989394642 and 44991076551). First try SAFE repairs
# of common paste damage — quoting, literal backslash-n escapes, CRLF, trailing
# spaces — each verified by openssl; then let the real key prove itself against
# the anchor. Values never leak: only counts/booleans are printed.
if [ "$meta_openssl" != "ok" ]; then
  # The value carries a private-key PEM fence but openssl cannot load it.
  # Repair the paste damage that produces this (dry run 44992807857 shape:
  # 121 chars / 2 newlines / openssl fails, i.e. a canonical 119-char,
  # 3-line PEM wrapped in quotes with a stray space): surrounding whitespace,
  # ANY leading/trailing quote (they need not match - pastes produce
  # '..."/"...'), literal \n / \r\n / \t escapes, CR, and finally a
  # canonical reconstruction from the base64 payload if the line structure
  # was collapsed. Every candidate is validated by openssl; the surviving
  # key must still match the committed anchor before anything is signed.
  repaired=0
  normalized_key="$RELEASE_SIGN_KEY"
  pass=0
  while [ "$pass" -lt 3 ] && [ "$repaired" = 0 ]; do
    pass=$((pass+1))
    normalized_key=$(printf '%s' "$normalized_key" | sed -E \
      -e 's/^[[:space:]]+//' -e 's/[[:space:]]+$//' \
      -e 's/^[\x27\x22]//' -e 's/[\x27\x22]$//' \
      -e 's/\\r\\n/\n/g' -e 's/\\n/\n/g' -e 's/\\t//g' -e 's/\r//g')
    printf '%s\n' "$normalized_key" > "$KEY"
    if openssl pkey -in "$KEY" -noout >/dev/null 2>&1; then
      repaired=1
    fi
  done
  if [ "$repaired" = 0 ]; then
    # Last resort: rebuild a canonical PEM from the fence label + base64 body.
    label=$(printf '%s' "$RELEASE_SIGN_KEY" \
      | sed -nE 's/.*-----BEGIN ([A-Z0-9 ]+)-----.*/\1/p' | head -n1)
    if [ -n "$label" ]; then
      payload=$(printf '%s' "$RELEASE_SIGN_KEY" \
        | sed -E -e 's/\\r\\n//g' -e 's/\\n//g' -e 's/\\t//g' \
                 -e "s/-----BEGIN ${label}-----//g" -e "s/-----END ${label}-----//g" \
                 -e 's/[^A-Za-z0-9+/=]//g')
      if [ -n "$payload" ] && [ $(( ${#payload} % 4 )) -eq 0 ]; then
        { printf -- '-----BEGIN %s-----\n' "$label"
          printf '%s' "$payload" | fold -w 64
          printf '\n-----END %s-----\n' "$label"; } > "$KEY"
        if openssl pkey -in "$KEY" -noout >/dev/null 2>&1; then
          repaired=2
        fi
      fi
    fi
  fi
  if [ "$repaired" = 0 ]; then
    first=$(sed -n '1p' "$KEY"); last=$(sed -n '2p' "$KEY")
    has_begin=no; has_end=no
    case "$first" in -----BEGIN*) has_begin=yes;; esac
    case "$last" in *-----END*) has_end=yes;; esac
    body=$(sed -e '1d' -e '$d' "$KEY" | tr -d '\r\n' | sed -E 's/[^A-Za-z0-9+/=]//g')
    echo "FAIL: RELEASE_SIGN_KEY has a PEM private-key header but openssl cannot read it" >&2
    echo "      metadata: ${key_chars} chars, ${meta_lc} line(s), CR=${meta_cr}, openssl=parse-failed" >&2
    echo "      structure: begin-fence=$has_begin end-fence=$has_end body=${#body} body%4=$(( ${#body} % 4 ))" >&2
    # openssl's own reason (value-free; any long base64-ish token is scrubbed
    # so a corrupted body can never surface in the log)
    plain=$(openssl pkey -in "$KEY" -noout 2>&1 || true)
    openssl_err=$(printf '%s' "$plain" | head -n1 | sed -E 's/[A-Za-z0-9+/]{40,}={0,2}/<scrubbed>/g')
    if [ -n "$openssl_err" ]; then echo "      openssl says: $openssl_err" >&2; fi
    echo "      Neither whitespace/quote/escape repair nor a payload reconstruction produced a" >&2
    echo "      readable key: the base64 body is most likely truncated or corrupted. The value is" >&2
    echo "      never printed; re-set the secret from the offline backup's exact file contents" >&2
    echo "      (an Ed25519 PKCS#8 PEM is 119 chars over 3 lines)." >&2
    exit 1
  fi
  [ "$repaired" = 1 ] && echo "WARN: repaired paste damage (quotes/whitespace/escapes/CRLF) — key parses now" >&2
  [ "$repaired" = 2 ] && echo "WARN: reconstructed the PEM line structure (damaged newlines) — key parses now" >&2
fi

# The secret must correspond to the committed trust anchor; a mismatched
# key fails the release before anything is signed. On mismatch, identify
# which known anchor the stored key belongs to — by NAME only: the check
# compares public keys, never prints key material, and only ever names
# anchors that are already committed next to the expected anchor (the
# usual cause is pasting the APK signing key into the CLI key's secret).
if ! bash "$(dirname "$0")/check-key-match.sh" "$KEY" "$PUB"; then
  derived="$SCRATCH/derived.pub.pem"
  if openssl pkey -in "$KEY" -pubout -out "$derived" 2>/dev/null; then
    match=""
    for anchor in "$(dirname "$PUB")"/*.pub.pem; do
      if [ ! -f "$anchor" ]; then continue; fi
      if [ "$anchor" = "$PUB" ]; then continue; fi
      if cmp -s "$derived" "$anchor"; then
        match="${match:+$match, }$(basename "$anchor")"
      fi
    done
    if [ -n "$match" ]; then
      echo "FAIL: GH_RELEASE_SIGNING_KEY holds a key that belongs to: $match" >&2
      echo "      That is not the expected CLI release anchor ($(basename "$PUB")), which it must match." >&2
    else
      echo "FAIL: GH_RELEASE_SIGNING_KEY holds a private key matching none of the anchors beside $PUB" >&2
    fi
    echo "      Compared by public-key equality only; no key material is printed." >&2
  fi
  exit 1
fi

bash "$(dirname "$0")/sign-artifacts.sh" \
  --dist "$DIST" --key "$KEY" --public-key "$PUB" \
  --commit "$COMMIT" --tag "$TAG" \
  --builder "$BUILDER" --toolchain "$TOOLCHAIN" --sign

bash "$(dirname "$0")/verify-signatures.sh" \
  --dist "$DIST" --public-key "$PUB" \
  --commit "$COMMIT" --require-sign

rm -f "$KEY"
echo "OK: release artifacts signed and verified (key removed)"
