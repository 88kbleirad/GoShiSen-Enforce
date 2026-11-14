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

SCRATCH="${RUNNER_TEMP:-$(mktemp -d)}"
KEY="$SCRATCH/goshisen-release-key.pem"
trap 'rm -f "$KEY"' EXIT

printf '%s' "$RELEASE_SIGN_KEY" > "$KEY"
chmod 600 "$KEY"

# The secret must correspond to the committed trust anchor; a mismatched
# key fails the release before anything is signed.
bash "$(dirname "$0")/check-key-match.sh" "$KEY" "$PUB"

bash "$(dirname "$0")/sign-artifacts.sh" \
  --dist "$DIST" --key "$KEY" --public-key "$PUB" \
  --commit "$COMMIT" --tag "$TAG" \
  --builder "$BUILDER" --toolchain "$TOOLCHAIN" --sign

bash "$(dirname "$0")/verify-signatures.sh" \
  --dist "$DIST" --public-key "$PUB" \
  --commit "$COMMIT" --require-sign

rm -f "$KEY"
echo "OK: release artifacts signed and verified (key removed)"
