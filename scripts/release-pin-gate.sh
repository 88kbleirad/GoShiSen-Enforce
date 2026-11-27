#!/usr/bin/env bash
# GoShiSen-Enforce — release pin/version agreement gate.
#
# Proves, for a release build, that the OpenWrt package's source pin and
# version labels identify the release consistently. The package Makefile
# lives in the same repo as the source, so the tag commit cannot contain
# its own hash; this gate replaces fake equality with checkable claims:
#
#   1. PKG_VERSION (openwrt/Makefile) equals the release version.
#   2. PKG_SOURCE_VERSION is an ancestor of (or equal to) the release
#      tag commit.
#   3. The source diff between the pin and the tag is EMPTY over the
#      runtime tree (cmd internal pkg go.mod go.sum openwrt/files);
#      otherwise the gate fails and prints the diff.
#   4. The apk payload binary reports --version == PKG_VERSION.
#   5. The apk filename version equals PKG_VERSION-r<PKG_RELEASE>.
#   6. (opt-in, GATE_TAG_SIGN_CHECK=1) the release tag is signed by an
#      allowed signer.
#   7/8. (opt-in, GATE_S005_CHECKS=1) payload Go >= MIN_GO_VERSION and
#      govulncheck binary-mode clean.
#   9. (opt-in, --manifest FILE / GATE_MANIFEST=FILE) the apk's build
#      manifest describes THIS commit and THIS apk: recipe + overlay
#      hashes equal the committed files, replaying them reproduces the
#      recorded recipe hash, the Go and SDK pins equal the repo's policy,
#      and the artifact hashes match the files. See
#      scripts/check-apk-build-manifest.sh.
#
# What this gate does NOT prove: that the pin is the tag commit itself
# (impossible by construction — the tag cannot contain its own hash).
# It proves source equivalence between what was pinned and what was
# tagged, and label agreement, so the published artifact is traceable
# to a single source tree.
#
# The claim this gate defers — "the tagged commit itself carries
# PKG_VERSION == the tag version" — is enforced BEFORE tagging by
# scripts/release-pretag-check.sh (F-01 gate).
#
# Usage:
#   scripts/release-pin-gate.sh \
#     --apk goshisen-0.2.4-r1.apk \
#     --tag v0.2.4 \
#     --payload /path/to/extracted/goshisen
#
# Exit codes:
#   0  PASS
#   1  FAIL (one of the checks failed; the failing check is named)
#   2  usage
#   3  ENV (git or apk-tools unavailable)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# GATE_REPO overrides the repository the gate inspects (tests use a
# scratch git repo). Defaults to the checkout containing this script.
GATE_REPO="${GATE_REPO:-$ROOT}"
PKG_MAKEFILE="$GATE_REPO/openwrt/Makefile"

APK=""
TAG=""
PAYLOAD=""
MANIFEST="${GATE_MANIFEST:-}"

usage() {
  echo "usage: $0 --apk FILE.apk --tag vX.Y.Z --payload PAYLOAD_BINARY [--manifest build-manifest.json]" >&2
  exit 2
}

while [ $# -gt 0 ]; do
  case "$1" in
    --apk) APK="$2"; shift 2 ;;
    --tag) TAG="$2"; shift 2 ;;
    --payload) PAYLOAD="$2"; shift 2 ;;
    --manifest) MANIFEST="$2"; shift 2 ;;
    *) usage ;;
  esac
done

[ -n "$APK" ] || { echo "ERROR: --apk required" >&2; usage; }
[ -n "$TAG" ] || { echo "ERROR: --tag required" >&2; usage; }
[ -n "$PAYLOAD" ] || { echo "ERROR: --payload required" >&2; usage; }

command -v git >/dev/null 2>&1 || { echo "ENV: git required" >&2; exit 3; }

# ---------------------------------------------------------------------------
# Extract package metadata from the Makefile.
# ---------------------------------------------------------------------------
pkg_var() { # pkg_var <name>
  sed -nE "s/^PKG_$1[[:space:]]*:=?[[:space:]]*(.*)/\1/p" "$PKG_MAKEFILE" | head -n1
}

PKG_VERSION="$(pkg_var VERSION)"
PKG_RELEASE="$(pkg_var RELEASE)"
PKG_SOURCE_VERSION="$(pkg_var SOURCE_VERSION)"
PKG_MIRROR_HASH="$(pkg_var MIRROR_HASH)"

fail() { echo "FAIL: $*" >&2; exit 1; }
ok() { echo "OK: $*"; }

[ -n "$PKG_VERSION" ] || fail "PKG_VERSION empty in $PKG_MAKEFILE"
[ -n "$PKG_RELEASE" ] || fail "PKG_RELEASE empty in $PKG_MAKEFILE"
[ -n "$PKG_SOURCE_VERSION" ] || fail "PKG_SOURCE_VERSION empty in $PKG_MAKEFILE"

# ---------------------------------------------------------------------------
# 1. PKG_VERSION equals the release version.
# ---------------------------------------------------------------------------
TAG_VERSION="${TAG#v}"
[ "$PKG_VERSION" = "$TAG_VERSION" ] || \
  fail "PKG_VERSION=$PKG_VERSION does not match tag version $TAG_VERSION (tag $TAG)"
ok "PKG_VERSION=$PKG_VERSION matches tag version"

# ---------------------------------------------------------------------------
# 2. PKG_SOURCE_VERSION is an ancestor of (or equal to) the tag commit.
# ---------------------------------------------------------------------------
TAG_COMMIT="$(git -C "$GATE_REPO" rev-parse --verify --quiet "$TAG^{commit}")" || \
  fail "tag $TAG not found in this repository"
PIN_COMMIT="$(git -C "$GATE_REPO" rev-parse --verify --quiet "$PKG_SOURCE_VERSION^{commit}")" || \
  fail "pin $PKG_SOURCE_VERSION not found in this repository"

if [ "$PIN_COMMIT" = "$TAG_COMMIT" ]; then
  :
elif git -C "$GATE_REPO" merge-base --is-ancestor "$PIN_COMMIT" "$TAG_COMMIT" 2>/dev/null; then
  :
else
  fail "pin $PKG_SOURCE_VERSION is not an ancestor of tag $TAG ($TAG_COMMIT)"
fi
ok "pin $PKG_SOURCE_VERSION is ancestor/equal of tag $TAG ($TAG_COMMIT)"

# ---------------------------------------------------------------------------
# 3. Source equivalence: git diff pin..tag over the runtime tree is empty.
# ---------------------------------------------------------------------------
if DIFF="$(git -C "$GATE_REPO" diff --stat "$PIN_COMMIT" "$TAG_COMMIT" -- \
    cmd internal pkg go.mod go.sum openwrt/files)"; then
  :
else
  fail "git diff between pin and tag failed"
fi
if [ -n "$DIFF" ]; then
  fail "source diff between pin and tag is NOT empty over cmd internal pkg go.mod go.sum openwrt/files:
$DIFF"
fi
ok "source tree identical between pin ($PIN_COMMIT) and tag ($TAG_COMMIT)"

# ---------------------------------------------------------------------------
# 4. Payload binary --version equals PKG_VERSION.
# ---------------------------------------------------------------------------
[ -f "$PAYLOAD" ] || fail "payload binary not found: $PAYLOAD"
PAYLOAD_VERSION="$("$PAYLOAD" --version 2>&1 | head -n1)"
case "$PAYLOAD_VERSION" in
  "goshisen $PKG_VERSION") ;;
  *) fail "payload --version '$PAYLOAD_VERSION' != 'goshisen $PKG_VERSION'" ;;
esac
ok "payload --version matches PKG_VERSION ($PKG_VERSION)"

# ---------------------------------------------------------------------------
# 5. Apk filename version equals PKG_VERSION-r<PKG_RELEASE>.
# ---------------------------------------------------------------------------
APK_BASENAME="$(basename "$APK")"
EXPECTED_APK="goshisen-$PKG_VERSION-r$PKG_RELEASE.apk"
[ "$APK_BASENAME" = "$EXPECTED_APK" ] || \
  fail "apk filename '$APK_BASENAME' != expected '$EXPECTED_APK'"
ok "apk filename matches PKG_VERSION-rPKG_RELEASE ($EXPECTED_APK)"

# ---------------------------------------------------------------------------
# 6. Tag signature (SSH allowed_signers, v0.2.6+). This check runs where
#    the trust anchor lives (the maintainer's release environment). It
#    is a HARD gate: an unsigned tag or a tag signed by a key not in the
#    committed allowed_signers file fails.
#
#    `git verify-tag` returns 0 even for unsigned tags ("error: no
#    signature found") and for a missing/empty allowed_signers file, so
#    we parse the output:
#      PASS: "Good \"git\" signature for <principal> ..."
#      FAIL: "No principal matched." / "no signature found" / missing file
# ---------------------------------------------------------------------------
if [ -n "${GATE_TAG_SIGN_CHECK:-}" ] && [ "$GATE_TAG_SIGN_CHECK" = "1" ]; then
  ALLOWED_SIGNERS="${ALLOWED_SIGNERS:-$ROOT/docs/trust/allowed_signers}"
  if [ ! -f "$ALLOWED_SIGNERS" ]; then
    fail "tag signature check: allowed_signers file missing: $ALLOWED_SIGNERS"
  fi
  verify_out="$(git -C "$GATE_REPO" -c gpg.format=ssh -c gpg.ssh.allowedSignersFile="$ALLOWED_SIGNERS" verify-tag "$TAG" 2>&1 || true)"
  case "$verify_out" in
    *'No principal matched.'*)
      fail "tag signature check: \"$TAG\" not signed by an allowed signer (see $ALLOWED_SIGNERS)"
      ;;
    *'no signature found'*)
      fail "tag signature check: \"$TAG\" is not signed (git verify-tag: no signature found)"
      ;;
    *'signature for '*)
      ok "tag signature verified: $TAG ($(echo "$verify_out" | head -n1))"
      ;;
    *)
      fail "tag signature check: unexpected verify-tag output: $(echo "$verify_out" | tr '\n' ' ')"
      ;;
  esac
else
  echo "note: tag signature check skipped (set GATE_TAG_SIGN_CHECK=1 to enable)" >&2
fi

# ---------------------------------------------------------------------------
# 7. S-005 gate — payload Go toolchain >= MIN_GO_VERSION (packaging policy).
# 8. S-005 gate — govulncheck binary-mode on the payload (empty allowlist).
#
# These are optional here (the signing path enforces them too, in
# apk-sign.yml production mode); the release run enables them with
# GATE_S005_CHECKS=1 so the pinned release apk is verified before it
# is staged. Fail closed on any missing tool.
# ---------------------------------------------------------------------------
if [ -n "${GATE_S005_CHECKS:-}" ] && [ "$GATE_S005_CHECKS" = "1" ]; then
  command -v go >/dev/null 2>&1 || fail "S-005 gates: 'go' toolchain required (>= $(grep -E '^MIN_GO_VERSION=' "$ROOT/packaging/go-toolchain.env" | cut -d= -f2-))"
  command -v govulncheck >/dev/null 2>&1 || fail "S-005 gates: 'govulncheck' required (install golang.org/x/vuln/cmd/govulncheck, pinned; see ci.yml)"
  [ -f "$APK" ] || fail "S-005 gates: apk not found: $APK"
  APK_TOOL="${APK_TOOL:-}" bash "$ROOT/scripts/check-apk-payload-go.sh" "$APK"
  APK_TOOL="${APK_TOOL:-}" bash "$ROOT/scripts/check-apk-payload-govuln.sh" "$APK"
  ok "S-005 gates passed: payload Go >= $(grep -E '^MIN_GO_VERSION=' "$ROOT/packaging/go-toolchain.env" | cut -d= -f2-) and govulncheck clean"
else
  echo "note: S-005 payload gates skipped (set GATE_S005_CHECKS=1 to enable)" >&2
fi

# ---------------------------------------------------------------------------
# 9. apk build manifest (opt-in: --manifest FILE / GATE_MANIFEST=FILE).
#
# The pin gate above ties the apk's LABELS to the tag; this step ties the
# apk's BYTES to the committed recipe, toolchain and SDK image, so a
# manifest cannot claim a build the repository cannot produce. Recommend
# running it with GATE_MANIFEST on every release (the release run sets
# both this and GATE_S005_CHECKS=1).
# ---------------------------------------------------------------------------
if [ -n "$MANIFEST" ]; then
  bash "$ROOT/scripts/check-apk-build-manifest.sh" \
    --manifest "$MANIFEST" --commit "$TAG_COMMIT" --repo "$GATE_REPO" \
    --apk "$APK" --payload "$PAYLOAD" \
    || fail "apk build manifest check failed: $MANIFEST"
  ok "apk build manifest describes this commit and this apk ($MANIFEST)"
else
  echo "note: apk build-manifest check skipped (pass --manifest FILE or set GATE_MANIFEST)" >&2
fi

echo "PASS: release pin/version agreement gate passed (tag $TAG)"
