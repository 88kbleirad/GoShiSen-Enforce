#!/usr/bin/env bash
# GoShiSen-Enforce OpenWrt lab image preparation.
#
# Builds a bootable OpenWrt QEMU image provisioned with:
#   - the GoShiSen-Enforce daemon binary
#   - its daemon config and the packaged fail-closed policy
#   - a LAN bridge (br-lan on eth1, 10.0.3.1/24) + DHCP WAN on eth0
#     (matches scripts/openwrt-lab.sh's topology)
#   - the NFQUEUE kernel modules the daemon needs
#   - an rc.local autostart of the daemon (log at /tmp/goshisen2.log)
#
# The base is the official OpenWrt release ext4-combined raw image; its
# sha256 is verified against the release manifest before any
# modification. Provisioning writes into the rootfs partition via a
# loop device (root required), and detaches only that device.
#
# Usage:
#   scripts/openwrt-image-prep.sh \
#     --base openwrt-25.12.6-x86-64-generic-ext4-combined.img.gz \
#     --daemon /path/to/goshisen \
#     --output /tmp/owrt.img
#   # or download the pinned release (checksum-verified) automatically:
#   scripts/openwrt-image-prep.sh --download --daemon ... --output ...
#   # print the pinned URLs/checksums/ABI and exit (no root, no network):
#   scripts/openwrt-image-prep.sh --print-config
#
# Environment overrides (used by tests):
#   OPENWRT_BASE_URL   base download host (default https://downloads.openwrt.org)
#
# Requires: root (losetup/partx/mount), gzip, sha256sum, curl (download).
set -euo pipefail

PINNED_RELEASE=25.12.6
PINNED_IMAGE=openwrt-$PINNED_RELEASE-x86-64-generic-ext4-combined.img.gz
ROOTFS_PART=2

# Kernel ABI of the pinned release's kmods. KVER is the full
# "<kernel>-<pkgrel>-<sourcehash>" directory name; the kmod package
# filenames embed the kernel version and the package release ("r1").
KVER=6.12.112-1-b4125196ecec75391093e1e2edeba490
KMOD_KERNEL=$(printf '%s' "$KVER" | cut -d- -f1)
KMOD_PKGREL=r1

# sha256 of the queue/conntrack kmods for this kernel ABI.
KMOD_NFNETLINK_NAME=kmod-nfnetlink-queue-$KMOD_KERNEL-$KMOD_PKGREL.apk
KMOD_NFT_NAME=kmod-nft-queue-$KMOD_KERNEL-$KMOD_PKGREL.apk
KMOD_CTNETLINK_NAME=kmod-nf-conntrack-netlink-$KMOD_KERNEL-$KMOD_PKGREL.apk
KMOD_NFNETLINK_SHA=5ba9879f171dbce8a9a998b609b30f7315134a2c859b42e707fd72b42c6a09e9
KMOD_NFT_SHA=0fd1adc999e1037ecda7ed9a6101e28432b899239adab6a125e11f3dd2f68d16
KMOD_CTNETLINK_SHA=a22d2782d2c7d83fc3c993db9d874a6aad5eb541c6da6461535efda19c2b92f4

# The official release index keeps the images directly under
# targets/x86/64/ (there is no "generic/" subdirectory; that path
# returns HTTP 404).
TARGET_DIR="releases/$PINNED_RELEASE/targets/x86/64"
BASE_URL=${OPENWRT_BASE_URL:-https://downloads.openwrt.org}
IMAGE_URL="$BASE_URL/$TARGET_DIR/$PINNED_IMAGE"
MANIFEST_URL="$BASE_URL/$TARGET_DIR/sha256sums"
KMOD_BASE_URL="$BASE_URL/$TARGET_DIR/kmods/$KVER"

CURL_OPTS=(--fail --location --silent --show-error --retry 3 --retry-delay 2
           --connect-timeout 15)

die() { echo "[image-prep] error: $*" >&2; exit 1; }
note() { echo "[image-prep] $*"; }

# print_config emits the pinned constants as KEY=VALUE lines for tests
# and for humans; it performs no network or privileged operation.
print_config() {
    cat <<EOF
PINNED_RELEASE=$PINNED_RELEASE
PINNED_IMAGE=$PINNED_IMAGE
TARGET_DIR=$TARGET_DIR
IMAGE_URL=$IMAGE_URL
MANIFEST_URL=$MANIFEST_URL
KVER=$KVER
KMOD_KERNEL=$KMOD_KERNEL
KMOD_PKGREL=$KMOD_PKGREL
KMOD_BASE_URL=$KMOD_BASE_URL
KMOD_NFNETLINK_NAME=$KMOD_NFNETLINK_NAME
KMOD_NFT_NAME=$KMOD_NFT_NAME
KMOD_CTNETLINK_NAME=$KMOD_CTNETLINK_NAME
KMOD_NFNETLINK_SHA=$KMOD_NFNETLINK_SHA
KMOD_NFT_SHA=$KMOD_NFT_SHA
KMOD_CTNETLINK_SHA=$KMOD_CTNETLINK_SHA
ROOTFS_PART=$ROOTFS_PART
EOF
}

if [ "${1:-}" = "--print-config" ]; then
    print_config
    exit 0
fi

WORK=$(mktemp -d -t gsh-image-prep-XXXXXXXX)
BASE=""
DAEMON=""
OUT=""
DOWNLOAD=0
LO=""
MNT=""

# cleanup releases only the resources this script owns, in order:
# unmount the mountpoint, detach the loop device we allocated, remove
# the temporary directory. It never runs a global loop teardown.
cleanup() {
    local rc=$?
    if [ -n "$MNT" ] && mountpoint -q "$MNT" 2>/dev/null; then
        umount "$MNT" || echo "[image-prep] warning: umount $MNT failed" >&2
    fi
    if [ -n "$MNT" ] && [ -d "$MNT" ]; then
        rmdir "$MNT" 2>/dev/null || true
    fi
    if [ -n "$LO" ]; then
        losetup -d "$LO" 2>/dev/null || \
            echo "[image-prep] warning: losetup -d $LO failed" >&2
    fi
    if [ -n "$WORK" ] && [ -d "$WORK" ]; then
        rm -rf "$WORK"
    fi
    return $rc
}
trap cleanup EXIT

while [ $# -gt 0 ]; do
    case "$1" in
        --base) BASE=$2; shift 2 ;;
        --daemon) DAEMON=$2; shift 2 ;;
        --output) OUT=$2; shift 2 ;;
        --download) DOWNLOAD=1; shift ;;
        *) die "unknown argument: $1" ;;
    esac
done

[ -n "$OUT" ] || die "--output is required"

if [ "$DOWNLOAD" -eq 1 ]; then
    BASE="$WORK/$PINNED_IMAGE"
    note "downloading $IMAGE_URL"
    curl "${CURL_OPTS[@]}" -o "$BASE" "$IMAGE_URL" \
        || die "image download failed: $IMAGE_URL"
    [ -s "$BASE" ] || die "downloaded image is empty: $BASE"

    note "downloading $MANIFEST_URL"
    curl "${CURL_OPTS[@]}" -o "$WORK/sha256sums" "$MANIFEST_URL" \
        || die "checksum manifest download failed: $MANIFEST_URL"

    grep -F -- "$PINNED_IMAGE" "$WORK/sha256sums" > "$WORK/expected.sha" \
        || die "no manifest entry for $PINNED_IMAGE in $MANIFEST_URL"
    (cd "$WORK" && sha256sum -c --strict expected.sha) \
        || die "checksum verification failed for $PINNED_IMAGE"
    note "checksum verified: $(cut -d' ' -f1 "$WORK/expected.sha")"
fi

[ -n "$BASE" ] || die "--base is required (or use --download)"
[ -f "$BASE" ] || die "base image not found: $BASE"
[ -n "$DAEMON" ] || die "--daemon binary path is required"
[ -f "$DAEMON" ] || die "daemon binary not found: $DAEMON"
[ "$(id -u)" = 0 ] || die "must run as root (loop mounts)"

# Build the flat raw image.
rm -f "$OUT"
zcat "$BASE" > "$OUT" || die "decompress failed: $BASE"

LO=$(losetup --find --show -P "$OUT") || die "losetup failed for $OUT"
partx -a "$LO" >/dev/null 2>&1 || true
sleep 0.5

# Locate the rootfs partition (ext4).
ROOT_DEV="${LO}p${ROOTFS_PART}"
[ -b "$ROOT_DEV" ] || die "rootfs partition $ROOT_DEV not found in $OUT"

MNT=$(mktemp -d -t gsh-owrt-mnt-XXXXXXXX)
mount "$ROOT_DEV" "$MNT" || die "mount $ROOT_DEV failed"

note "provisioning rootfs at $MNT"

# 1. Daemon binary.
install -m 0755 "$DAEMON" "$MNT/usr/sbin/goshisen"

# 2. Daemon config (rendered form of openwrt/files/etc/config/goshisen).
mkdir -p "$MNT/etc/goshisen"
cat > "$MNT/etc/goshisen/config.yaml" <<EOF
policy_file: /etc/goshisen/policy.yaml
queue_num: 100
queue_maxlen: 1024
hook_scope: openwrt
log_level: debug
interface: br-lan
api_port: 0
metrics_port: 0
EOF

# 3. Packaged fail-closed policy.
install -m 0644 "$(dirname "${BASH_SOURCE[0]}")/../openwrt/files/etc/goshisen/policy.yaml" \
    "$MNT/etc/goshisen/policy.yaml"

# 3b. Kernel modules. The stock image ships no NFQUEUE/queue/conntrack
# modules, so the daemon's hook and its stateful ACCEPT path cannot work
# without them. Download the pinned kmod packages for this exact kernel
# ABI, verify their checksums, and install them offline into the rootfs.
note "installing NFQUEUE kmods ($KMOD_KERNEL)"
mkdir -p "$WORK/apks"
for spec in \
    "$KMOD_NFNETLINK_NAME:$KMOD_NFNETLINK_SHA" \
    "$KMOD_NFT_NAME:$KMOD_NFT_SHA" \
    "$KMOD_CTNETLINK_NAME:$KMOD_CTNETLINK_SHA"
do
    name=${spec%%:*}; sha=${spec##*:}
    curl "${CURL_OPTS[@]}" -o "$WORK/apks/$name" "$KMOD_BASE_URL/$name" \
        || die "kmod download failed: $KMOD_BASE_URL/$name"
    echo "$sha  $WORK/apks/$name" | sha256sum -c --strict - \
        || die "kmod checksum verification failed: $name"
done
install -m 0644 "$WORK/apks/$KMOD_NFNETLINK_NAME" \
    "$WORK/apks/$KMOD_NFT_NAME" \
    "$WORK/apks/$KMOD_CTNETLINK_NAME" \
    "$MNT/tmp/"
chroot "$MNT" /bin/sh -c \
    "apk add --no-network --allow-untrusted --root / /tmp/$KMOD_NFNETLINK_NAME /tmp/$KMOD_NFT_NAME /tmp/$KMOD_CTNETLINK_NAME" \
    >/dev/null || die "offline kmod installation failed"
rm -f "$MNT/tmp/$KMOD_NFNETLINK_NAME" "$MNT/tmp/$KMOD_NFT_NAME" \
    "$MNT/tmp/$KMOD_CTNETLINK_NAME"

# 4. Network: LAN bridge on eth1 (10.0.3.1/24), DHCP WAN on eth0.
cat > "$MNT/etc/config/network" <<'EOF'
config interface 'loopback'
	option device 'lo'
	option proto 'static'
	list ipaddr '127.0.0.1/8'

config globals 'globals'
	option ula_prefix 'fdcd:ea48:12a4::/48'

config device
	option name 'br-lan'
	option type 'bridge'
	list ports 'eth1'

config interface 'lan'
	option device 'br-lan'
	option proto 'static'
	list ipaddr '10.0.3.1/24'

config interface 'wan'
	option device 'eth0'
	option proto 'dhcp'
EOF

# 5. Autostart the daemon at boot (matches the lab's manual invocation).
if ! grep -q "goshisen" "$MNT/etc/rc.local" 2>/dev/null; then
    sed -i '/^exit 0/i /usr/sbin/goshisen --config /etc/goshisen/config.yaml >/tmp/goshisen2.log 2>&1 &' \
        "$MNT/etc/rc.local"
fi

umount "$MNT" || die "umount $MNT failed"
rmdir "$MNT" || true
MNT=""
losetup -d "$LO" || die "losetup -d $LO failed"
LO=""

note "done: $OUT (daemon $(stat -c%s "$DAEMON") bytes, $(stat -c%s "$OUT") bytes image)"
