#!/usr/bin/env bash
# release-workflow-guard-test.sh — F-02 regression tests for
# .github/workflows/release.yml (issue #100).
#
# Two layers:
#   * structural: scripts/check-release-workflow-structure.py must accept
#     the real workflow and reject one negative fixture per rule (a rule
#     that stops firing is caught here).
#   * behavioural: the real workflow's fail-closed step bodies are
#     extracted (--print-step) and executed against good/bad inputs —
#     the dry-run ref guard, the asset checksum gate, the trust-anchor
#     fingerprint gate and the dry-run signing path (wrong/empty key).
#
# The CLI signing key value is never read: the dry-run fixtures pass a
# wrong key and an empty key and only assert the exit status.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
CHECK="$ROOT/scripts/check-release-workflow-structure.py"
REAL="$ROOT/.github/workflows/release.yml"
FIX="$ROOT/tests/ci/fixtures/release-workflow"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }
step() { # step <name-prefix> <dest>  — extract a run body from the sign job
  python3 "$CHECK" --release "$REAL" --print-step sign "$1" > "$2" \
    || fail "cannot extract step '$1' from $REAL"
  [ -s "$2" ] || fail "extracted step '$1' is empty"
}

# PyYAML is required (ships with yamllint on ubuntu-24.04 runners).
# Fail closed, never skip the check.
python3 -c 'import yaml' || fail "PyYAML is required for the structural checker"

# ---------------------------------------------------------------------------
# structural: positive
python3 "$CHECK" "$FIX/good.yml" >/dev/null || fail "good.yml was REJECTED"

# ---------------------------------------------------------------------------
# structural: negative fixtures, one per rule
expect_fail() {
  local file="$1" want="$2" out
  out="$(python3 "$CHECK" "$FIX/$file" 2>&1 || true)"
  case "$out" in
    *"$want"*) ;;
    *) fail "$file: expected reason '$want', got: $out" ;;
  esac
}

expect_fail bad-no-env.yml                 "must declare environment"
expect_fail bad-secret-in-build.yml        "secret-scope"
expect_fail bad-dryrun-secret-in-build.yml "secret-scope"
expect_fail bad-build-write.yml            "must be exactly contents: read"
expect_fail bad-interp.yml                 "interpolates \${{ github."
expect_fail bad-sign-read.yml              "sign-only-write"
expect_fail bad-secret-in-run-shell.yml    "secret-not-in-shell"
expect_fail bad-dryrun-no-sign.yml         "dry-run-signs-with-key"
expect_fail bad-dryrun-upload.yml          "dry-run-no-writes"
expect_fail bad-release-unprotected.yml    "release-step-guarded"
expect_fail bad-dryrun-noguard.yml         "dry-run-main-only"
expect_fail bad-build-env.yml              "build-readonly"

# structural: the CLI key must not appear in ANY other workflow without the
# environment, even when release.yml itself is clean (repo-wide rule).
cat > "$TMP/rogue.yml" <<'YAML'
permissions:
  contents: read
jobs:
  rogue:
    steps:
      - name: leak
        env:
          K: ${{ secrets.GH_RELEASE_SIGNING_KEY }}
        run: echo hi
YAML
out="$(python3 "$CHECK" --release "$REAL" --scan "$TMP/rogue.yml" 2>&1 || true)"
case "$out" in
  *"secret-scope"*"rogue.yml"*) ;;
  *) fail "repo-wide secret scan did not reject rogue.yml: $out" ;;
esac

# structural: the real workflows (release + every sibling) must pass.
scan_args=(--release "$REAL" --scan "$REAL")
for wf in "$ROOT"/.github/workflows/*.yml; do
  scan_args+=(--scan "$wf")
done
python3 "$CHECK" "${scan_args[@]}" >/dev/null || fail "real workflows failed the structural check"

# ---------------------------------------------------------------------------
# behavioural: dry-run ref guard — main only, fail-closed
step "Dry-run ref guard" "$TMP/refguard.sh"
GITHUB_REF=refs/heads/main bash "$TMP/refguard.sh" >/dev/null \
  || fail "dry-run ref guard rejected refs/heads/main"
for ref in refs/heads/dev refs/heads/main-x refs/tags/v1.2.3 refs/pull/1/merge; do
  if GITHUB_REF="$ref" bash "$TMP/refguard.sh" >/dev/null 2>&1; then
    fail "dry-run ref guard ACCEPTED $ref"
  fi
done

# ---------------------------------------------------------------------------
# behavioural: asset checksum gate (payload == build checksums, fail-closed)
step "Verify assets against build checksums" "$TMP/checksums.sh"
fresh_dist() {
  rm -rf "$TMP/work"; mkdir -p "$TMP/work/dist"
  printf 'amd64' > "$TMP/work/dist/goshisen-linux-amd64.tar.gz"
  printf 'arm64' > "$TMP/work/dist/goshisen-linux-arm64.tar.gz"
  printf '{"spdxVersion":"SPDX-2.3","packages":[{"name":"goshisen"}]}' > "$TMP/work/dist/sbom.spdx.json"
  (cd "$TMP/work/dist" && sha256sum goshisen-linux-*.tar.gz > checksums.txt)
}
run_gate() { (cd "$TMP/work" && bash "$TMP/checksums.sh"); }

fresh_dist
run_gate >/dev/null || fail "checksum gate rejected matching assets"

fresh_dist
printf 'x' >> "$TMP/work/dist/goshisen-linux-amd64.tar.gz"
run_gate >/dev/null 2>&1 && fail "checksum gate accepted a tampered archive"

fresh_dist
printf 'stray' > "$TMP/work/dist/stray.txt"
run_gate >/dev/null 2>&1 && fail "checksum gate accepted an unexpected extra asset"

fresh_dist
rm "$TMP/work/dist/sbom.spdx.json"
run_gate >/dev/null 2>&1 && fail "checksum gate accepted a missing asset"

# ---------------------------------------------------------------------------
# behavioural: production trust-anchor fingerprint gate
step "Verify production trust anchor" "$TMP/anchor.sh"
( cd "$ROOT" && bash "$TMP/anchor.sh" ) >/dev/null \
  || fail "anchor gate rejected the committed production anchor"

mkdir -p "$TMP/foreign/keys"
openssl genpkey -algorithm ed25519 -out "$TMP/foreign/keys/k.pem" 2>/dev/null
openssl pkey -in "$TMP/foreign/keys/k.pem" -pubout -out "$TMP/foreign/keys/goshisen-release.pub.pem" 2>/dev/null
if ( cd "$TMP/foreign" && bash "$TMP/anchor.sh" ) >/dev/null 2>&1; then
  fail "anchor gate ACCEPTED a foreign (non-production) anchor"
fi

# ---------------------------------------------------------------------------
# behavioural: the dry-run signing path fails closed on a wrong or empty key
# (never reads the real secret; only exit statuses are asserted).
step "Dry-run — sign a fixed dummy" "$TMP/dryrun.sh"
openssl genpkey -algorithm ed25519 -out "$TMP/wrong.pem" 2>/dev/null

if ( cd "$ROOT" && GITHUB_SHA=deadbeef RELEASE_SIGN_KEY="$(cat "$TMP/wrong.pem")" \
     bash "$TMP/dryrun.sh" ) >/dev/null 2>&1; then
  fail "dry-run signing path ACCEPTED a key that does not match the production anchor"
fi

if ( cd "$ROOT" && GITHUB_SHA=deadbeef RELEASE_SIGN_KEY="" \
     bash "$TMP/dryrun.sh" ) >/dev/null 2>&1; then
  fail "dry-run signing path ACCEPTED an empty key"
fi

# behavioural: the dry-run step's success path works end to end. Signing with
# the real secret is not possible here, so the same code path is exercised with
# a scratch keypair substituted through release-sign.sh's documented
# RELEASE_PUB_KEY knob; the printed fingerprint is still the committed
# production anchor's (hardcoded in the step).
openssl genpkey -algorithm ed25519 -out "$TMP/good.pem" 2>/dev/null
openssl pkey -in "$TMP/good.pem" -pubout -out "$TMP/good.pub.pem" 2>/dev/null
out="$( cd "$ROOT" && GITHUB_SHA=deadbeef \
        RELEASE_PUB_KEY="$TMP/good.pub.pem" \
        RELEASE_SIGN_KEY="$(cat "$TMP/good.pem")" \
        bash "$TMP/dryrun.sh" 2>&1 )" \
  || fail "dry-run signing path rejected a matching scratch keypair: $out"
case "$out" in
  *VERIFY-PASS*) ;;
  *) fail "dry-run step did not print the verification result: $out" ;;
esac
case "$out" in
  *24215e016621925215dec0ad6ea14ac74253a4e97249a973e89d649a2217cca0*) ;;
  *) fail "dry-run step did not print the production fingerprint: $out" ;;
esac
# Leak check: no private-key material may reach the printed output.
key_body="$(grep -v -- '-----' "$TMP/good.pem" | tr -d '\n')"
case "$out" in
  *"$key_body"*|*"PRIVATE KEY"*) fail "dry-run output leaked private key material" ;;
esac

# behavioural: a mis-pasted secret is diagnosed by SHAPE (never by value).
# These are the two shapes that a wrong environment copy takes in practice:
# base64-encoded PEM, and the public key instead of the private key.
base64_pem="$(base64 -w0 < "$TMP/good.pem")"
if out="$( cd "$ROOT" && GITHUB_SHA=deadbeef RELEASE_SIGN_KEY="$base64_pem" \
           bash "$TMP/dryrun.sh" 2>&1 )"; then
  fail "base64-encoded secret was ACCEPTED as the signing key"
fi
case "$out" in
  *"is not a PEM private key"*) ;;
  *) fail "base64 secret: no shape diagnosis in output: $out" ;;
esac
case "$out" in
  *"$base64_pem"*|*"PRIVATE KEY"*) fail "diagnostic leaked the secret value" ;;
esac

if out="$( cd "$ROOT" && GITHUB_SHA=deadbeef RELEASE_SIGN_KEY="$(cat "$TMP/good.pub.pem")" \
           bash "$TMP/dryrun.sh" 2>&1 )"; then
  fail "public-key PEM was ACCEPTED as the signing key"
fi
case "$out" in
  *"holds a PUBLIC key PEM"*) ;;
  *) fail "public-key secret: no shape diagnosis in output: $out" ;;
esac

# behavioural: a secret that is a valid PEM but belongs to a DIFFERENT key is
# rejected, and the diagnosis names which known anchor it belongs to (the usual
# mis-paste is the APK signing key) — names only, never key material.
openssl genpkey -algorithm ed25519 -out "$TMP/other.pem" 2>/dev/null
openssl pkey -in "$TMP/other.pem" -pubout -out "$TMP/other.pub.pem" 2>/dev/null
mkdir -p "$TMP/anchors"
cp "$TMP/good.pub.pem" "$TMP/anchors/goshisen-release.pub.pem"    # expected anchor
cp "$TMP/other.pub.pem" "$TMP/anchors/goshisen-apk.pub.pem"       # a different known anchor

if out="$( cd "$ROOT" && GITHUB_SHA=deadbeef \
           RELEASE_PUB_KEY="$TMP/anchors/goshisen-release.pub.pem" \
           RELEASE_SIGN_KEY="$(cat "$TMP/other.pem")" \
           bash "$TMP/dryrun.sh" 2>&1 )"; then
  fail "a private key not matching the expected anchor was ACCEPTED"
fi
case "$out" in
  *"holds a key that belongs to: goshisen-apk.pub.pem"*) ;;
  *) fail "wrong-key secret: diagnosis did not name the matching anchor: $out" ;;
esac
case "$out" in
  *"-----BEGIN"*) fail "diagnosis leaked key material" ;;
esac

# no other known anchor matches -> said so explicitly, still fails closed
rm "$TMP/anchors/goshisen-apk.pub.pem"
if out="$( cd "$ROOT" && GITHUB_SHA=deadbeef \
           RELEASE_PUB_KEY="$TMP/anchors/goshisen-release.pub.pem" \
           RELEASE_SIGN_KEY="$(cat "$TMP/other.pem")" \
           bash "$TMP/dryrun.sh" 2>&1 )"; then
  fail "an unrelated private key was ACCEPTED"
fi
case "$out" in
  *"matching none of the anchors"*) ;;
  *) fail "unrelated-key secret: no 'none of the anchors' diagnosis: $out" ;;
esac

echo "RELEASE-WORKFLOW-GUARD-TEST-PASS"
