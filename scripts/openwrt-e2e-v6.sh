#!/usr/bin/env bash
# GoShiSen-Enforce — IPv6 stateful e2e (dual-tap lab, kernel 6.12.112).
#
# Runs the issue #5 stateful IPv6 scenarios against the dual-tap lab
# (scripts/openwrt-lab-v6.sh up). The router forwards IPv6, so traffic
# hits the inet goshisen hook queue 101. Every scenario is paired with
# its IPv4 control on the same path, with counter evidence:
#
#   1. v6 DENY policy blocks (curl fails, server saw no genuine request,
#      drop counter advanced); v4 control DENY blocks identically.
#   2. v6 ALLOW stateful policy completes (HTTP 200, accept counter
#      advanced); v4 control ALLOW completes identically.
#   3. one-directional policy (no return rule) is blocked for BOTH IPv4
#      and IPv6 (proves it is policy semantics, not an IPv6 gap).
#   4. switching back from ALLOW to DENY blocks new flows (v4+v6).
#
# Prerequisites:
#   - v6 lab up (scripts/openwrt-lab-v6.sh up)
#   - root (or sudo -n) for client-namespace curl
#   - the image has the daemon + kmods + rc.local autostart (image-prep)
#
# Exit codes:
#   0  PASS
#   1  FAIL
#   3  ENV
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
CONSOLE="$ROOT/scripts/openwrt-console.py"
NS_LAN=gsh_v6_lan
NS_WAN=gsh_v6_wan
CONSOLE_PORT=${OPENWRT_V6_CONSOLE_PORT:-5555}
SRV_PORT=${OPENWRT_V6_HTTP_PORT:-8089}
SRV_IP4=10.0.2.2
SRV_IP6=fd00::2
LAN_IP=10.0.3.1
V6_LAN=fd70:1::1
WAN_IP=10.0.2.15
V6_WAN=fd00::1
MARKER=GSH-V6-PROOF
QUEUE6=101
QUEUE4=100
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

note() { echo "[e2e-v6] $*"; }
fail() { echo "[e2e-v6] FAIL: $*" >&2; exit 1; }
denv() { echo "[e2e-v6] ENV: $*" >&2; exit 3; }

need() {
    local t
    for t in "$@"; do
        command -v "$t" >/dev/null 2>&1 || denv "required tool $t missing"
    done
}

vm() {
    python3 "$CONSOLE" --port "$CONSOLE_PORT" --cmd "$1" --timeout "${2:-25}" 2>&1 \
        | tr -c '[:print:]\n\r\t' '?' | sed 's/\r//g'
}

client() { # client <lan|wan> cmd...
    local ns=$1; shift
    if [ "$ns" = wan ]; then
        if [ "$(id -u)" = 0 ]; then ip netns exec "$NS_WAN" "$@"
        else sudo -n ip netns exec "$NS_WAN" "$@"; fi
    else
        if [ "$(id -u)" = 0 ]; then ip netns exec "$NS_LAN" "$@"
        else sudo -n ip netns exec "$NS_LAN" "$@"; fi
    fi
}

# ---------------------------------------------------------------------------
# Guest network bootstrap (dual-stack; image-prep bakes br-lan on eth1).
# ---------------------------------------------------------------------------
guest_net() {
    # Idempotent: if the v6 LAN address is already on br-lan (a
    # previous run configured it), the network restart is skipped — it
    # ties up the serial console and can transiently drop the daemon's
    # queue bind.
    if vm "ip -6 addr show dev br-lan | grep -q '$V6_LAN' && echo CFG_DONE" 20 \
        | grep -q CFG_DONE; then
        note "guest network already configured (v6 LAN addr present); skipping restart"
        # Ensure forwarding + routes anyway (idempotent).
        vm "ip addr add $WAN_IP/24 dev eth0 2>/dev/null || true; \
            ip -6 addr add $V6_WAN/64 dev eth0 2>/dev/null || true; \
            ip route add default via $SRV_IP4 dev eth0 2>/dev/null || true; \
            ip -6 route add default via $SRV_IP6 dev eth0 2>/dev/null || true; \
            sysctl -w net.ipv6.conf.all.forwarding=1 >/dev/null; \
            sysctl -w net.ipv6.conf.eth0.forwarding=1 >/dev/null; \
            sysctl -w net.ipv6.conf.br-lan.forwarding=1 >/dev/null" 40 >/dev/null
        return 0
    fi
    # Configure eth0 (WAN) static + eth1 (LAN) already bridged with the
    # image-prep address. Use UCI so fw4 sees consistent state.
    vm "uci set network.wan.device='eth0'; uci set network.wan.proto='static'; \
        uci set network.wan.ipaddr='$WAN_IP/24'; \
        uci set network.wan.ip6addr='$V6_WAN/64'; \
        uci set network.wan.gateway='$SRV_IP4'; \
        uci set network.wan.ip6gw='$SRV_IP6'; \
        uci set network.lan.ip6addr='$V6_LAN/64'; \
        uci commit network; /etc/init.d/network restart" 90 >/dev/null
    sleep 10
    # Explicit fallback (idempotent) + forwarding; verify in a separate
    # console round-trip so the restart output cannot swallow the marker.
    vm "ip addr add $WAN_IP/24 dev eth0 2>/dev/null || true; \
        ip -6 addr add $V6_WAN/64 dev eth0 2>/dev/null || true; \
        ip -6 addr add $V6_LAN/64 dev br-lan 2>/dev/null || true; \
        ip route add default via $SRV_IP4 dev eth0 2>/dev/null || true; \
        ip -6 route add default via $SRV_IP6 dev eth0 2>/dev/null || true; \
        sysctl -w net.ipv6.conf.all.forwarding=1 >/dev/null; \
        sysctl -w net.ipv6.conf.eth0.forwarding=1 >/dev/null; \
        sysctl -w net.ipv6.conf.br-lan.forwarding=1 >/dev/null" 40 >/dev/null
    vm "echo NETDONE" 20 | grep -q NETDONE || fail "guest network bootstrap failed"
    sleep 2
}

# ---------------------------------------------------------------------------
# Evidence snapshots (counters per queue).
# ---------------------------------------------------------------------------
snapshot() {
    local tag=$1
    vm "nft -a list chain inet goshisen forward > /tmp/v6_hook.txt; \
        cat /proc/net/netfilter/nfnetlink_queue > /tmp/v6_qs.txt; \
        wget -q -O- http://127.0.0.1:${LAB_METRICS_PORT:-9090}/metrics > /tmp/v6_met.txt; \
        echo MET_RC=\$? > /tmp/v6_metrc.txt; \
        echo WROTE" >/dev/null
    # Read phase: emit the tagged lines. Every grep is guarded (|| true)
    # so a missing table/metrics cannot abort the snapshot; the kernel
    # queue dump is emitted first and unconditionally.
    {
        vm "cat /tmp/v6_qs.txt; echo @@HOOK; \
            grep -aE 'queue (to|num)' /tmp/v6_hook.txt || true; \
            echo @@MET; \
            grep -aE '^goshisen_packets_total' /tmp/v6_met.txt || true; \
            grep -aE '^goshisen_packets_by_state' /tmp/v6_met.txt || true; \
            grep -aE '^goshisen_backend_errors' /tmp/v6_met.txt || true; \
            echo @@METRC; cat /tmp/v6_metrc.txt"
    } > "$WORK/$tag.raw"
    [ -s "$WORK/$tag.raw" ] || fail "snapshot $tag produced no output"
    # The verdict counters come from the metrics listener; `metrics_port:
    # 0` disables it. Fail with an ENV diagnosis instead of the misleading
    # "no daemon verdicts recorded" downstream.
    grep -aq 'MET_RC=0' "$WORK/$tag.raw" \
        || fail "ENV: metrics listener unreachable at 127.0.0.1:${LAB_METRICS_PORT:-9090} (metrics_port disabled?) — verdict counters unavailable"
}

snap_num() { # snap_num <tag> <queue> <field-name>
    local tag=$1 q=$2 key=$3
    case "$key" in
        id_sequence)     grep -aE "^[[:space:]]*$q[[:space:]]" "$WORK/$tag.raw" | tail -n1 | awk '{print $8}' ;;
        queue_total)     grep -aE "^[[:space:]]*$q[[:space:]]" "$WORK/$tag.raw" | tail -n1 | awk '{print $3}' ;;
        queue_dropped)   grep -aE "^[[:space:]]*$q[[:space:]]" "$WORK/$tag.raw" | tail -n1 | awk '{print $6}' ;;
        queue_user_dropped) grep -aE "^[[:space:]]*$q[[:space:]]" "$WORK/$tag.raw" | tail -n1 | awk '{print $7}' ;;
        peer_portid)     grep -aE "^[[:space:]]*$q[[:space:]]" "$WORK/$tag.raw" | tail -n1 | awk '{print $2}' ;;
        # Per-family hook rule counters (one forward rule per family; the
        # queue number distinguishes them in the nft dump).
        hook_packets4)   grep -a "queue to 100" "$WORK/$tag.raw" | grep -aoE 'packets [0-9]+' | grep -aoE '[0-9]+' | tail -n1 ;;
        hook_packets6)   grep -a "queue to 101" "$WORK/$tag.raw" | grep -aoE 'packets [0-9]+' | grep -aoE '[0-9]+' | tail -n1 ;;
        verdict_accept)  grep -aE "^goshisen_packets_total\{action=\"accept\",protocol=\"tcp\"\}" "$WORK/$tag.raw" | tail -n1 | awk '{print $NF}' ;;
        verdict_drop)    grep -aE "^goshisen_packets_total\{action=\"drop\",protocol=\"tcp\"\}" "$WORK/$tag.raw" | tail -n1 | awk '{print $NF}' ;;
        state_new)       grep -aE "^goshisen_packets_by_state_total\{state=\"new\"\}" "$WORK/$tag.raw" | tail -n1 | awk '{print $NF}' ;;
        state_estab)     grep -aE "^goshisen_packets_by_state_total\{state=\"established\"\}" "$WORK/$tag.raw" | tail -n1 | awk '{print $NF}' ;;
        backend_errors)  grep -aE "^goshisen_backend_errors_total" "$WORK/$tag.raw" | awk '{s+=$NF} END{print s+0}' ;;
        *) fail "unknown snapshot field $key" ;;
    esac
}

snap_or_zero() { local v; v=$(snap_num "$1" "$3" "$2" 2>/dev/null || true); echo "${v:-0}"; }
# snap_or_zero <tag> <key> <queue>  → snap_num <tag> <queue> <key>
snap_delta() { echo $(( $(snap_or_zero "$2" "$1" "$3") - $(snap_or_zero "$4" "$1" "$3") )); }
# snap_delta <field> <after> <queue> <before> → snap_or_zero <tag> <key> <queue>

# ---------------------------------------------------------------------------
# Policy application with reload confirmation (policy files fetched by wget
# from the WAN marker server into the guest).
# ---------------------------------------------------------------------------
apply_policy() { # apply_policy <policy-filename-in-www> <expected-rule-count>
    local file=$1 want=$2
    vm "wget -q -O /etc/goshisen/policy.yaml http://$SRV_IP4:$SRV_PORT/$file; \
        /etc/init.d/goshisen reload; sleep 3; echo RELOADED" 60 \
        | grep -q RELOADED || fail "policy reload failed for $file"
    local pid; pid=$(vm "ps | grep goshisen | grep config | grep -v grep" \
        | grep -a -oE '^[[:space:]]*[0-9]+' | tr -d ' ' | tail -n1)
    [ -n "$pid" ] || fail "no goshisen daemon process in VM"
    local before now
    before=$(vm "echo RELOAD_COUNT=\$(grep -Ec 'policy (reloaded|published)\" .*source=sighup .*rules=$want([^0-9]|\$)' /tmp/goshisen2.log)" \
        | grep -aE '^RELOAD_COUNT=' | tail -n1 | sed 's/^RELOAD_COUNT=//')
    before=${before:-0}
    vm "kill -HUP $pid" >/dev/null
    local deadline=$((SECONDS + 30))
    while [ $SECONDS -lt $deadline ]; do
        now=$(vm "echo RELOAD_COUNT=\$(grep -Ec 'policy (reloaded|published)\" .*source=sighup .*rules=$want([^0-9]|\$)' /tmp/goshisen2.log)" \
            | grep -aE '^RELOAD_COUNT=' | tail -n1 | sed 's/^RELOAD_COUNT=//')
        now=${now:-0}
        if [ "$now" -gt "$before" ]; then
            note "policy applied: rules=$want"
            return 0
        fi
        sleep 2
    done
    fail "policy did not reload to rules=$want within 30s (want $file)"
}

# ---------------------------------------------------------------------------
# Marker server in the WAN namespace (dual-stack).
# ---------------------------------------------------------------------------
start_server() {
    mkdir -p "$WORK/www"
    printf '%s\n' "$MARKER" > "$WORK/www/proof.txt"
    # Ensure the WAN ns can reach LAN clients (return route via guest's
    # WAN address); idempotent.
    client wan ip route add 10.0.3.0/24 via 10.0.2.15 2>/dev/null || true
    client wan ip -6 route add fd70:1::/64 via fd00::1 2>/dev/null || true
    cp "$ROOT/tests/openwrt_integration/policies/deny-lab-http.yaml" "$WORK/www/deny.yaml"
    cp "$ROOT/tests/openwrt_integration/policies/allow-lab-http.yaml" "$WORK/www/allow.yaml"
    # v6 policies: canonical bidirectional ESTABLISHED/RELATED.
    cat > "$WORK/www/allow-v6.yaml" <<'EOF'
version: 1
default_action: DROP
rules:
  - id: allow-lab-http4
    action: ACCEPT
    protocol: tcp
    src: 10.0.3.0/24
    dst: 10.0.2.2
    dst_port: 8089
    state: [NEW]
  - id: allow-v6-new
    action: ACCEPT
    protocol: tcp
    src: fd70:1::/64
    dst: fd00::2
    dst_port: 8089
    state: [NEW]
  - id: allow-established
    action: ACCEPT
    protocol: any
    state: [ESTABLISHED, RELATED]
EOF
    # One-directional for BOTH families: NEW allowed, no return rule.
    # The v4 case is the control: if the v6 failure were an IPv6 gap,
    # v4 would succeed here; it must NOT (proves policy semantics).
    cat > "$WORK/www/one-directional-v6.yaml" <<'EOF'
version: 1
default_action: DROP
rules:
  - id: allow-lab-http4
    action: ACCEPT
    protocol: tcp
    src: 10.0.3.0/24
    dst: 10.0.2.2
    dst_port: 8089
    state: [NEW]
  - id: allow-v6-new
    action: ACCEPT
    protocol: tcp
    src: fd70:1::/64
    dst: fd00::2
    dst_port: 8089
    state: [NEW]
EOF
    client wan python3 -m http.server "$SRV_PORT" --bind :: --directory "$WORK/www" \
        > "$WORK/server.log" 2>&1 &
    echo $! > "$WORK/server.pid"
    sleep 1.5
    kill -0 "$(cat "$WORK/server.pid")" 2>/dev/null || fail "marker server did not start"
    # Dual-stack reachability from the guest's output path (not the forward hook).
    vm "wget -q -O- http://$SRV_IP4:$SRV_PORT/proof.txt" | grep -q "$MARKER" \
        || fail "marker server not reachable from VM over IPv4"
    vm "wget -q -O- 'http://[$SRV_IP6]:$SRV_PORT/proof.txt'" | grep -q "$MARKER" \
        || fail "marker server not reachable from VM over IPv6"
    note "marker server reachable from VM (v4+v6)"
}

stop_server() {
    [ -f "$WORK/server.pid" ] && kill "$(cat "$WORK/server.pid")" 2>/dev/null || true
}

srv_hits4() { grep -c "GET /proof.txt" "$WORK/server.log" 2>/dev/null || true; }

# ---------------------------------------------------------------------------
# Curl helpers (v4/v6 from the LAN namespace).
# ---------------------------------------------------------------------------
curl4() {
    client lan curl -sS --http1.1 --connect-timeout 8 --max-time 15 \
        -w $'\nHTTP_CODE:%{http_code}' \
        "http://$SRV_IP4:$SRV_PORT/proof.txt" 2>&1
}
curl6() {
    client lan curl -sS -g -6 --http1.1 --connect-timeout 8 --max-time 15 \
        -w $'\nHTTP_CODE:%{http_code}' \
        "http://[$SRV_IP6]:$SRV_PORT/proof.txt" 2>&1
}

# ---------------------------------------------------------------------------
# Scenario helpers.
# ---------------------------------------------------------------------------
assert_blocked() { # assert_blocked <family> <curl-out> <rc> <snapshot> <before-snapshot> <queue>
    local fam=$1 out=$2 rc=$3 after=$4 before=$5 q=$6
    [ "$rc" -ne 0 ] || fail "$fam: curl exited 0 but must be blocked"
    printf '%s' "$out" | grep -q "$MARKER" && fail "$fam: marker body returned under blocking policy"
    printf '%s' "$out" | grep -q 'HTTP_CODE:200' && fail "$fam: HTTP 200 returned under blocking policy"
    local d hk
    d=$(snap_delta verdict_drop "$after" "$q" "$before")
    if [ "$fam" = v4 ]; then hk=$(snap_delta hook_packets4 "$after" "$q" "$before"); else hk=$(snap_delta hook_packets6 "$after" "$q" "$before"); fi
    [ "$d" -gt 0 ] || fail "$fam: no daemon DROP verdicts recorded"
    [ "$hk" -gt 0 ] || fail "$fam: hook counter did not advance (packet never queued)"
    note "$fam BLOCKED PASS: curl exit=$rc, drop verdicts +$d, hook +$hk"
}

assert_allowed() { # assert_allowed <family> <curl-out> <rc> <snapshot> <before-snapshot> <queue>
    local fam=$1 out=$2 rc=$3 after=$4 before=$5 q=$6
    [ "$rc" -eq 0 ] || fail "$fam: curl failed (exit $rc): $(printf '%s' "$out" | tail -n2)"
    printf '%s' "$out" | grep -q "$MARKER" || fail "$fam: marker body missing: $(printf '%s' "$out" | tail -n3)"
    printf '%s' "$out" | grep -q 'HTTP_CODE:200' || fail "$fam: HTTP status was not 200"
    local a hk
    a=$(snap_delta verdict_accept "$after" "$q" "$before")
    if [ "$fam" = v4 ]; then hk=$(snap_delta hook_packets4 "$after" "$q" "$before"); else hk=$(snap_delta hook_packets6 "$after" "$q" "$before"); fi
    [ "$a" -gt 0 ] || fail "$fam: no daemon ACCEPT verdicts recorded"
    [ "$hk" -gt 0 ] || fail "$fam: hook counter did not advance (packet never queued)"
    note "$fam ALLOWED PASS: curl exit=0, HTTP 200, accept verdicts +$a, hook +$hk"
}

# ---------------------------------------------------------------------------
# Scenario 1: v6 DENY blocks (v4 control DENY blocks identically).
# ---------------------------------------------------------------------------
scenario_deny() {
    note "── scenario 1/4: DENY blocks new flows (v4 control + v6) ──"
    apply_policy deny.yaml 1
    snapshot deny_before
    local hits_before; hits_before=$(srv_hits4)

    set +e
    local out4 rc4 out6 rc6
    out4=$(curl4); rc4=$?
    out6=$(curl6); rc6=$?
    set -e

    snapshot deny_after
    assert_blocked "v4" "$out4" "$rc4" deny_after deny_before "$QUEUE4"
    assert_blocked "v6" "$out6" "$rc6" deny_after deny_before "$QUEUE6"
    [ "$(srv_hits4)" -eq "$hits_before" ] \
        || fail "server received requests under DENY (v4 or v6)"
    note "server saw no request under DENY (hits flat at $hits_before)"
}

# ---------------------------------------------------------------------------
# Scenario 2: stateful ALLOW completes for v4+v6 (canonical bidirectional
# ESTABLISHED/RELATED).
# ---------------------------------------------------------------------------
scenario_allow() {
    note "── scenario 2/4: stateful ALLOW completes (v4 control + v6) ──"
    apply_policy allow-v6.yaml 3
    snapshot allow_before

    set +e
    local out4 rc4 out6 rc6
    out4=$(curl4); rc4=$?
    out6=$(curl6); rc6=$?
    set -e

    snapshot allow_after
    assert_allowed "v4" "$out4" "$rc4" allow_after allow_before "$QUEUE4"
    assert_allowed "v6" "$out6" "$rc6" allow_after allow_before "$QUEUE6"
    # Server must have logged genuine requests.
    grep -q "GET /proof.txt" "$WORK/server.log" || fail "server logged no request under ALLOW"
    note "server logged requests (v4+v6)"
}

# ---------------------------------------------------------------------------
# Scenario 3: one-directional policy (no return rule) is blocked for both
# v4 and v6 — proof that the phase-12 failure was policy semantics.
# ---------------------------------------------------------------------------
scenario_one_directional() {
    note "── scenario 3/4: one-directional policy blocked for v4 AND v6 ──"
    apply_policy one-directional-v6.yaml 2
    snapshot one_before

    set +e
    local out4 rc4 out6 rc6
    out4=$(curl4); rc4=$?
    out6=$(curl6); rc6=$?
    set -e

    snapshot one_after
    assert_blocked "v4" "$out4" "$rc4" one_after one_before "$QUEUE4"
    assert_blocked "v6" "$out6" "$rc6" one_after one_before "$QUEUE6"
    note "one-directional semantics confirmed (no return rule → blocked both families)"
}

# ---------------------------------------------------------------------------
# Scenario 4: ALLOW → DENY switch blocks new flows (v4+v6).
# ---------------------------------------------------------------------------
scenario_switch_back() {
    note "── scenario 4/4: ALLOW→DENY switch blocks new flows ──"
    apply_policy allow-v6.yaml 3
    # Establish one flow under ALLOW.
    set +e
    curl6 >/dev/null 2>&1; local rc_allow=$?
    set -e
    [ "$rc_allow" -eq 0 ] || fail "precondition: ALLOW did not complete before switch"

    apply_policy deny.yaml 1
    snapshot switch_before
    set +e
    local out4 rc4 out6 rc6
    out4=$(curl4); rc4=$?
    out6=$(curl6); rc6=$?
    set -e
    snapshot switch_after

    assert_blocked "v4" "$out4" "$rc4" switch_after switch_before "$QUEUE4"
    assert_blocked "v6" "$out6" "$rc6" switch_after switch_before "$QUEUE6"
    note "switch-back confirmed (new flows blocked under DENY)"
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
need bash python3 curl ip awk sed grep wget
[ -f "$CONSOLE" ] || denv "console helper missing: $CONSOLE"

ip netns list | grep -q "^$NS_LAN\b" || denv "v6 lab LAN namespace missing (run: scripts/openwrt-lab-v6.sh up)"
ip netns list | grep -q "^$NS_WAN\b" || denv "v6 lab WAN namespace missing (run: scripts/openwrt-lab-v6.sh up)"
timeout 5 bash -c "echo > /dev/tcp/127.0.0.1/$CONSOLE_PORT" 2>/dev/null \
    || denv "serial console 127.0.0.1:$CONSOLE_PORT not reachable (v6 lab up?)"
client lan true 2>/dev/null || denv "cannot execute in LAN namespace (root or passwordless sudo required)"
client wan true 2>/dev/null || denv "cannot execute in WAN namespace (root or passwordless sudo required)"

# Daemon ready: queues bound. Primary check is the /proc queue bind
# (authoritative — the daemon may have started before this run and its
# log line rotated/overwritten); fall back to the log line. If the
# rc.local autostart failed (slow/contended CI boot race), start the
# daemon explicitly (busybox has setsid; a plain `&` child dies on
# serial-session SIGHUP) and re-poll once before failing.
daemon_ready=0
deadline=$((SECONDS + 300))
while [ $SECONDS -lt $deadline ]; do
    # A bound queue has a non-zero peer portid on the expected line.
    if vm "awk '\$1==$QUEUE4{print \$2} \$1==$QUEUE6{print \$2}' /proc/net/netfilter/nfnetlink_queue" \
        | grep -a -E '^[1-9][0-9]*$' | wc -l | grep -qE '^2$'; then
        daemon_ready=1
        break
    fi
    if vm "echo READY_COUNT=\$(grep -c 'NFQUEUE bound' /tmp/goshisen2.log 2>/dev/null || echo 0)" \
        | grep -a -E '^READY_COUNT=' | tail -n1 | grep -qE '^READY_COUNT=[1-9]'; then
        daemon_ready=1
        break
    fi
    sleep 3
done
if [ "$daemon_ready" -ne 1 ]; then
    note "daemon not ready after 300s; starting it explicitly"
    vm "setsid /usr/sbin/goshisen --config /etc/goshisen/config.yaml >/tmp/goshisen2.log 2>&1 < /dev/null & sleep 5" 30 >/dev/null
    deadline2=$((SECONDS + 120))
    while [ $SECONDS -lt $deadline2 ]; do
        if vm "awk '\$1==$QUEUE4{print \$2} \$1==$QUEUE6{print \$2}' /proc/net/netfilter/nfnetlink_queue" \
            | grep -a -E '^[1-9][0-9]*$' | wc -l | grep -qE '^2$'; then
            daemon_ready=1
            break
        fi
        sleep 3
    done
fi
[ "$daemon_ready" -eq 1 ] || denv "daemon not ready (no NFQUEUE bound in /tmp/goshisen2.log)"

guest_net

# Queue bind check after the network restart. The daemon autostarts via
# rc.local and binds 100+101; the network restart may transiently drop
# the queue bind, so poll until both are non-zero. If the daemon truly
# died (rc.local race), start it exactly as rc.local does.
snapshot baseline
peer4=$(snap_or_zero baseline peer_portid "$QUEUE4")
peer6=$(snap_or_zero baseline peer_portid "$QUEUE6")
deadline=$((SECONDS + 120))
while { [ "$peer4" = "0" ] || [ "$peer6" = "0" ]; } && [ $SECONDS -lt $deadline ]; do
    sleep 5
    rm -f "$WORK/baseline.raw"
    snapshot baseline
    peer4=$(snap_or_zero baseline peer_portid "$QUEUE4")
    peer6=$(snap_or_zero baseline peer_portid "$QUEUE6")
done
if [ "$peer4" = "0" ] || [ "$peer6" = "0" ]; then
    note "queue 100/101 not bound after 120s; (re)starting daemon"
    # busybox: setsid survives the serial session (a plain `&` child is
    # killed on session SIGHUP; nohup is absent).
    vm "setsid /usr/sbin/goshisen --config /etc/goshisen/config.yaml >/tmp/goshisen2.log 2>&1 < /dev/null & sleep 5" 30 >/dev/null
    sleep 10
    rm -f "$WORK/baseline.raw"
    snapshot baseline
    peer4=$(snap_or_zero baseline peer_portid "$QUEUE4")
    peer6=$(snap_or_zero baseline peer_portid "$QUEUE6")
fi
[ "$peer4" != "0" ] || denv "queue 100 not bound (daemon not running)"
[ "$peer6" != "0" ] || denv "queue 101 not bound (daemon not running)"
note "queues bound (100 peer=$peer4, 101 peer=$peer6)"

start_server
trap 'stop_server; rm -rf "$WORK"' EXIT

scenario_deny
scenario_allow
scenario_one_directional
scenario_switch_back

# Record the guest kernel for traceability.
kern=$(vm "uname -r" | grep -a -E '[0-9]+\.[0-9]+\.[0-9]+' | tail -n1)
note "guest kernel: $kern"
[ -n "$kern" ] || fail "could not record guest kernel version"

apply_policy deny.yaml 1
stop_server
trap 'rm -rf "$WORK"' EXIT

note "PASS: v6 stateful scenarios 1-4 completed with v4 controls and counter evidence."
exit 0
