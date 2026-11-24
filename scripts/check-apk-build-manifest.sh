#!/usr/bin/env bash
# scripts/check-apk-build-manifest.sh — fail-closed verification that an
# apk was built from the committed recipe by the committed recipe.
#
# Why: the published apk is the artifact operators install, and "which
# source, which recipe, which toolchain" was previously only prose in a
# release note. scripts/build-apk-release.sh now emits a build manifest
# next to the apk; this checker re-derives every claim in it from the
# repository, so a manifest cannot describe a build that the committed
# files cannot produce.
#
# What it proves (all fail-closed):
#   1. the manifest is well-formed and has the required fields;
#   2. recipe.committed_sha256 covers EXACTLY the files under openwrt/ in
#      the recorded commit, and the recorded hashes equal the committed
#      bytes (no missing file, no extra file, no edited file);
#   3. recipe.overlay.sha256 equals the committed overlay patch at that
#      commit;
#   4. replaying the committed Makefile + the committed overlay + the
#      manifest's declared substitutions reproduces
#      recipe.makefile_final_sha256 — i.e. the built recipe is exactly
#      what the repo describes;
#   5. pkg.source_version resolves in the repository and is an
#      ancestor-or-equal of the recorded commit (the pin claim, same
#      shape as release-pin-gate.sh step 2);
#   6. build.go.version >= MIN_GO_VERSION and
#      build.go.tarball_sha256 / bin_go_sha256 equal the pinned values in
#      packaging/go-toolchain.env;
#   7. build.sdk_image equals the SDK digest pinned in
#      .github/workflows/apk-sign.yml (script and workflow cannot drift);
#   8. artifacts.apk.sha256 / artifacts.payload.sha256 match the actual
#      files when they are available, the apk name matches
#      PKG_VERSION-rPKG_RELEASE, the apk is unsigned (a plain tar.gz), and
#      the payload reports `goshisen <pkg.version>`;
#   9. with --compare, a second manifest (the A/B partner) has identical
#      artifact hashes and identical inputs.
#
# What it does NOT prove:
#   - that mirrors/downloads were uncompromised beyond sha256 verification
#     (the SDK verifies the source tarball against PKG_MIRROR_HASH);
#   - that the apk is trustworthy for installation — that is the signature
#     anchor's job (scripts/apk-verify.sh / apk-sign.yml);
#   - that the build ran on the tagged commit: the caller must point
#     --commit/--tag at the commit that was actually built.
#
# Usage:
#   scripts/check-apk-build-manifest.sh --manifest build-manifest-A.json \
#     [--apk goshisen-0.2.9-r1.apk] [--payload daemon.bin] \
#     [--commit SHA | --tag vX.Y.Z] [--repo DIR] [--compare build-manifest-B.json]
#
# Exit codes: 0 PASS, 1 FAIL (reason named on stderr), 2 usage, 3 ENV
# (missing jq/git/python3).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
REPO="$ROOT"
MANIFEST=""
APK=""
PAYLOAD=""
REF=""
COMPARE=""

usage() {
  echo "usage: $0 --manifest FILE [--apk FILE] [--payload FILE] [--commit SHA|--tag vX.Y.Z] [--repo DIR] [--compare FILE]" >&2
  exit 2
}
fail() { echo "FAIL: $*" >&2; exit 1; }
ok() { echo "OK: $*"; }

while [ $# -gt 0 ]; do
  case "$1" in
    --manifest) MANIFEST="${2:?}"; shift 2 ;;
    --apk) APK="${2:?}"; shift 2 ;;
    --payload) PAYLOAD="${2:?}"; shift 2 ;;
    --commit|--tag) REF="${2:?}"; shift 2 ;;
    --repo) REPO="${2:?}"; shift 2 ;;
    --compare) COMPARE="${2:?}"; shift 2 ;;
    -h|--help) usage ;;
    *) echo "unknown argument: $1" >&2; usage ;;
  esac
done
[ -n "$MANIFEST" ] || usage

for c in jq git python3 sha256sum; do
  command -v "$c" >/dev/null 2>&1 || { echo "ENV: $c required" >&2; exit 3; }
done
git -C "$REPO" rev-parse --is-inside-work-tree >/dev/null 2>&1 || { echo "ENV: $REPO is not a git work tree" >&2; exit 3; }

POLICY="$ROOT/packaging/go-toolchain.env"
APK_SIGN_WORKFLOW="$ROOT/.github/workflows/apk-sign.yml"
policy_get() { sed -nE "s/^$1=[[:space:]]*//p" "$POLICY" | head -n1; }

# --- 1. shape ------------------------------------------------------------
[ -f "$MANIFEST" ] || fail "manifest '$MANIFEST' not found"
jq -e . "$MANIFEST" >/dev/null 2>&1 || fail "manifest '$MANIFEST' is not valid JSON"
[ "$(jq -r '.manifest_version // empty' "$MANIFEST")" = "1" ] \
  || fail "manifest_version must be 1 (got '$(jq -r '.manifest_version // "missing"' "$MANIFEST")')"
for q in '.source.commit' '.pkg.name' '.pkg.version' '.pkg.release' '.pkg.source_version' \
         '.pkg.mirror_hash' '.recipe.committed_sha256' '.recipe.overlay.path' \
         '.recipe.overlay.sha256' '.recipe.makefile_final_sha256' '.build.sdk_image' \
         '.build.go.version' '.build.go.tarball_sha256' '.build.go.bin_go_sha256' \
         '.artifacts.apk.sha256' '.artifacts.payload.sha256'; do
  v="$(jq -r "($q) // empty" "$MANIFEST")"
  [ -n "$v" ] || fail "manifest lacks required field $q"
done
jq -e '.recipe.committed_sha256 | type == "object" and length > 0' "$MANIFEST" >/dev/null \
  || fail "recipe.committed_sha256 must be a non-empty object"
jq -e '.recipe.substitutions | type == "array"' "$MANIFEST" >/dev/null \
  || fail "recipe.substitutions must be an array"

PKG_VERSION="$(jq -r '.pkg.version' "$MANIFEST")"
PKG_RELEASE="$(jq -r '.pkg.release' "$MANIFEST")"
PKG_NAME="$(jq -r '.pkg.name' "$MANIFEST")"

# --- 2. the commit the manifest claims ----------------------------------
M_COMMIT="$(jq -r '.source.commit' "$MANIFEST")"
if [ -n "$REF" ]; then
  REF_SHA="$(git -C "$REPO" rev-parse --verify --quiet "$REF^{commit}")" \
    || fail "ref '$REF' not found in $REPO"
  [ "$REF_SHA" = "$M_COMMIT" ] \
    || fail "manifest source.commit $M_COMMIT != $REF ($REF_SHA)"
fi
git -C "$REPO" rev-parse --verify --quiet "$M_COMMIT^{commit}" >/dev/null \
  || fail "manifest source.commit $M_COMMIT is not a commit in $REPO"
ok "manifest commit $M_COMMIT resolves in the repository"

# --- 3. recipe hashes == committed bytes ---------------------------------
COMMITTED="$(git -C "$REPO" ls-tree -r --name-only "$M_COMMIT" -- openwrt | sort)"
[ -n "$COMMITTED" ] || fail "no files under openwrt/ at $M_COMMIT"
RECORDED="$(jq -r '.recipe.committed_sha256 | keys[]' "$MANIFEST" | sort)"
missing="$(comm -23 <(printf '%s\n' "$COMMITTED") <(printf '%s\n' "$RECORDED"))"
extra="$(comm -13 <(printf '%s\n' "$COMMITTED") <(printf '%s\n' "$RECORDED"))"
[ -z "$missing" ] || fail "manifest does not hash these committed recipe files: $(tr '\n' ' ' <<<"$missing")"
[ -z "$extra" ] || fail "manifest hashes files that are not in the recipe at $M_COMMIT: $(tr '\n' ' ' <<<"$extra")"
while IFS= read -r rel; do
  case "$rel" in
    openwrt/*) ;;
    *) fail "recorded recipe path '$rel' is outside openwrt/" ;;
  esac
  case "/$rel" in
    */../*|*/./*) fail "recorded recipe path '$rel' is not normalized" ;;
  esac
  want="$(jq -r --arg p "$rel" '.recipe.committed_sha256[$p]' "$MANIFEST")"
  got="$(git -C "$REPO" show "$M_COMMIT:$rel" | sha256sum | cut -d' ' -f1)"
  [ "$got" = "$want" ] || fail "recipe file $rel: committed sha256 $got != manifest $want"
done < <(printf '%s\n' "$RECORDED")
ok "all $(wc -l <<<"$RECORDED") recipe file hashes match $M_COMMIT"

# --- 4. overlay == committed patch --------------------------------------
OVL_REL="$(jq -r '.recipe.overlay.path' "$MANIFEST")"
OVL_WANT="$(jq -r '.recipe.overlay.sha256' "$MANIFEST")"
git -C "$REPO" cat-file -e "$M_COMMIT:$OVL_REL" 2>/dev/null \
  || fail "overlay '$OVL_REL' does not exist at $M_COMMIT"
OVL_GOT="$(git -C "$REPO" show "$M_COMMIT:$OVL_REL" | sha256sum | cut -d' ' -f1)"
[ "$OVL_GOT" = "$OVL_WANT" ] || fail "overlay $OVL_REL: committed sha256 $OVL_GOT != manifest $OVL_WANT"
ok "overlay hash matches the committed patch ($OVL_REL)"

# --- 5. replay the recipe (committed Makefile + overlay + substitutions) -
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
git -C "$REPO" archive "$M_COMMIT" openwrt | tar -x -C "$TMP"
git -C "$REPO" show "$M_COMMIT:$OVL_REL" > "$TMP/overlay.patch"
(cd "$TMP" && git apply "$TMP/overlay.patch") \
  || fail "the committed overlay does not apply to the committed openwrt/Makefile at $M_COMMIT"
jq -c '.recipe.substitutions[]?' "$MANIFEST" | while IFS= read -r sub; do
  key="$(jq -r '.key' <<<"$sub")"
  val="$(jq -r '.value' <<<"$sub")"
  python3 - "$TMP/openwrt/Makefile" "$key" "$val" <<'PY' \
    || fail "cannot apply recorded substitution $key"
import re, sys
path, key, val = sys.argv[1], sys.argv[2], sys.argv[3]
text = open(path, encoding='utf-8').read()
new, n = re.subn(rf'^{re.escape(key)}:=.*$', f'{key}:={val}', text, count=1, flags=re.M)
if n != 1:
    sys.exit(1)
open(path, 'w', encoding='utf-8').write(new)
PY
done
REPLAYED="$(sha256sum "$TMP/openwrt/Makefile" | cut -d' ' -f1)"
WANT_MK="$(jq -r '.recipe.makefile_final_sha256' "$MANIFEST")"
[ "$REPLAYED" = "$WANT_MK" ] \
  || fail "replayed recipe sha256 $REPLAYED != manifest makefile_final_sha256 $WANT_MK (the committed recipe cannot produce the built Makefile)"
ok "recipe replays to the manifest's makefile_final_sha256"

# --- 6. pin claim -------------------------------------------------------
SRC_VER="$(jq -r '.pkg.source_version' "$MANIFEST")"
SRC_SHA="$(git -C "$REPO" rev-parse --verify --quiet "$SRC_VER^{commit}")" \
  || fail "pkg.source_version $SRC_VER is not a commit in $REPO"
if [ "$SRC_SHA" = "$M_COMMIT" ]; then
  ok "pin $SRC_VER == the build commit"
elif git -C "$REPO" merge-base --is-ancestor "$SRC_SHA" "$M_COMMIT" 2>/dev/null; then
  ok "pin $SRC_VER is an ancestor of $M_COMMIT ($SRC_SHA)"
else
  fail "pkg.source_version $SRC_SHA is not an ancestor of the build commit $M_COMMIT"
fi
MIRROR="$(jq -r '.pkg.mirror_hash' "$MANIFEST")"
[[ "$MIRROR" =~ ^[0-9a-f]{64}$ ]] || fail "pkg.mirror_hash '$MIRROR' is not a sha256"

# --- 7. toolchain policy ------------------------------------------------
GO_VER="$(jq -r '.build.go.version' "$MANIFEST")"
MIN_GO="$(policy_get MIN_GO_VERSION)"
[ -n "$MIN_GO" ] || fail "$POLICY lacks MIN_GO_VERSION"
if [ "$(printf '%s\n' "$MIN_GO" "$GO_VER" | sort -V | head -1)" != "$MIN_GO" ]; then
  fail "build.go.version $GO_VER is below MIN_GO_VERSION $MIN_GO"
fi
ok "Go version $GO_VER >= MIN_GO_VERSION $MIN_GO"
for pair in "tarball_sha256 GO_TOOLCHAIN_TARBALL_SHA256" "bin_go_sha256 GO_TOOLCHAIN_BIN_GO_SHA256"; do
  set -- $pair
  m="$(jq -r ".build.go.$1" "$MANIFEST")"
  p="$(policy_get "$2")"
  [ -n "$p" ] || fail "$POLICY lacks $2"
  [ "$m" = "$p" ] || fail "build.go.$1 $m != pinned $2 $p"
done
ok "Go tarball + bin/go hashes match the pinned toolchain policy"

# --- 8. SDK digest == the workflow's pin --------------------------------
SDK_M="$(jq -r '.build.sdk_image' "$MANIFEST")"
SDK_P="$(sed -nE 's/^[[:space:]]*image:[[:space:]]*(openwrt\/sdk@sha256:[0-9a-f]{64}).*/\1/p' "$APK_SIGN_WORKFLOW" | head -n1)"
[ -n "$SDK_P" ] || fail "cannot read the pinned SDK digest from $APK_SIGN_WORKFLOW"
[ "$SDK_M" = "$SDK_P" ] || fail "build.sdk_image $SDK_M != the workflow pin $SDK_P"
ok "SDK image digest matches the apk-sign.yml pin"

# --- 9. artifacts -------------------------------------------------------
APK_NAME="$(jq -r '.artifacts.apk.name // empty' "$MANIFEST")"
EXPECTED_NAME="$PKG_NAME-$PKG_VERSION-r$PKG_RELEASE.apk"
[ "$APK_NAME" = "$EXPECTED_NAME" ] \
  || fail "artifacts.apk.name '$APK_NAME' != '$EXPECTED_NAME'"
if [ -z "$APK" ]; then
  cand="$(dirname "$MANIFEST")/$(jq -r '.artifacts.apk.name' "$MANIFEST" | sed "s/\.apk\$/-$(jq -r '.label // "A"' "$MANIFEST").apk/")"
  [ -f "$cand" ] && APK="$cand" || true
fi
if [ -n "$APK" ]; then
  [ -f "$APK" ] || fail "apk '$APK' not found"
  A_SHA="$(sha256sum "$APK" | cut -d' ' -f1)"
  A_WANT="$(jq -r '.artifacts.apk.sha256' "$MANIFEST")"
  [ "$A_SHA" = "$A_WANT" ] || fail "apk sha256 $A_SHA != manifest $A_WANT"
  tar -tzf "$APK" >/dev/null 2>&1 \
    || fail "apk '$APK' is not a plain tar.gz — the manifest says unsigned, but it looks signed"
  ok "apk sha256 matches and the apk is unsigned ($A_SHA)"
else
  echo "note: apk not supplied or not found next to the manifest; sha256 not compared" >&2
fi
if [ -z "$PAYLOAD" ]; then
  cand="$(dirname "$MANIFEST")/daemon-$(jq -r '.label // "A"' "$MANIFEST").bin"
  [ -f "$cand" ] && PAYLOAD="$cand" || true
fi
if [ -n "$PAYLOAD" ]; then
  [ -f "$PAYLOAD" ] || fail "payload '$PAYLOAD' not found"
  P_SHA="$(sha256sum "$PAYLOAD" | cut -d' ' -f1)"
  P_WANT="$(jq -r '.artifacts.payload.sha256' "$MANIFEST")"
  [ "$P_SHA" = "$P_WANT" ] || fail "payload sha256 $P_SHA != manifest $P_WANT"
  if [ -x "$PAYLOAD" ] && out="$("$PAYLOAD" --version 2>/dev/null)"; then
    [ "$out" = "goshisen $PKG_VERSION" ] \
      || fail "payload --version '$out' != 'goshisen $PKG_VERSION'"
  fi
  ok "payload sha256 matches ($P_SHA)"
else
  echo "note: payload not supplied or not found next to the manifest; sha256 not compared" >&2
fi

# --- 10. optional A/B comparison ---------------------------------------
if [ -n "$COMPARE" ]; then
  [ -f "$COMPARE" ] || fail "compare manifest '$COMPARE' not found"
  jq -e . "$COMPARE" >/dev/null 2>&1 || fail "compare manifest '$COMPARE' is not valid JSON"
  for f in '.source.commit' '.pkg.version' '.pkg.release' '.pkg.source_version' \
           '.pkg.mirror_hash' '.recipe.makefile_final_sha256' '.build.sdk_image' \
           '.build.go.tarball_sha256' '.artifacts.apk.sha256' '.artifacts.payload.sha256'; do
    a="$(jq -r "($f) // empty" "$MANIFEST")"
    b="$(jq -r "($f) // empty" "$COMPARE")"
    [ "$a" = "$b" ] || fail "A/B mismatch on $f: '$(jq -r "($f) // empty" "$MANIFEST")' vs '$b'"
  done
  la="$(jq -r '.label // "?"' "$MANIFEST")"; lb="$(jq -r '.label // "?"' "$COMPARE")"
  [ "$la" != "$lb" ] || fail "both manifests carry label '$la' — not two independent builds"
  ok "A/B manifests agree on inputs and on apk + payload hashes (labels $la/$lb)"
fi

echo "APK-BUILD-MANIFEST-CHECK-PASS"