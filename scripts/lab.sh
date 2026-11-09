#!/usr/bin/env bash
# GoShiSen-Enforce lab: isolated network namespaces + veth pairs + a
# router namespace.
#
# The lab is fully self-contained: it touches only namespaces prefixed
# with gsh_, veth pairs it creates, and the nftables table `gsh_lab`
# INSIDE the router namespace. The host firewall is never listed,
# flushed, or modified.
#
# Usage:
#   scripts/lab.sh up      create the lab (idempotent)
#   scripts/lab.sh down    tear everything down (idempotent)
#   scripts/lab.sh status  print current lab state
#
# Requires root (CAP_NET_ADMIN) and the ip/nft tools.

set -euo pipefail

PREFIX=gsh_
NS_FW=${PREFIX}fw
NS_C=${PREFIX}client
NS_S=${PREFIX}server

VETH_FW_C=gsh-fw0
VETH_C_FW=gsh-c0
VETH_FW_S=gsh-fw1
VETH_S_FW=gsh-s0

IP_FW_C=10.99.0.1/24
IP_C=10.99.0.2/24
IP_FW_S=10.99.1.1/24
IP_S=10.99.1.2/24

# Public alias address hosted on the firewall for DNAT tests.
IP_PUB=10.99.0.100/24

die() {
  echo "lab: error: $*" >&2
  exit 1
}

root_check() {
  [ "$(id -u)" = 0 ] || die "must run as root (needs CAP_NET_ADMIN)"
  command -v ip >/dev/null || die "ip tool missing"
  command -v nft >/dev/null || die "nft tool missing"
}

ns_exists() { ip netns list | grep -qx "$1"; }

up() {
  local cid
  cid=$(ip netns list | awk '$1 ~ /^gsh_/ {print $1}' | grep -c . || true)
  if [ "$cid" -gt 0 ]; then
    echo "lab: namespaces already present ($cid); run 'lab.sh down' first"
    exit 1
  fi

  ip netns add "$NS_FW"
  ip netns add "$NS_C"
  ip netns add "$NS_S"

  ip link add "$VETH_FW_C" type veth peer name "$VETH_C_FW"
  ip link add "$VETH_FW_S" type veth peer name "$VETH_S_FW"

  ip link set "$VETH_C_FW" netns "$NS_C"
  ip link set "$VETH_S_FW" netns "$NS_S"
  ip link set "$VETH_FW_C" netns "$NS_FW"
  ip link set "$VETH_FW_S" netns "$NS_FW"

  ip netns exec "$NS_FW" ip link set lo up
  ip netns exec "$NS_FW" ip link set "$VETH_FW_C" up
  ip netns exec "$NS_FW" ip link set "$VETH_FW_S" up
  ip netns exec "$NS_FW" ip addr add "$IP_FW_C" dev "$VETH_FW_C"
  ip netns exec "$NS_FW" ip addr add "$IP_FW_S" dev "$VETH_FW_S"
  ip netns exec "$NS_FW" ip addr add "$IP_PUB" dev "$VETH_FW_C"

  ip netns exec "$NS_C" ip link set lo up
  ip netns exec "$NS_C" ip link set "$VETH_C_FW" up
  ip netns exec "$NS_C" ip addr add "$IP_C" dev "$VETH_C_FW"
  ip netns exec "$NS_C" ip route add default via 10.99.0.1
  # For MASQUERADE tests (client egress out of the lab is not required;
  # the alias network is used to reach the public IP).
  ip netns exec "$NS_C" ip route add 10.99.0.100/32 via 10.99.0.1 2>/dev/null || true

  ip netns exec "$NS_S" ip link set lo up
  ip netns exec "$NS_S" ip link set "$VETH_S_FW" up
  ip netns exec "$NS_S" ip addr add "$IP_S" dev "$VETH_S_FW"
  ip netns exec "$NS_S" ip route add default via 10.99.1.1

  # Router behavior inside the firewall namespace only.
  ip netns exec "$NS_FW" sysctl -w net.ipv4.ip_forward=1 >/dev/null

  # The only nftables changes in the whole system: inside $NS_FW.
  ip netns exec "$NS_FW" nft -f - <<'NFT'
table ip gsh_lab {
    chain forward {
        type filter hook forward priority filter; policy drop;
        meta l4proto { tcp, udp } counter queue num 0
    }
}
NFT

  echo "lab: up (router=$NS_FW client=$NS_C server=$NS_S)"
}

down() {
  # nft state lives in the namespace; deleting the namespace removes it.
  for ns in "$NS_FW" "$NS_C" "$NS_S"; do
    ip netns del "$ns" 2>/dev/null || true
  done
  # Veth peers outside namespaces are gone with their namespaces; any
  # leftovers (shouldn't exist) are removed defensively.
  for v in "$VETH_FW_C" "$VETH_FW_S"; do
    ip link del "$v" 2>/dev/null || true
  done
  echo "lab: down"
}

status() {
  for ns in "$NS_FW" "$NS_C" "$NS_S"; do
    if ns_exists "$ns"; then
      echo "  $ns: present"
    else
      echo "  $ns: absent"
    fi
  done
}

case "${1:-}" in
  up) root_check; up ;;
  down) root_check; down ;;
  status) status ;;
  *) echo "usage: $0 {up|down|status}" >&2; exit 2 ;;
esac
