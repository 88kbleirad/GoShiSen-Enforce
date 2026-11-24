#!/usr/bin/env bash
# scripts/build-apk-release.sh — reproducible OpenWrt apk build for a
# GoShiSen-Enforce release, from committed inputs only.
#
# Why this exists: the v0.2.6–v0.2.8 apks were built with an ad-hoc local
# recipe copy (the SDK's `golang/host` build dependency disabled and the
# official go1.27.4 mounted at /go-host) that was never committed, so
# "built from the repo" could not be re-run by anyone. This script makes
# that recipe explicit, reviewable and hash-recorded; the only change it
# makes to the recipe is a committed patch (packaging/apk-build/
# no-golang-host.patch) applied to a SCRATCH copy — the working tree's
# openwrt/Makefile is never modified.
#
# What it produces in --out:
#   goshisen-<ver>-r<rel>-<label>.apk   the unsigned package
#   daemon-<label>.bin                  extracted usr/sbin/goshisen
#   version-<label>.txt                 payload --version
#   build-manifest-<label>.json         provenance (see docs/release/
#                                       release-pin-gate.md and
#                                       scripts/check-apk-build-manifest.sh)
#   logs/<label>-*.log                  feeds/defconfig/download/compile logs
#
# Inputs are explicit and pinned; every one is verified before use:
#   --commit <sha|ref>   commit whose openwrt/ + packaging/apk-build/ files
#                        are used (default HEAD of the repo)
#   --sdk-image <ref>    default: the digest pinned in
#                        .github/workflows/apk-sign.yml (read at run time,
#                        so script and workflow cannot drift apart)
#   --go-root <dir>      an already extracted official go1.27.4 tree; its
#                        bin/go sha256 must equal GO_TOOLCHAIN_BIN_GO_SHA256
#                        (packaging/go-toolchain.env). Without --go-root the
#                        pinned tarball is downloaded and sha256-verified.
#   --pkg-version <v>    override PKG_VERSION (release builds: normally left at
#                        the committed value). Digits and dots only — apk
#                        mkpkg rejects suffixes like `-test`, so a TEST build
#                        must use --pkg-release instead.
#   --pkg-release <n>    override PKG_RELEASE (e.g. 99 for a clearly-labelled
#                        local test build: version 0.2.9-r99 instead of r1)
#   --pkg-source-version <sha>  override PKG_SOURCE_VERSION (TEST builds of
#                        a tree whose pin is not the commit being tested).
#   --mirror-hash <sha256>      expected PKG_MIRROR_HASH; when omitted it is
#                        DERIVED with the zero-hash two-pass procedure
#                        (write 64 zeros, run `download`, read "got <hash>",
#                        write it, re-run `download` and verify the tarball).
#
# Network: the SDK needs to clone its pinned feeds and to fetch the pinned
# package source (git clone of PKG_SOURCE_VERSION); the pinned Go tarball
# is fetched only when --go-root is not given. Nothing else is downloaded.
#
# Fail-closed: non-zero on a dirty working tree, an unknown commit, a
# missing/extra recipe file, an overlay that does not apply, a Go
# tarball/bin mismatch, a mirror-hash mismatch, a missing artifact, or a
# disk watchdog trip. It never publishes, signs or tags anything.
#
# Usage (two independent builds, then compare):
#   scripts/build-apk-release.sh --commit HEAD --pkg-release 99 \
#     --go-root /data/toolchain/go1.27.4 --label A --out /data/v029-build/out \
#     --work /data/v029-build/wA
#   ... --label B --work /data/v029-build/wB
#   jq -r '.artifacts.apk.sha256, .artifacts.payload.sha256' \
#     /data/v029-build/out/build-manifest-{A,B}.json
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
POLICY="$ROOT/packaging/go-toolchain.env"
OVERLAY_REL="packaging/apk-build/no-golang-host.patch"
SDK_CONFIG_REL="packaging/apk-build/sdk-x86_64.config"
APK_SIGN_WORKFLOW="$ROOT/.github/workflows/apk-sign.yml"

COMMIT="HEAD"
SDK_IMAGE=""
GO_ROOT=""
PKG_VERSION_OVERRIDE=""
PKG_RELEASE_OVERRIDE=""
PKG_SOURCE_OVERRIDE=""
MIRROR_HASH=""
OUT=""
WORK=""
LABEL="A"
KEEP_WORK=0
CANONICAL_NAME=0
BUILD_TIMEOUT="${BUILD_TIMEOUT:-3600}"
PRINT_CONTAINER_SCRIPT=""

usage() {
  sed -n '/^# Usage (two independent builds/,/^#     --work/p' "$0" >&2
  echo "usage: $0 --out DIR [--commit REF] [--label A|B] [--work DIR]" >&2
  echo "         [--pkg-version V] [--pkg-source-version SHA] [--mirror-hash SHA256]" >&2
  echo "         [--go-root DIR | download the pinned tarball] [--sdk-image REF] [--keep-work]" >&2
  echo "         [--print-container-script derive|derive2|build]   # pre-flight: pipe it to bash -n" >&2
  exit 2
}

while [ $# -gt 0 ]; do
  case "$1" in
    --commit) COMMIT="${2:?}"; shift 2 ;;
    --sdk-image) SDK_IMAGE="${2:?}"; shift 2 ;;
    --go-root) GO_ROOT="${2:?}"; shift 2 ;;
    --pkg-version) PKG_VERSION_OVERRIDE="${2:?}"; shift 2 ;;
    --pkg-release) PKG_RELEASE_OVERRIDE="${2:?}"; shift 2 ;;
    --pkg-source-version) PKG_SOURCE_OVERRIDE="${2:?}"; shift 2 ;;
    --mirror-hash) MIRROR_HASH="${2:?}"; shift 2 ;;
    --out) OUT="${2:?}"; shift 2 ;;
    --work) WORK="${2:?}"; shift 2 ;;
    --label) LABEL="${2:?}"; shift 2 ;;
    --canonical-name) CANONICAL_NAME=1; shift ;;
    --print-container-script) PRINT_CONTAINER_SCRIPT="${2:?}"; shift 2 ;;
    --keep-work) KEEP_WORK=1; shift ;;
    -h|--help) usage ;;
    *) echo "unknown argument: $1" >&2; usage ;;
  esac
done

die() { echo "FAIL: $*" >&2; exit 1; }
ok() { echo "OK: $*"; }
note() { echo "note: $*" >&2; }

# --- container scripts (defined before any precondition) ------------------
# Single-quoted literals: nothing needs escaping, so a quoting mistake cannot
# silently break a 10-minute build (it did once). They read LABEL,
# PKG_VERSION, PKG_RELEASE and TARBALL_NAME from the environment docker_run
# passes. Syntax-check without a build, docker, a commit or a clean tree:
#   for s in derive derive2 build; do scripts/build-apk-release.sh --print-container-script $s | bash -n; done
PRELUDE='
set -euo pipefail
cd /builder
export PATH=/go-host/bin:/opt/venv/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
export GOTOOLCHAIN=local
export TMPDIR=/builder/tmp GOTMPDIR=/builder/tmp GOCACHE=/builder/tmp/gocache GOMODCACHE=/builder/tmp/gomod
go version
# Order matters and matches the v0.2.8 procedure: feeds update/install first,
# then our config fragment folded in by `make defconfig`.
./scripts/feeds update -a >/tmpwork/feeds-update.log 2>&1 || { tail -30 /tmpwork/feeds-update.log; exit 1; }
./scripts/feeds install -a >/tmpwork/feeds-install.log 2>&1 || { tail -30 /tmpwork/feeds-install.log; exit 1; }
cp /in/sdk.config .config
make defconfig >/tmpwork/defconfig.log 2>&1 || { tail -20 /tmpwork/defconfig.log; exit 1; }
'
# Disk watchdog: the container overlay lives on the host root filesystem, so
# abort before the host fills. It dies with the container; no cleanup needed.
WATCHDOG='
( while :; do a=$(df --output=avail -k /builder 2>/dev/null | tail -1); [ -n "$a" ] && [ "$a" -lt 409600 ] && { echo "DISK-WATCHDOG-ABORT avail=${a}K" >&2; pkill -x make || true; touch /tmp/diskwarn; }; sleep 10; done ) &
'
CB_DERIVE="$PRELUDE$WATCHDOG"'
make package/feeds/goshisen/download V=s >/tmpwork/download.log 2>&1 || true
grep -E "Hash mismatch for file $TARBALL_NAME" /tmpwork/download.log | tail -1 || true
find dl -name "$TARBALL_NAME" -printf "%s\n" || true
sha256sum dl/$TARBALL_NAME 2>/dev/null || true
'
CB_DERIVE2="$PRELUDE$WATCHDOG"'
make package/feeds/goshisen/download V=s >/tmpwork/download2.log 2>&1 || { tail -30 /tmpwork/download2.log; exit 1; }
sha256sum dl/$TARBALL_NAME
'
CB_BUILD="$PRELUDE$WATCHDOG"'
make package/feeds/goshisen/compile V=s >/tmpwork/compile.log 2>&1 || {
  echo COMPILE-FAILED
  tail -40 /tmpwork/compile.log
  [ -f /tmp/diskwarn ] && echo DISK-WATCHDOG-FIRED
  exit 1
}
APK=$(find /builder/bin -name "goshisen-*.apk" | head -1)
[ -n "$APK" ] || { echo NO-APK-PRODUCED; exit 1; }
echo "APK=$APK"
sha256sum "$APK"
mkdir -p /tmpwork/apk-x
/builder/staging_dir/host/bin/apk extract --allow-untrusted --destination /tmpwork/apk-x "$APK" >/dev/null
cp /tmpwork/apk-x/usr/sbin/goshisen /out/daemon-$LABEL.bin
sha256sum /out/daemon-$LABEL.bin
/out/daemon-$LABEL.bin --version | tee /out/version-$LABEL.txt
echo "--- go buildinfo ---"
go version -m /out/daemon-$LABEL.bin | head -2 || true
cp "$APK" /out/goshisen-$PKG_VERSION-r$PKG_RELEASE-$LABEL.apk
mkdir -p /tmpwork/emptykeys
if /builder/staging_dir/host/bin/apk verify --keys-dir /tmpwork/emptykeys "$APK" >/tmpwork/verify.out 2>&1; then
  echo "SIGNCHECK-FAIL: the produced apk verifies as TRUSTED - this script must only build unsigned apks"
  cat /tmpwork/verify.out
  exit 1
fi
grep -q "UNTRUSTED signature" /tmpwork/verify.out || {
  echo "SIGNCHECK-FAIL: unexpected apk verify output for an unsigned package"
  cat /tmpwork/verify.out
  exit 1
}
echo "SIGNCHECK-UNSIGNED-OK apk verify reports UNTRUSTED signature (unsigned build, as expected)"
echo BUILD-OK-$LABEL
'
if [ -n "$PRINT_CONTAINER_SCRIPT" ]; then
  case "$PRINT_CONTAINER_SCRIPT" in
    derive) printf '%s\n' "$CB_DERIVE" ;;
    derive2) printf '%s\n' "$CB_DERIVE2" ;;
    build) printf '%s\n' "$CB_BUILD" ;;
    *) die "--print-container-script takes derive|derive2|build" ;;
  esac
  exit 0
fi

[ -n "$OUT" ] || usage
[[ "$LABEL" =~ ^[A-Za-z0-9._-]+$ ]] || die "invalid --label '$LABEL'"

# --- pinned toolchain policy (single source of truth) ---------------------
[ -f "$POLICY" ] || die "missing $POLICY"
policy_get() { sed -nE "s/^$1=[[:space:]]*//p" "$POLICY" | head -n1; }
MIN_GO_VERSION="$(policy_get MIN_GO_VERSION)"
GO_VERSION="$(policy_get GO_TOOLCHAIN_VERSION)"
GO_TARBALL="$(policy_get GO_TOOLCHAIN_TARBALL)"
GO_TARBALL_URL="$(policy_get GO_TOOLCHAIN_TARBALL_URL)"
GO_TARBALL_SHA256="$(policy_get GO_TOOLCHAIN_TARBALL_SHA256)"
GO_BIN_SHA256="$(policy_get GO_TOOLCHAIN_BIN_GO_SHA256)"
for v in MIN_GO_VERSION GO_TOOLCHAIN_VERSION GO_TOOLCHAIN_TARBALL \
         GO_TOOLCHAIN_TARBALL_URL GO_TOOLCHAIN_TARBALL_SHA256 GO_TOOLCHAIN_BIN_GO_SHA256; do
  [ -n "$(policy_get "$v")" ] || die "packaging/go-toolchain.env lacks $v"
done
[[ "$GO_TARBALL_SHA256" =~ ^[0-9a-f]{64}$ ]] || die "bad GO_TOOLCHAIN_TARBALL_SHA256"
[[ "$GO_BIN_SHA256" =~ ^[0-9a-f]{64}$ ]] || die "bad GO_TOOLCHAIN_BIN_GO_SHA256"

# --- SDK image: default to the digest the signing workflow pins ----------
default_sdk_image() {
  sed -nE 's/^[[:space:]]*image:[[:space:]]*(openwrt\/sdk@sha256:[0-9a-f]{64}).*/\1/p' \
    "$APK_SIGN_WORKFLOW" | head -n1
}
if [ -z "$SDK_IMAGE" ]; then
  SDK_IMAGE="$(default_sdk_image)"
  [ -n "$SDK_IMAGE" ] || die "cannot read the pinned SDK digest from $APK_SIGN_WORKFLOW (pass --sdk-image)"
  note "SDK image taken from apk-sign.yml: $SDK_IMAGE"
else
  pinned="$(default_sdk_image)"
  [ "$SDK_IMAGE" = "$pinned" ] || die "--sdk-image $SDK_IMAGE differs from the workflow pin $pinned"
fi
[[ "$SDK_IMAGE" =~ ^openwrt/sdk@sha256:[0-9a-f]{64}$ ]] \
  || die "--sdk-image must be a digest reference (openwrt/sdk@sha256:<64 hex>), got '$SDK_IMAGE'"
SDK_TAG="$(grep -E "^[[:space:]]*image:[[:space:]]*$SDK_IMAGE" "$APK_SIGN_WORKFLOW" \
  | sed -nE 's/.*#[[:space:]]*([^[:space:]]+).*/\1/p' | head -n1)"

# --- repository preconditions -------------------------------------------
git -C "$ROOT" rev-parse --is-inside-work-tree >/dev/null 2>&1 || die "$ROOT is not a git work tree"
COMMIT_SHA="$(git -C "$ROOT" rev-parse --verify --quiet "$COMMIT^{commit}")" \
  || die "commit '$COMMIT' not found in $ROOT"
COMMIT_SUBJECT="$(git -C "$ROOT" log -1 --format=%s "$COMMIT_SHA")"
if [ -n "$(git -C "$ROOT" status --porcelain=v1)" ]; then
  die "working tree is dirty — the manifest must describe exactly the commit (commit or stash first)"
fi

OUT="$(realpath -m "$OUT")"
[ -n "$WORK" ] || WORK="$OUT/work-$LABEL"
WORK="$(realpath -m "$WORK")"
for d in "$OUT" "$WORK"; do
  case "$d/" in
    "$ROOT"/*) die "$d is inside the repository; use a scratch dir outside $ROOT" ;;
  esac
done
if [ -n "$MIRROR_HASH" ]; then
  [[ "$MIRROR_HASH" =~ ^[0-9a-f]{64}$ ]] || die "--mirror-hash must be 64 lowercase hex chars"
fi

command -v docker >/dev/null 2>&1 || die "docker not available"
command -v jq >/dev/null 2>&1 || die "jq not available"

mkdir -p "$OUT/logs" "$WORK/logs" "$WORK/recipe" "$WORK/inputs" "$WORK/bin" "$WORK/tmp" "$WORK/dl"
cleanup() { [ "$KEEP_WORK" = 1 ] || rm -rf "$WORK/go" "$WORK/recipe"; }
trap cleanup EXIT

# --- 1. materialize the recipe from the COMMITTED tree -------------------
# git archive guarantees the committed bytes (no working-tree edits leak in).
git -C "$ROOT" archive "$COMMIT_SHA" openwrt "$OVERLAY_REL" "$SDK_CONFIG_REL" \
  > "$WORK/recipe.tar" 2>"$WORK/logs/$LABEL-archive.log" \
  || { tail -3 "$WORK/logs/$LABEL-archive.log" >&2
       die "git archive failed for $COMMIT_SHA — does that commit contain $OVERLAY_REL and $SDK_CONFIG_REL?"; }
tar -xf "$WORK/recipe.tar" -C "$WORK/recipe"
MAKEFILE="$WORK/recipe/openwrt/Makefile"
[ -f "$MAKEFILE" ] || die "openwrt/Makefile missing from $COMMIT_SHA"
cp "$WORK/recipe/$SDK_CONFIG_REL" "$WORK/inputs/sdk.config"

# Committed hashes of every recipe file (the manifest's hash chain base).
declare -A RECIPE_SHA=()
while IFS= read -r rel; do
  RECIPE_SHA["$rel"]="$(sha256sum "$WORK/recipe/$rel" | cut -d' ' -f1)"
done < <(cd "$WORK/recipe" && find openwrt -type f | sort)
OVERLAY_SHA="$(sha256sum "$WORK/recipe/$OVERLAY_REL" | cut -d' ' -f1)"
SDK_CONFIG_SHA="$(sha256sum "$WORK/inputs/sdk.config" | cut -d' ' -f1)"

# --- 2. overlay (scratch only) ------------------------------------------
(cd "$WORK/recipe" && git apply --verbose "$OVERLAY_REL") 2>"$WORK/logs/$LABEL-overlay.log" \
  || die "overlay $OVERLAY_REL does not apply to openwrt/Makefile at $COMMIT_SHA"
grep -q '^# PKG_BUILD_DEPENDS:=golang/host$' "$MAKEFILE" \
  || die "overlay did not disable PKG_BUILD_DEPENDS (unexpected patch result)"

# --- 3. declared substitutions (recorded, so the checker can replay) -----
SUBS_JSON='[]'
subst() { # subst <KEY> <value> — rewrite `KEY:=...` exactly once
  local key="$1" val="$2" before
  before="$(grep -cE "^$key:=" "$MAKEFILE" || true)"
  [ "$before" = "1" ] || die "$key must appear exactly once in the patched Makefile (found $before)"
  python3 - "$MAKEFILE" "$key" "$val" <<'PY'
import re, sys
path, key, val = sys.argv[1], sys.argv[2], sys.argv[3]
text = open(path, encoding='utf-8').read()
new, n = re.subn(rf'^{re.escape(key)}:=.*$', f'{key}:={val}', text, count=1, flags=re.M)
assert n == 1, n
open(path, 'w', encoding='utf-8').write(new)
PY
  SUBS_JSON="$(jq -c --arg k "$key" --arg v "$val" \
    '. + [{key:$k, value:$v, rule:("s/^" + $k + ":=.*/" + $k + ":=" + $v + "/")}]' <<<"$SUBS_JSON")"
}
[ -n "$PKG_VERSION_OVERRIDE" ] && subst PKG_VERSION "$PKG_VERSION_OVERRIDE"
[ -n "$PKG_RELEASE_OVERRIDE" ] && subst PKG_RELEASE "$PKG_RELEASE_OVERRIDE"
[ -n "$PKG_SOURCE_OVERRIDE" ] && subst PKG_SOURCE_VERSION "$PKG_SOURCE_OVERRIDE"

pkg_var() { sed -nE "s/^PKG_$1[[:space:]]*:=?[[:space:]]*(.*)/\1/p" "$MAKEFILE" | head -n1; }
PKG_VERSION="$(pkg_var VERSION)"
PKG_RELEASE="$(pkg_var RELEASE)"
# apk mkpkg rejects an upstream version with a free-form suffix
# ("package version is invalid" for 0.2.9-test): an apk version is
# <digits[.digits]*-r<integer>]. Fail closed HERE, before the container
# work, instead of after the dependency closure has been built.
[[ "$PKG_VERSION" =~ ^[0-9]+(\.[0-9]+)*$ ]] \
  || die "PKG_VERSION '$PKG_VERSION' is not a valid apk version (digits and dots only; apk mkpkg rejects '-test'-style suffixes — use --pkg-release <n> to mark a test build)"
[[ "$PKG_RELEASE" =~ ^[0-9]+$ ]] || die "PKG_RELEASE '$PKG_RELEASE' must be an integer"
PKG_SOURCE_VERSION="$(pkg_var SOURCE_VERSION)"
PKG_SOURCE_RESOLVED="$(git -C "$ROOT" rev-parse --verify --quiet "$PKG_SOURCE_VERSION^{commit}")" \
  || die "PKG_SOURCE_VERSION $PKG_SOURCE_VERSION is not a commit in this repo"
MAKEFILE_FINAL_SHA="$(sha256sum "$MAKEFILE" | cut -d' ' -f1)"
APK_NAME="goshisen-$PKG_VERSION-r$PKG_RELEASE.apk"
TARBALL_NAME="goshisen-$PKG_VERSION.tar.zst"

# --- 4. Go toolchain (pinned, hash-verified) ----------------------------
if [ -n "$GO_ROOT" ]; then
  GO_ROOT="$(realpath "$GO_ROOT")"
  [ -x "$GO_ROOT/bin/go" ] || die "--go-root $GO_ROOT has no bin/go"
  GOT_SHA="$(sha256sum "$GO_ROOT/bin/go" | cut -d' ' -f1)"
  [ "$GOT_SHA" = "$GO_BIN_SHA256" ] \
    || die "bin/go sha256 $GOT_SHA != pinned $GO_BIN_SHA256 (not the official go$GO_VERSION tree)"
  GO_SOURCE="local:$GO_ROOT (bin/go sha256 verified)"
else
  [ -f "$WORK/go/$GO_TARBALL" ] || curl -fSL --max-time 1800 -o "$WORK/go/$GO_TARBALL" "$GO_TARBALL_URL" \
    || die "cannot download $GO_TARBALL_URL"
  GOT_TAR_SHA="$(sha256sum "$WORK/go/$GO_TARBALL" | cut -d' ' -f1)"
  [ "$GOT_TAR_SHA" = "$GO_TARBALL_SHA256" ] \
    || die "Go tarball sha256 $GOT_TAR_SHA != pinned $GO_TARBALL_SHA256"
  tar -xzf "$WORK/go/$GO_TARBALL" -C "$WORK/go"
  GO_ROOT="$WORK/go/go"
  GOT_SHA="$(sha256sum "$GO_ROOT/bin/go" | cut -d' ' -f1)"
  [ "$GOT_SHA" = "$GO_BIN_SHA256" ] || die "extracted bin/go sha256 $GOT_SHA != pinned $GO_BIN_SHA256"
  GO_SOURCE="tarball:$GO_TARBALL_URL (sha256 verified)"
fi
GO_ACTUAL="$("$GO_ROOT/bin/go" version 2>&1 | awk '{print $3}')"
[ "$GO_ACTUAL" = "go$GO_VERSION" ] || die "$GO_ROOT/bin/go reports $GO_ACTUAL, expected go$GO_VERSION"
ok "Go toolchain go$GO_VERSION verified ($GO_SOURCE)"

# --- 5. container-side helpers ------------------------------------------
# Mounts: the scratch recipe feed, the verified Go, /out for artifacts, and
# bin/tmp/dl on the host so the container overlay (on /) does not fill.
# NEVER mount over /builder/staging_dir — it hides the SDK's prebuilt host
# tools (the "cpio: command not found" trap).
docker_run() { # docker_run <logfile> <container-script> [args...]
  local log="$1"; shift
  local script="$1"; shift
  timeout "$BUILD_TIMEOUT" docker run --rm --network host --user root \
    -e LABEL="$LABEL" -e PKG_VERSION="$PKG_VERSION" -e PKG_RELEASE="$PKG_RELEASE" \
    -e TARBALL_NAME="$TARBALL_NAME" \
    -v "$WORK/recipe/openwrt":/builder/package/feeds/goshisen:ro \
    -v "$WORK/inputs":/in:ro \
    -v "$GO_ROOT":/go-host:ro \
    -v "$WORK/bin":/builder/bin \
    -v "$WORK/tmp":/builder/tmp \
    -v "$WORK/dl":/builder/dl \
    -v "$OUT":/out \
    -v "$WORK/tmp":/tmpwork \
    "$SDK_IMAGE" bash -c "$script" bash "$@" >>"$log" 2>&1
}

# (The container scripts and the --print-container-script pre-flight live at the
# top of this file, before any precondition, so they can be syntax-checked
# without docker, a commit or a clean tree.)

# The mirror hash is part of the Makefile we mount, so each pass writes the
# scratch Makefile first (the mounted feed is read-only to the container).
scratch_set_mirror() { # scratch_set_mirror <sha256>
  subst PKG_MIRROR_HASH "$1"
  MAKEFILE_FINAL_SHA="$(sha256sum "$MAKEFILE" | cut -d' ' -f1)"
}

# --- 6. derive PKG_MIRROR_HASH if the caller did not supply it ----------
MIRROR_DERIVED=false
if [ -n "$MIRROR_HASH" ]; then
  scratch_set_mirror "$MIRROR_HASH"
  ok "using caller-supplied PKG_MIRROR_HASH $MIRROR_HASH"
else
  scratch_set_mirror "0000000000000000000000000000000000000000000000000000000000000000"
  note "deriving PKG_MIRROR_HASH with the zero-hash two-pass procedure"
  docker_run "$OUT/logs/$LABEL-derive.log" "$CB_DERIVE" \
    || die "derive pass failed (see $OUT/logs/$LABEL-derive.log)"
  MIRROR_HASH="$(sed -nE "s/.*Hash mismatch for file $TARBALL_NAME: expected [0-9a-f]{64}, got ([0-9a-f]{64}).*/\1/p" \
    "$OUT/logs/$LABEL-derive.log" | tail -1)"
  [[ "$MIRROR_HASH" =~ ^[0-9a-f]{64}$ ]] \
    || die "could not derive PKG_MIRROR_HASH (see $OUT/logs/$LABEL-derive.log; pass --mirror-hash)"
  MIRROR_DERIVED=true
  ok "derived PKG_MIRROR_HASH $MIRROR_HASH"
  scratch_set_mirror "$MIRROR_HASH"
  # Second pass proves the pinned hash is accepted and the tarball matches.
  docker_run "$OUT/logs/$LABEL-derive2.log" "$CB_DERIVE2" \
    || die "download pass with the derived hash failed (see $OUT/logs/$LABEL-derive2.log)"
  GOT_TARBALL_SHA="$(grep -E "  dl/$TARBALL_NAME\$" "$OUT/logs/$LABEL-derive2.log" | tail -1 | cut -d' ' -f1)"
  [ "$GOT_TARBALL_SHA" = "$MIRROR_HASH" ] \
    || die "downloaded tarball sha256 '$GOT_TARBALL_SHA' != derived PKG_MIRROR_HASH $MIRROR_HASH"
  ok "mirror hash re-verified against the produced tarball"
fi

# --- 7. build -----------------------------------------------------------
docker_run "$OUT/logs/$LABEL-build.log" "$CB_BUILD" \
  || die "build failed (see $OUT/logs/$LABEL-build.log)"

APK_FILE="$OUT/goshisen-$PKG_VERSION-r$PKG_RELEASE-$LABEL.apk"
PAYLOAD_FILE="$OUT/daemon-$LABEL.bin"
[ -f "$APK_FILE" ] || die "expected apk $APK_FILE not produced"
[ -f "$PAYLOAD_FILE" ] || die "expected payload $PAYLOAD_FILE not produced"
APK_FILE_LABELED="$(basename "$APK_FILE")"
if [ "$CANONICAL_NAME" = 1 ]; then
  # The release path must stage the apk under exactly PKG_VERSION-rPKG_RELEASE
  # (apk-sign.yml fetches the draft asset by that name).
  cp "$APK_FILE" "$OUT/$APK_NAME"
  ok "also staged the canonical release name $APK_NAME (same bytes)"
fi
APK_SHA="$(sha256sum "$APK_FILE" | cut -d' ' -f1)"
PAYLOAD_SHA="$(sha256sum "$PAYLOAD_FILE" | cut -d' ' -f1)"
APK_SIZE="$(stat -c %s "$APK_FILE")"
PAYLOAD_SIZE="$(stat -c %s "$PAYLOAD_FILE")"
PAYLOAD_VERSION="$("$PAYLOAD_FILE" --version 2>&1 | head -1)"
PAYLOAD_GO="$(strings -n 8 "$PAYLOAD_FILE" | grep -A1 'Go buildinf:' | tail -1 | grep -oE 'go[0-9]+\.[0-9]+\.[0-9]+' | head -1)"
[ "$PAYLOAD_VERSION" = "goshisen $PKG_VERSION" ] \
  || die "payload --version '$PAYLOAD_VERSION' != 'goshisen $PKG_VERSION'"
[ -n "$PAYLOAD_GO" ] || die "payload has no readable Go buildinfo"
# Unsignedness and extractability are asserted INSIDE the container (apk-tools
# v3 only): OpenWrt 25.12 apk v3 is an ADB container, not a plain tar.gz, so a
# host-side `tar` probe cannot tell signed from unsigned. The build pass prints
# SIGNCHECK-UNSIGNED-OK only after `apk verify` reports UNTRUSTED (i.e. nothing
# trusts this file yet) and the payload was extracted successfully.
grep -q 'SIGNCHECK-UNSIGNED-OK' "$OUT/logs/$LABEL-build.log" \
  || die "the build pass did not report SIGNCHECK-UNSIGNED-OK (see $OUT/logs/$LABEL-build.log)"

# --- 8. manifest --------------------------------------------------------
RECIPE_JSON="$(for rel in "${!RECIPE_SHA[@]}"; do
    printf '%s\n%s\n' "$rel" "${RECIPE_SHA[$rel]}"
  done | jq -Rn '[inputs] | [range(0; length; 2) as $i | {key: .[$i], value: .[$i+1]}] | from_entries')"

MANIFEST="$OUT/build-manifest-$LABEL.json"
jq -n \
  --arg created "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --arg label "$LABEL" \
  --arg commit "$COMMIT_SHA" --arg subject "$COMMIT_SUBJECT" \
  --arg pkgver "$PKG_VERSION" --arg pkgrel "$PKG_RELEASE" \
  --arg srcver "$PKG_SOURCE_VERSION" --arg srcres "$PKG_SOURCE_RESOLVED" \
  --arg mirror "$MIRROR_HASH" --argjson derived "$MIRROR_DERIVED" \
  --argjson recipe "$RECIPE_JSON" \
  --arg overlay "$OVERLAY_REL" --arg overlaysha "$OVERLAY_SHA" \
  --arg cfg "$SDK_CONFIG_REL" --arg cfgsha "$SDK_CONFIG_SHA" \
  --arg mksha "$MAKEFILE_FINAL_SHA" --argjson subs "$SUBS_JSON" \
  --arg sdk "$SDK_IMAGE" --arg sdktag "$SDK_TAG" \
  --arg gover "$GO_VERSION" --arg gotar "$GO_TARBALL" \
  --arg gotarsha "$GO_TARBALL_SHA256" --arg gotarsrc "$GO_SOURCE" \
  --arg gobinsha "$GO_BIN_SHA256" \
  --arg apkname "$APK_NAME" --arg apkfile "$APK_FILE_LABELED" --arg apksha "$APK_SHA" \
  --argjson apksize "$APK_SIZE" \
  --arg paysha "$PAYLOAD_SHA" --argjson paysize "$PAYLOAD_SIZE" \
  --arg payver "$PAYLOAD_VERSION" --arg paygo "$PAYLOAD_GO" \
  '{
     manifest_version: 1,
     created_utc: $created,
     label: $label,
     generator: "scripts/build-apk-release.sh",
     source: {commit: $commit, commit_subject: $subject, tree_clean: true},
     pkg: {
       name: "goshisen", version: $pkgver, release: $pkgrel,
       source_version: $srcver, source_version_resolved: $srcres,
       mirror_hash: $mirror, mirror_hash_derived: $derived
     },
     recipe: {
       dir: "openwrt", committed_sha256: $recipe,
       overlay: {path: $overlay, sha256: $overlaysha},
       sdk_config: {path: $cfg, sha256: $cfgsha},
       makefile_final_sha256: $mksha,
       substitutions: $subs
     },
     build: {
       sdk_image: $sdk, sdk_tag: $sdktag, goarch: "amd64",
       go: {version: $gover, tarball: $gotar, tarball_sha256: $gotarsha,
            bin_go_sha256: $gobinsha, source: $gotarsrc, gotoolchain: "local"},
       env: {CGO_ENABLED: "0", GOOS: "linux", GOARCH: "amd64",
             GOTOOLCHAIN: "local", TMPDIR: "/builder/tmp",
             GOCACHE: "/builder/tmp/gocache", GOMODCACHE: "/builder/tmp/gomod"},
       strip: true,
       buildid: "omitted (openwrt/Makefile does not pass -buildid=)"
     },
     artifacts: {
       apk: {name: $apkname, file: $apkfile, sha256: $apksha, size: $apksize,
             signed: false,
             apk_verify: "UNTRUSTED signature (apk verify --keys-dir <empty>, rc=1) — signature added later by apk-sign.yml"},
       payload: {path: "usr/sbin/goshisen", sha256: $paysha, size: $paysize,
                 version: $payver, go_buildinfo: $paygo}
     }
   }' > "$MANIFEST"
jq -e . "$MANIFEST" >/dev/null || die "manifest $MANIFEST is not valid JSON"

echo
echo "=== build $LABEL complete ==="
echo "commit            $COMMIT_SHA"
echo "PKG_VERSION       $PKG_VERSION (PKG_RELEASE $PKG_RELEASE, label $LABEL)"
echo "PKG_SOURCE_VERSION $PKG_SOURCE_VERSION -> $PKG_SOURCE_RESOLVED"
echo "PKG_MIRROR_HASH   $MIRROR_HASH (derived=$MIRROR_DERIVED)"
echo "sdk image         $SDK_IMAGE"
echo "go                go$GO_VERSION (bin/go $GO_BIN_SHA256)"
echo "apk               $(basename "$APK_FILE") $APK_SHA ($APK_SIZE bytes)"
echo "payload           $PAYLOAD_SHA ($PAYLOAD_SIZE bytes) $PAYLOAD_GO"
echo "manifest          $MANIFEST"
echo "BUILD-APK-RELEASE-OK-$LABEL"