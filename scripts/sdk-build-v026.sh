#!/usr/bin/env bash
# scripts/sdk-build-v026.sh — v0.2.6 GoShiSen-Enforce OpenWrt apk build recipe (S-005).
#
# Recommended Go-pinning approach for the v0.2.6 release build:
#
#   The apk payload is compiled inside the pinned OpenWrt SDK container
#   (openwrt/sdk@sha256:a17d6436f7add9207513b6edd0370f5a1aeeceeedf3c9080de8909112892f261,
#   tag x86_64-25.12.6). The image bakes feeds.conf.default pinning every
#   feed to a commit; `scripts/feeds update` honors those pins, so the
#   image's feed state is FIXED. That feed state's golang/host default is
#   go1.26.9 and golang1.27 is go1.27.1 (feed commit 093d551...). Both are
#   below the S-005 minimum (go1.27.4).
#
#   Repinning the packages feed to a newer commit (option 2, e.g.
#   46d425591 on openwrt-25.12 or db68036ba on master, both carrying
#   golang1.27=1.27.4) BREAKS the SDK's metadata scan in this image
#   (verified: "ERROR: please fix feeds/.../Makefile" for several
#   packages, e.g. admin/bottom, lang/python/python-cryptography), so a
#   feed pin is NOT a viable fix for this SDK snapshot.
#
#   The chosen approach (option 3): mount a Go toolchain verified against
#   the official go.dev sha256 into the SDK container, put it first on
#   PATH with GOTOOLCHAIN=local, and build. The package is still built by
#   the OpenWrt buildroot (legitimate OpenWrt-style build); only the Go
#   compiler is pinned, exactly as CI pins setup-go. The go.mod
#   `toolchain go1.27.4` line stays as a second layer, and the S-005
#   sign-time gates (scripts/check-apk-payload-go.sh +
#   scripts/check-apk-payload-govuln.sh) enforce the outcome.
#
#   Toolchain: go1.27.4 linux/amd64, official go.dev tarball
#     sha256 7226736c2339719cda28fbf3a54d71aa63bcefe376e98cef00034268dab24840
#   (verify with `sha256sum` before mounting).
#
#   Payload buildinfo requirement: the package Makefile must NOT pass
#   -buildid= to the Go linker AND must set STRIP:=true (v0.2.6
#   changes) so the govulncheck gate can parse the payload binary. The
#   SDK's default sstrip removes the ELF section string table, which
#   breaks `go version -m` and binary-mode govulncheck even when
#   -buildid= is kept (verified: go1.27.4 payload with -buildid= only
#   still failed; with STRIP:=true `go version -m` reads go1.27.4 and
#   govulncheck binary-mode reports no findings).
#
# Usage:
#   GOSHISEN_FEED=/path/to/repo/openwrt \
#   GO_1274=/path/to/verified/go1.27.4 \
#   OUT=/path/to/out \
#   bash scripts/sdk-build-v026.sh <label>
#
# Outputs in $OUT:
#   goshisen-<PKG_VERSION>-r1-<label>.apk, apk-<label>.sha256,
#   daemon-<label>.bin, daemon-<label>.sha256, version-<label>.txt
set -euo pipefail

GOSHISEN_FEED=${GOSHISEN_FEED:?set to the repo openwrt/ feed dir}
GO_1274=${GO_1274:?set to a verified go1.27.4 root (bin/go inside)}
SDK_IMAGE=${SDK_IMAGE:-openwrt/sdk@sha256:a17d6436f7add9207513b6edd0370f5a1aeeceeedf3c9080de8909112892f261}
OUT=${OUT:?set to an output dir}
LABEL=${1:?usage: sdk-build-v026.sh <label>}
PKG_VERSION=${PKG_VERSION:-0.2.6}
BUILD_DIR=${BUILD_DIR:-/data/s005-t3/bd-${LABEL}}
FEEDS_DIR=${FEEDS_DIR:-/data/s005-t3/feeds-${LABEL}}

# Sanity: the mounted Go must really be 1.27.4.
[ -x "$GO_1274/bin/go" ] || { echo "ERROR: $GO_1274/bin/go not found" >&2; exit 1; }
GO_SHA=$(sha256sum "$GO_1274/bin/go" | cut -d' ' -f1)
echo "using Go $GO_1274 (bin/go sha256 $GO_SHA)"

mkdir -p "$OUT" "$BUILD_DIR" "$FEEDS_DIR" /data/s005-t3/dl /data/s005-t3/tmp

# The SDK image ships a prebuilt kernel build dir under /builder/build_dir
# (needed for the kmod DEPENDS of CONFIG_ALL builds). A bind mount over
# /builder/build_dir shadows it, so copy it (via tar) into the bind first.
docker run --rm --network none --user root -v "$BUILD_DIR":/srv-bd --entrypoint sh \
  "$SDK_IMAGE" -c \
  'if [ -d /builder/build_dir/target-x86_64_musl ]; then cd /builder/build_dir && tar -cf - target-x86_64_musl | (cd /srv-bd && tar -xf -); fi' \
  >/dev/null 2>&1 || true

docker run --rm --network host --user root \
  -v "$BUILD_DIR":/builder/build_dir \
  -v "$FEEDS_DIR":/builder/feeds \
  -v /data/s005-t3/dl:/builder/dl \
  -v /data/s005-t3/tmp:/builder/tmp \
  -v "$GO_1274":/go-host:ro \
  -v "$GO_1274":/usr/local/go:ro \
  -v "$OUT":/out \
  -v "$GOSHISEN_FEED":/builder/package/feeds/goshisen:ro \
  "$SDK_IMAGE" \
  bash -c '
set -euxo pipefail
cd /builder
LABEL="$1"
echo "=== 1. feeds update (baked feeds.conf.default pins) ==="
./scripts/feeds update -a >/tmp/fu.log 2>&1 || { echo FEED-UPDATE-FAIL; tail -30 /tmp/fu.log; exit 1; }
for f in base packages luci routing telephony video; do
  git -C feeds/$f log -1 --format="FEED $f %H %ci" 2>/dev/null
done
grep -n "GO_DEFAULT_VERSION:=" feeds/packages/lang/golang/golang-values.mk
sed -n "10,16p" feeds/packages/lang/golang/golang1.27/Makefile
echo "=== 2. feeds install ==="
./scripts/feeds install -a >/tmp/fi.log 2>&1 || { echo FEED-INSTALL-FAIL; tail -30 /tmp/fi.log; exit 1; }
echo "=== 3. defconfig ==="
cp /out/sdk.config .config
make defconfig >/tmp/def.log 2>&1 || { tail -30 /tmp/def.log; exit 1; }
echo "=== 4. goshisen compile (mounted VERIFIED go1.27.4; GOTOOLCHAIN=local) ==="
export PATH=/go-host/bin:$PATH
export GOTOOLCHAIN=local
export TMPDIR=/builder/tmp GOTMPDIR=/builder/tmp GOCACHE=/builder/tmp/gocache GOMODCACHE=/builder/tmp/gomod
go version
make package/feeds/goshisen/compile V=s >/tmp/goshisen.log 2>&1 || { echo GOSHISEN-FAIL; tail -40 /tmp/goshisen.log; exit 1; }
echo "=== 5. capture evidence ==="
APK=$(find bin/packages -name "goshisen-*.apk" | head -1)
echo "APK=$APK"
sha256sum "$APK" | tee /out/apk-$LABEL.sha256
mkdir -p /tmp/apk-x
/builder/staging_dir/host/bin/apk extract --allow-untrusted --destination /tmp/apk-x "/builder/$APK" >/dev/null 2>&1
cp /tmp/apk-x/usr/sbin/goshisen /out/daemon-$LABEL.bin
sha256sum /out/daemon-$LABEL.bin | tee /out/daemon-$LABEL.sha256
/out/daemon-$LABEL.bin --version | tee /out/version-$LABEL.txt
cp "/builder/$APK" /out/$(basename "$APK" | sed "s/\.apk/-$LABEL.apk/")
echo "=== payload Go buildinfo ==="
strings -n 8 /out/daemon-$LABEL.bin | grep -A2 "Go buildinf:" | head -3
go version -m /out/daemon-$LABEL.bin | head -1 || true
echo BUILD-OK-$LABEL
exit 0
' bash "$LABEL"
