#!/usr/bin/env bash
# GoShiSen-Enforce OpenWrt lab image preparation.
#
# Builds a bootable OpenWrt QEMU image provisioned with:
#   - the GoShiSen-Enforce daemon binary
#   - its daemon config and the packaged fail-closed policy
#   - a LAN bridge (br-lan on eth1, 10.0.3.1/24) + DHCP WAN on eth0
#     (matches scripts/openwrt-lab.sh's topology)
#   - an rc.local autostart of the daemon (log at /tmp/goshisen2.log)
#
# The stock image is the official OpenWrt release ext4-combined raw
# image; its sha256 is verified against the release manifest before any
# modification. Provisioning writes into the rootfs partition via a
# loop device (root required).
#
# Usage:
#   scripts/openwrt-image-prep.sh \
#     --base openwrt-25.12.6-x86-64-generic-ext4-combined.img.gz \
#     --daemon /path/to/goshisen \
#     --output /tmp/owrt.img
#   # or download the pinned release automatically:
#   scripts/openwrt-image-prep.sh --download --daemon ... --output ...
#
# Requires: root (losetup/partx/mount), gzip, sha256sum.
set -euo pipefail

PINNED_RELEASE=25.12.6
PINNED_IMAGE=openwrt-$PINNED_RELEASE-x86-64-generic-ext4-combined.img.gz
PINNED_URL="https://downloads.openwrt.org/releases/$PINNED_RELEASE/targets/x86/64/generic/$PINNED_IMAGE"
ROOTFS_PART=2

# Kernel version for the pinned release (from the kmods directory).
KVER=6.12.112-1-b4125196ecec75391093e1e2edeba490
KMOD_BASE="https://downloads.openwrt.org/releases/$PINNED_RELEASE/targets/x86/64/kmods/$KVER"
# sha256 of the queue kmods for this kernel.
KMOD_NFNETLINK_SHA=5ba9879f171dbce8a9a998b609b30f7315134a2c859b42e707fd72b42c6a09e9
KMOD_NFT_SHA=0fd1adc999e1037ecda7ed9a6101e28432b899239adab6a125e11f3dd2f68d16
KMOD_CTNETLINK_SHA=a22d2782d2c7d83fc3c993db9d874a6aad5eb541c6da6461535efda19c2b92f4

die() { echo "[image-prep] error: $*" >&2; exit 1; }
note() { echo "[image-prep] $*"; }
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

BASE=""
DAEMON=""
OUT=""
DOWNLOAD=0
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
    BASE="/tmp/$PINNED_IMAGE"
    note "downloading $PINNED_URL"
    curl -fL --retry 3 -o "$BASE" "$PINNED_URL"
    # Verify against the release manifest.
    curl -fL --retry 3 -o /tmp/openwrt-sha256sums \
        "https://downloads.openwrt.org/releases/$PINNED_RELEASE/targets/x86/64/sha256sums"
    grep -F "$PINNED_IMAGE" /tmp/openwrt-sha256sums > /tmp/expected.sha
    (cd /tmp && sha256sum -c expected.sha) || die "checksum mismatch for $PINNED_IMAGE"
fi

[ -n "$BASE" ] || die "--base is required (or use --download)"
[ -f "$BASE" ] || die "base image not found: $BASE"
[ -n "$DAEMON" ] || die "--daemon binary path is required"
[ -f "$DAEMON" ] || die "daemon binary not found: $DAEMON"
[ "$(id -u)" = 0 ] || die "must run as root (loop mounts)"

# Build the flat raw image.
rm -f "$OUT"
zcat "$BASE" > "$OUT"

LO=$(losetup --find --show -P "$OUT")
trap 'losetup -D 2>/dev/null || true' EXIT
partx -a "$LO" >/dev/null 2>&1 || true
sleep 0.5

# Locate the rootfs partition (ext4).
ROOT_DEV="${LO}p${ROOTFS_PART}"
[ -b "$ROOT_DEV" ] || die "rootfs partition $ROOT_DEV not found in $OUT"

MNT=$(mktemp -d)
trap 'umount "$MNT" 2>/dev/null || true; losetup -D 2>/dev/null || true; rmdir "$MNT" 2>/dev/null || true' EXIT
mount "$ROOT_DEV" "$MNT"

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

# 3b. Queue kernel modules (NFQUEUE + nft-queue). OpenWrt's base
# image does not ship them; without them the daemon's queue rule cannot
# be created. Download the pinned kmod packages and install them into
# the rootfs offline (apk --root, --allow-untrusted from local files).
note "installing NFQUEUE kmods"
mkdir -p "$WORK/apks"
curl -fL --retry 3 -o "$WORK/apks/kmod-nfnetlink-queue.apk" \
    "$KMOD_BASE/kmod-nfnetlink-queue-6.12.112-r1.apk"
curl -fL --retry 3 -o "$WORK/apks/kmod-nft-queue.apk" \
    "$KMOD_BASE/kmod-nft-queue-6.12.112-r1.apk"
curl -fL --retry 3 -o "$WORK/apks/kmod-nf-conntrack-netlink.apk" \
    "$KMOD_BASE/kmod-nf-conntrack-netlink-6.12.112-r1.apk"
echo "$KMOD_NFNETLINK_SHA  $WORK/apks/kmod-nfnetlink-queue.apk" | sha256sum -c -
echo "$KMOD_NFT_SHA  $WORK/apks/kmod-nft-queue.apk" | sha256sum -c -
echo "$KMOD_CTNETLINK_SHA  $WORK/apks/kmod-nf-conntrack-netlink.apk" | sha256sum -c -
install -m 0644 "$WORK/apks/kmod-nfnetlink-queue.apk" \
    "$WORK/apks/kmod-nft-queue.apk" \
    "$WORK/apks/kmod-nf-conntrack-netlink.apk" \
    "$MNT/tmp/"
chroot "$MNT" /bin/sh -c \
    "apk add --no-network --allow-untrusted --root / /tmp/kmod-nfnetlink-queue.apk /tmp/kmod-nft-queue.apk /tmp/kmod-nf-conntrack-netlink.apk" \
    >/dev/null
rm -f "$MNT/tmp/kmod-nfnetlink-queue.apk" "$MNT/tmp/kmod-nft-queue.apk" \
    "$MNT/tmp/kmod-nf-conntrack-netlink.apk"

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

umount "$MNT"
losetup -D
trap - EXIT

note "done: $OUT (daemon $(stat -c%s "$DAEMON") bytes, $(stat -c%s "$OUT") bytes image)"
