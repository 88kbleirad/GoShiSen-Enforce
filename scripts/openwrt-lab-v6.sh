#!/usr/bin/env bash
# GoShiSen-Enforce OpenWrt QEMU lab — IPv6 dual-tap variant.
#
# Topology (dual TAP; no slirp, so IPv6 forwarding actually crosses the
# router and must hit the inet goshisen hook queue 101):
#
#   LAN ns  gsh_v6_lan : veth 10.0.3.2/24 + fd70:1::2/64, gw 10.0.3.1/fd70:1::1
#   WAN ns  gsh_v6_wan : veth 10.0.2.2/24 + fd00::2/64 (marker server)
#   guest eth1 (LAN): 10.0.3.1/24 + fd70:1::1/64 (br-lan from image-prep)
#   guest eth0 (WAN): 10.0.2.15/24 + fd00::1/64, default via 10.0.2.2/fd00::2
#
# The image is provisioned by scripts/openwrt-image-prep.sh (daemon +
# kmods + rc.local autostart baked in); the guest's LAN bridge and
# DHCP-vs-static WAN are configured by the v6 e2e script after boot.
#
# Usage:
#   scripts/openwrt-lab-v6.sh up [port]
#   scripts/openwrt-lab-v6.sh down
#   scripts/openwrt-lab-v6.sh console [port]
#   scripts/openwrt-lab-v6.sh client wan|lan cmd...
set -euo pipefail

IMAGE=${OPENWRT_IMAGE:-/tmp/owrt.img}
PIDFILE=${OPENWRT_V6_PIDFILE:-/tmp/owrt-v6-qemu.pid}
LOG=${OPENWRT_V6_LOG:-/tmp/owrt-v6-console.log}
BR_LAN=gsh-v6br0
BR_WAN=gsh-v6br1
TAP_LAN=gsh-v6tap0
TAP_WAN=gsh-v6tap1
NS_LAN=gsh_v6_lan
NS_WAN=gsh_v6_wan
VETH_LAN=gsh-v6l0
VETH_WAN=gsh-v6w0
LAN_GW=10.0.3.1
LAN_CLIENT=10.0.3.2
LAN_CLIENT_SUBNET=10.0.3.0/24
WAN_GW=10.0.2.2
WAN_IP=10.0.2.15
V6_LAN=fd70:1::1
V6_CLIENT=fd70:1::2
V6_LAN_SUBNET=fd70:1::/64
V6_WAN=fd00::1
V6_GW=fd00::2

die() {
    echo "ERROR: $*" >&2
    exit 1
}

run_root() {
    if [ "$(id -u)" = 0 ]; then
        "$@"
    else
        sudo -n "$@"
    fi
}

cleanup_resources() {
    pkill -9 -f "qemu-system-[x]86_64.*gsh-v6tap" 2>/dev/null || true
    run_root ip netns del "$NS_LAN" 2>/dev/null || true
    run_root ip netns del "$NS_WAN" 2>/dev/null || true
    for x in "$TAP_LAN" "$TAP_WAN" "$BR_LAN" "$BR_WAN" "$VETH_LAN" "$VETH_WAN"; do
        run_root ip link del "$x" 2>/dev/null || true
    done
    for br in "$BR_LAN" "$BR_WAN"; do
        run_root iptables -D FORWARD -i "$br" -j ACCEPT 2>/dev/null || true
        run_root iptables -D FORWARD -o "$br" -j ACCEPT 2>/dev/null || true
    done
}

up() {
    local port=${1:-5555}
    [ -f "$IMAGE" ] || die "OpenWrt image not found: $IMAGE (set OPENWRT_IMAGE)"
    if [ -f "$PIDFILE" ]; then
        pid=$(cat "$PIDFILE" 2>/dev/null || true)
        if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null && \
           grep -q "gsh-v6tap" "/proc/$pid/cmdline" 2>/dev/null; then
            echo "OpenWrt v6 lab already running (pid $pid)"
            return 0
        fi
        rm -f "$PIDFILE"
    fi
    cleanup_resources

    # LAN client namespace + TAP on a bridge.
    run_root ip netns add "$NS_LAN" 2>/dev/null || true
    run_root ip link add "$BR_LAN" type bridge 2>/dev/null || true
    run_root ip tuntap add dev "$TAP_LAN" mode tap 2>/dev/null || true
    run_root ip link add "$VETH_LAN" type veth peer name veth-v6lan 2>/dev/null || true
    run_root ip link set veth-v6lan netns "$NS_LAN"
    run_root ip link set "$VETH_LAN" master "$BR_LAN"
    run_root ip link set "$TAP_LAN" master "$BR_LAN"
    run_root ip link set "$BR_LAN" up
    run_root ip link set "$TAP_LAN" up
    run_root ip link set "$VETH_LAN" up
    run_root ip netns exec "$NS_LAN" ip link set lo up
    run_root ip netns exec "$NS_LAN" ip link set veth-v6lan up
    run_root ip netns exec "$NS_LAN" ip addr add "$LAN_CLIENT/24" dev veth-v6lan
    run_root ip netns exec "$NS_LAN" ip -6 addr add "$V6_CLIENT/64" dev veth-v6lan
    run_root ip netns exec "$NS_LAN" ip route add default via "$LAN_GW"
    run_root ip netns exec "$NS_LAN" ip -6 route add default via "$V6_LAN"

    # WAN namespace + TAP on a bridge (marker server side).
    run_root ip netns add "$NS_WAN" 2>/dev/null || true
    run_root ip link add "$BR_WAN" type bridge 2>/dev/null || true
    run_root ip tuntap add dev "$TAP_WAN" mode tap 2>/dev/null || true
    run_root ip link add "$VETH_WAN" type veth peer name veth-v6wan 2>/dev/null || true
    run_root ip link set veth-v6wan netns "$NS_WAN"
    run_root ip link set "$VETH_WAN" master "$BR_WAN"
    run_root ip link set "$TAP_WAN" master "$BR_WAN"
    run_root ip link set "$BR_WAN" up
    run_root ip link set "$TAP_WAN" up
    run_root ip link set "$VETH_WAN" up
    run_root ip netns exec "$NS_WAN" ip link set lo up
    run_root ip netns exec "$NS_WAN" ip link set veth-v6wan up
    run_root ip netns exec "$NS_WAN" ip addr add "$WAN_GW/24" dev veth-v6wan
    run_root ip netns exec "$NS_WAN" ip -6 addr add "$V6_GW/64" dev veth-v6wan
    # Return route to the LAN subnet via the guest's WAN address (the
    # guest forwards both families; without this the WAN side cannot
    # reach LAN clients, so replies (HTTP/SYN-ACK) are dropped as
    # unreachable).
    run_root ip netns exec "$NS_WAN" ip route add "$LAN_CLIENT_SUBNET" via "$WAN_IP" 2>/dev/null || true
    run_root ip netns exec "$NS_WAN" ip -6 route add "$V6_LAN_SUBNET" via "$V6_WAN" 2>/dev/null || true

    # br_netfilter + Docker's FORWARD policy DROP would silently drop
    # bridged lab traffic. Add narrowly-scoped ACCEPT rules for these
    # lab bridges only (removed on down).
    for br in "$BR_LAN" "$BR_WAN"; do
        run_root iptables -C FORWARD -i "$br" -j ACCEPT 2>/dev/null || \
            run_root iptables -I FORWARD 1 -i "$br" -j ACCEPT
        run_root iptables -C FORWARD -o "$br" -j ACCEPT 2>/dev/null || \
            run_root iptables -I FORWARD 1 -o "$br" -j ACCEPT
    done

    qemu-system-x86_64 \
        -machine pc -m 512 -smp 2 -no-reboot \
        -drive file="$IMAGE",format=raw,if=virtio \
        -netdev tap,id=wan0,ifname="$TAP_WAN",script=no,downscript=no \
        -device virtio-net-pci,netdev=wan0 \
        -netdev tap,id=lan0,ifname="$TAP_LAN",script=no,downscript=no \
        -device virtio-net-pci,netdev=lan0 \
        -serial "telnet:127.0.0.1:$port,server,nowait" \
        -monitor none -display none \
        > "$LOG" 2>&1 &
    echo $! > "$PIDFILE"
    echo "OpenWrt v6 lab up (serial console: 127.0.0.1:$port, pid $(cat "$PIDFILE"))"
}

down() {
    if [ -f "$PIDFILE" ]; then
        kill "$(cat "$PIDFILE")" 2>/dev/null || true
        rm -f "$PIDFILE"
    fi
    cleanup_resources
    echo "OpenWrt v6 lab down"
}

console() {
    local port=${1:-5555}
    exec nc 127.0.0.1 "$port"
}

client() {
    local ns=${1:-lan}
    shift
    if [ "$ns" = wan ]; then
        run_root ip netns exec "$NS_WAN" "$@"
    else
        run_root ip netns exec "$NS_LAN" "$@"
    fi
}

case "${1:-}" in
    up) up "${2:-5555}" ;;
    down) down ;;
    console) console "${2:-5555}" ;;
    client) client "$@" ;;
    *) die "usage: $0 {up|down|console|client} [port|wan|lan args]" ;;
esac
