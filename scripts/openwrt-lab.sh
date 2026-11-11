#!/usr/bin/env bash
# GoShiSen-Enforce OpenWrt QEMU lab.
#
# Topology:
#
#   WAN: QEMU user-mode (slirp) — isolated 10.0.2.0/24, no host
#        network access. Port 127.0.0.1:2222 forwards to the VM's SSH.
#   LAN: host TAP (gsh-owrt0) bridged to a host network namespace
#        (gsh_owrt_lan) that acts as the virtual LAN client.
#        (10.0.3.0/24)
#
# The lab never touches the host firewall, libvirt, or real devices.
# It needs root for the TAP/namespace parts (sudo -n is used when not
# already root).
#
# Usage:
#   scripts/openwrt-lab.sh up [port]    boot VM + LAN client ns
#   scripts/openwrt-lab.sh down          stop everything
#   scripts/openwrt-lab.sh console       attach to serial console
#   scripts/openwrt-lab.sh client cmd... run a command in the LAN ns
set -euo pipefail

IMAGE=${OPENWRT_IMAGE:-/tmp/owrt.img}
PIDFILE=${OPENWRT_PIDFILE:-/tmp/owrt-qemu.pid}
LOG=${OPENWRT_LOG:-/tmp/owrt-console.log}
BR=gsh-owrt-br
TAP=gsh-owrt0
NS=gsh_owrt_lan
VETH=gsh-owrt-v0
LAN_IP_SUBNET=10.0.3.0/24
LAN_GW=10.0.3.1
LAN_CLIENT=10.0.3.2

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

up() {
    local port=${1:-5555}
    [ -f "$IMAGE" ] || die "OpenWrt image not found: $IMAGE (set OPENWRT_IMAGE)"
    if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
        echo "OpenWrt lab already running (pid $(cat "$PIDFILE"))"
        return 0
    fi

    # LAN client namespace + TAP on a bridge.
    run_root ip netns add "$NS" 2>/dev/null || true
    run_root ip link add "$BR" type bridge 2>/dev/null || true
    run_root ip tuntap add dev "$TAP" mode tap 2>/dev/null || true
    run_root ip link add "$VETH" type veth peer name veth1
    run_root ip link set veth1 netns "$NS"
    run_root ip link set "$VETH" master "$BR"
    run_root ip link set "$TAP" master "$BR"
    run_root ip link set "$BR" up
    run_root ip link set "$TAP" up
    run_root ip link set "$VETH" up

    # br_netfilter + Docker's FORWARD policy DROP would silently drop
    # bridged lab traffic. Add narrowly-scoped ACCEPT rules for this
    # lab bridge only (removed on down). This does not touch Docker's
    # own chains or any other forward traffic.
    run_root iptables -C FORWARD -i "$BR" -j ACCEPT 2>/dev/null || \
        run_root iptables -I FORWARD 1 -i "$BR" -j ACCEPT
    run_root iptables -C FORWARD -o "$BR" -j ACCEPT 2>/dev/null || \
        run_root iptables -I FORWARD 1 -o "$BR" -j ACCEPT
    run_root ip netns exec "$NS" ip link set lo up
    run_root ip netns exec "$NS" ip link set veth1 up
    run_root ip netns exec "$NS" ip addr add "$LAN_CLIENT/24" dev veth1
    run_root ip netns exec "$NS" ip route add default via "$LAN_GW"

    qemu-system-x86_64 \
        -machine pc -m 512 -smp 2 -no-reboot \
        -drive file="$IMAGE",format=raw,if=virtio \
        -netdev user,id=wan0,hostfwd=tcp:127.0.0.1:2222-:22 \
        -device virtio-net-pci,netdev=wan0 \
        -netdev tap,id=lan0,ifname="$TAP",script=no,downscript=no \
        -device virtio-net-pci,netdev=lan0 \
        -serial "telnet:127.0.0.1:$port,server,nowait" \
        -monitor none -display none \
        > "$LOG" 2>&1 &
    echo $! > "$PIDFILE"
    echo "OpenWrt lab up (serial console: 127.0.0.1:$port, pid $(cat "$PIDFILE"))"
}

down() {
    if [ -f "$PIDFILE" ]; then
        kill "$(cat "$PIDFILE")" 2>/dev/null || true
        rm -f "$PIDFILE"
    fi
    run_root ip netns del "$NS" 2>/dev/null || true
    run_root ip link del "$TAP" 2>/dev/null || true
    run_root ip link del "$BR" 2>/dev/null || true
    run_root iptables -D FORWARD -i "$BR" -j ACCEPT 2>/dev/null || true
    run_root iptables -D FORWARD -o "$BR" -j ACCEPT 2>/dev/null || true
    echo "OpenWrt lab down"
}

console() {
    local port=${1:-5555}
    exec nc 127.0.0.1 "$port"
}

client() {
    shift
    run_root ip netns exec "$NS" "$@"
}

case "${1:-}" in
    up) up "${2:-5555}" ;;
    down) down ;;
    console) console "${2:-5555}" ;;
    client) client "$@" ;;
    *) die "usage: $0 {up|down|console|client} [port|args]" ;;
esac
