#!/usr/bin/env bash
# GoShiSen-Enforce OpenWrt QEMU lab end-to-end enforcement test.
#
# Proves, on one network path, that:
#   - an explicit DROP policy blocks an HTTP request (client timeout,
#     no server-side request, daemon drop verdicts),
#   - an explicit ACCEPT policy permits the identical request (HTTP 200
#     with the marker body, server-side request, daemon accept verdicts
#     across NEW and ESTABLISHED states), and
#   - the nftables hook counter, the kernel queue sequence, and the
#     daemon's verdict metrics advance in lockstep, while the kernel's
#     queue_dropped / queue_user_dropped counters stay flat.
#
# It never treats a netcat exit code or a growing queue sequence as
# proof of enforcement.
#
# Prerequisites:
#   - lab up: scripts/openwrt-lab.sh up
#     (client namespace gsh_owrt_lan, serial console 127.0.0.1:5555)
#   - root (or sudo -n) for curl inside the client namespace
#   - python3, curl, wget, ip
#
# Exit codes:
#   0  PASS       all controls and evidence assertions
#   1  FAIL       behavior or evidence mismatch
#   3  ENV        prerequisites missing (lab down, tools absent)
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
CONSOLE="$ROOT/scripts/openwrt-console.py"
POLICY_DIR="$ROOT/tests/openwrt_integration/policies"
NS=${OPENWRT_LAB_NS:-gsh_owrt_lan}
CONSOLE_PORT=${OPENWRT_CONSOLE_PORT:-5555}
SRV_PORT=${OPENWRT_HTTP_PORT:-8088}
SRV_HOST=127.0.0.1
# QEMU user-mode networking aliases the host loopback at the gateway.
SRV_IP=10.0.2.2
MARKER=GSH-ALLOW-PROOF
QUEUE=100
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

note() { echo "[e2e] $*"; }
fail() { echo "[e2e] FAIL: $*" >&2; exit 1; }
denv() { echo "[e2e] ENV: $*" >&2; exit 3; }

need() {
    local t
    for t in "$@"; do
        command -v "$t" >/dev/null 2>&1 || denv "required tool $t missing"
    done
}

# vm runs one command in the VM over the serial console. The transcript
# includes echoed input and serial cursor control bytes; strip both so
# downstream greps never treat the capture as binary.
vm() {
    python3 "$CONSOLE" --port "$CONSOLE_PORT" --cmd "$1" --timeout 25 2>&1 \
        | tr -cd '\11\12\15\40-\176\200-\377' | sed 's/\r//g'
}

# client runs a command in the LAN client namespace (root or sudo -n).
client() {
    if [ "$(id -u)" = 0 ]; then ip netns exec "$NS" "$@"
    else sudo -n ip netns exec "$NS" "$@"; fi
}

# ---------------------------------------------------------------------------
# Evidence snapshots
#
# One console round trip per snapshot: the VM writes every counter to a
# file, then a second call cats it (large outputs truncate otherwise).
# Fields of /proc/net/netfilter/nfnetlink_queue (per the kernel's
# nfqnl_seq_show):
#   1 queue_num 2 peer_portid 3 queue_total 4 copy_mode 5 copy_range
#   6 queue_dropped 7 queue_user_dropped 8 id_sequence 9 (always 1)
# ---------------------------------------------------------------------------
snapshot() {
    local tag=$1
    # Write phase: capture all counters to files in one console call.
    vm "nft list chain ip goshisen forward | grep 'queue to' > /tmp/e2e_hook.txt; \
        cat /proc/net/netfilter/nfnetlink_queue > /tmp/e2e_qs.txt; \
        wget -q -O- http://127.0.0.1:9090/metrics > /tmp/e2e_met.txt; \
        echo WROTE" >/dev/null
    # Read phase: emit only the small, tagged counter lines. The full
    # metrics blob (>1.5 KiB) truncates on the serial console under
    # TCG, so we never cat it wholesale.
    {
        vm "echo @@SNAP; grep -a 'queue to' /tmp/e2e_hook.txt; cat /tmp/e2e_qs.txt; \
            echo @@MET; grep -aE '^goshisen_packets_total' /tmp/e2e_met.txt; \
            grep -aE '^goshisen_packets_by_state' /tmp/e2e_met.txt; \
            grep -aE '^goshisen_backend_errors' /tmp/e2e_met.txt"
    } > "$WORK/$tag.raw"
    [ -s "$WORK/$tag.raw" ] || fail "snapshot $tag produced no output"
}

# snap_num extracts a value from a snapshot by grepping the raw transcript.
# The console transcript can contain cursor-control bytes; grep -a forces
# text mode so the file is not treated as binary.
snap_num() { # snap_num <tag> <field-name>
    local tag=$1 key=$2
    case "$key" in
        id_sequence)     grep -aE "^[[:space:]]*$QUEUE[[:space:]]" "$WORK/$tag.raw" | tail -n1 | awk '{print $8}' ;;
        queue_total)     grep -aE "^[[:space:]]*$QUEUE[[:space:]]" "$WORK/$tag.raw" | tail -n1 | awk '{print $3}' ;;
        queue_dropped)   grep -aE "^[[:space:]]*$QUEUE[[:space:]]" "$WORK/$tag.raw" | tail -n1 | awk '{print $6}' ;;
        queue_user_dropped) grep -aE "^[[:space:]]*$QUEUE[[:space:]]" "$WORK/$tag.raw" | tail -n1 | awk '{print $7}' ;;
        peer_portid)     grep -aE "^[[:space:]]*$QUEUE[[:space:]]" "$WORK/$tag.raw" | tail -n1 | awk '{print $2}' ;;
        hook_packets)    grep -a 'queue to' "$WORK/$tag.raw" | grep -aoE 'packets [0-9]+' | grep -aoE '[0-9]+' | tail -n1 ;;
        verdict_accept)  grep -aE "^goshisen_packets_total\{action=\"accept\",protocol=\"tcp\"\}" "$WORK/$tag.raw" | tail -n1 | awk '{print $NF}' ;;
        verdict_drop)    grep -aE "^goshisen_packets_total\{action=\"drop\",protocol=\"tcp\"\}" "$WORK/$tag.raw" | tail -n1 | awk '{print $NF}' ;;
        state_new)       grep -aE "^goshisen_packets_by_state_total\{state=\"new\"\}" "$WORK/$tag.raw" | tail -n1 | awk '{print $NF}' ;;
        state_estab)     grep -aE "^goshisen_packets_by_state_total\{state=\"established\"\}" "$WORK/$tag.raw" | tail -n1 | awk '{print $NF}' ;;
        backend_errors)  grep -aE "^goshisen_backend_errors_total" "$WORK/$tag.raw" | awk '{s+=$NF} END{print s+0}' ;;
        *) fail "unknown snapshot field $key" ;;
    esac
}

snap_or_zero() { local v; v=$(snap_num "$1" "$2" 2>/dev/null || true); echo "${v:-0}"; }
snap_delta() { echo $(( $(snap_or_zero "$2" "$1") - $(snap_or_zero "$3" "$1") )); }

# ---------------------------------------------------------------------------
# Policy application with reload confirmation
# ---------------------------------------------------------------------------
daemon_pid() {
    vm "ps | grep goshisen | grep config | grep -v grep" \
        | grep -a -oE '^[[:space:]]*[0-9]+' | tr -d ' ' | tail -n1
}

apply_policy() { # apply_policy <host-policy-file> <expected-rule-count>
    local file=$1 want=$2 line args=()
    [ -f "$file" ] || fail "policy file missing: $file"
    while IFS= read -r line; do
        case "$line" in ''|'#'*) continue ;; esac
        case "$line" in *"'"*) fail "policy line contains a single quote: $line" ;; esac
        args+=("'$line'")
    done < "$file"
    vm "printf '%s\n' ${args[*]} > /etc/goshisen/policy.yaml; wc -l < /etc/goshisen/policy.yaml" >/dev/null

    local pid; pid=$(daemon_pid)
    [ -n "$pid" ] || fail "no goshisen daemon process in VM"
    # Count matching reload lines before the signal, then wait for the
    # count to increase (tail -1 races a concurrent log write). The VM
    # output is tagged so the echoed command line cannot be confused
    # with the result; the result line is the last RELOAD_COUNT= line.
    local before
    before=$(vm "echo RELOAD_COUNT=\$(grep -c 'policy reloaded\" rules=$want' /tmp/goshisen2.log)" \
        | grep -aE '^RELOAD_COUNT=' | tail -n1 | sed 's/^RELOAD_COUNT=//')
    before=${before:-0}
    vm "kill -HUP $pid" >/dev/null
    local deadline=$((SECONDS + 30))
    while [ $SECONDS -lt $deadline ]; do
        local now
        now=$(vm "echo RELOAD_COUNT=\$(grep -c 'policy reloaded\" rules=$want' /tmp/goshisen2.log)" \
            | grep -aE '^RELOAD_COUNT=' | tail -n1 | sed 's/^RELOAD_COUNT=//')
        now=${now:-0}
        if [ "$now" -gt "$before" ]; then
            note "policy applied: rules=$want"
            return 0
        fi
        sleep 2
    done
    fail "policy did not reload to rules=$want within 30s (want $file); daemon log: $(vm 'tail -3 /tmp/goshisen2.log')"
}

# ---------------------------------------------------------------------------
# Host marker server
# ---------------------------------------------------------------------------
start_server() {
    # Clean any leftover server from a previous unfinished run.
    if [ "$(id -u)" = 0 ]; then pkill -f "http.server $SRV_PORT" 2>/dev/null || true
    else sudo -n pkill -f "http.server $SRV_PORT" 2>/dev/null || true; fi
    sleep 0.5
    printf '%s\n' "$MARKER" > "$WORK/proof.txt"
    ( cd "$WORK" && nohup python3 -m http.server "$SRV_PORT" --bind "$SRV_HOST" \
        --directory "$WORK" > "$WORK/server.log" 2>&1 & echo $! > "$WORK/server.pid" )
    sleep 1
    kill -0 "$(cat "$WORK/server.pid")" 2>/dev/null || fail "marker server did not start"
    # Availability from the VM's output path (not the forward hook).
    vm "wget -q -O- http://$SRV_IP:$SRV_PORT/proof.txt" | grep -q "$MARKER" \
        || fail "marker server not reachable from the VM (checked $SRV_IP:$SRV_PORT)"
    note "marker server reachable from VM output path ($SRV_IP:$SRV_PORT)"
}

srv_hits() {
    local n
    n=$(grep -c "GET /proof.txt" "$WORK/server.log" 2>/dev/null || true)
    echo "${n:-0}"
}

# ---------------------------------------------------------------------------
# Control 1: explicit DROP
# ---------------------------------------------------------------------------
run_drop_control() {
    note "── control 1/2: explicit DROP ──"
    apply_policy "$POLICY_DIR/deny-lab-http.yaml" 1
    snapshot drop_before
    local hits_before; hits_before=$(srv_hits)

    set +e
    local out
    out=$(client curl -sS --http1.1 --connect-timeout 10 --max-time 20 \
        -w $'\nHTTP_CODE:%{http_code}' \
        "http://$SRV_IP:$SRV_PORT/proof.txt" 2>&1)
    local rc=$?
    set -e

    [ "$rc" -ne 0 ] || fail "DROP control: curl exited 0 but the request must be blocked"
    printf '%s' "$out" | grep -q "$MARKER" && fail "DROP control: marker body returned under a DROP policy"
    printf '%s' "$out" | grep -q 'HTTP_CODE:200' && fail "DROP control: HTTP 200 returned under a DROP policy"

    snapshot drop_after
    local hook qd qud d sum
    hook=$(snap_delta hook_packets drop_after drop_before)
    qd=$(snap_delta queue_dropped drop_after drop_before)
    qud=$(snap_delta queue_user_dropped drop_after drop_before)
    d=$(snap_delta verdict_drop drop_after drop_before)
    sum=$(snap_delta verdict_accept drop_after drop_before)

    [ "$qd" -eq 0 ]  || fail "DROP control: queue_dropped grew by $qd (kernel drop, not a policy verdict)"
    [ "$qud" -eq 0 ] || fail "DROP control: queue_user_dropped grew by $qud"
    [ "$hook" -gt 0 ] || fail "DROP control: hook counter did not advance (packet never queued)"
    [ "$d" -gt 0 ]    || fail "DROP control: no daemon DROP verdicts recorded"
    [ "$((d + sum))" -eq "$hook" ] \
        || fail "DROP control: identity mismatch (hook +$hook, verdicts +$((d + sum)))"
    [ "$(srv_hits)" -eq "$hits_before" ] \
        || fail "DROP control: the server received the request despite the DROP policy"

    note "DROP control PASS: curl exit=$rc, daemon drop verdicts +$d, hook +$hook, queue drops +0"
}

# ---------------------------------------------------------------------------
# Control 2: explicit ACCEPT (same path, same queue, same hook)
# ---------------------------------------------------------------------------
run_accept_control() {
    note "── control 2/2: explicit ACCEPT ──"
    apply_policy "$POLICY_DIR/allow-lab-http.yaml" 2
    snapshot accept_before

    set +e
    local out
    out=$(client curl -sS --http1.1 --connect-timeout 10 --max-time 20 \
        -w $'\nHTTP_CODE:%{http_code}' \
        "http://$SRV_IP:$SRV_PORT/proof.txt" 2>&1)
    local rc=$?
    set -e

    [ "$rc" -eq 0 ] || fail "ACCEPT control: curl failed (exit $rc): $(printf '%s' "$out" | tail -n2)"
    printf '%s' "$out" | grep -q "$MARKER" || fail "ACCEPT control: marker body missing: $(printf '%s' "$out" | tail -n3)"
    printf '%s' "$out" | grep -q 'HTTP_CODE:200' || fail "ACCEPT control: HTTP status was not 200"

    snapshot accept_after
    local hook a d nw es qd qud
    hook=$(snap_delta hook_packets accept_after accept_before)
    a=$(snap_delta verdict_accept accept_after accept_before)
    d=$(snap_delta verdict_drop accept_after accept_before)
    nw=$(snap_delta state_new accept_after accept_before)
    es=$(snap_delta state_estab accept_after accept_before)
    qd=$(snap_delta queue_dropped accept_after accept_before)
    qud=$(snap_delta queue_user_dropped accept_after accept_before)

    [ "$qd" -eq 0 ]  || fail "ACCEPT control: queue_dropped grew by $qd"
    [ "$qud" -eq 0 ] || fail "ACCEPT control: queue_user_dropped grew by $qud"
    [ "$hook" -gt 0 ] || fail "ACCEPT control: hook counter did not advance"
    [ "$a" -gt 0 ]    || fail "ACCEPT control: no daemon ACCEPT verdicts recorded"
    [ "$es" -gt 0 ]   || fail "ACCEPT control: no ESTABLISHED verdicts (conntrack state absent?)"
    [ "$((a + d))" -eq "$hook" ] \
        || fail "ACCEPT control: identity mismatch (hook +$hook, verdicts +$((a + d)))"
    grep -q "GET /proof.txt" "$WORK/server.log" \
        || fail "ACCEPT control: server logged no request"

    note "ACCEPT control PASS: curl exit=0, HTTP 200, marker body, accept verdicts +$a (new +$nw, established +$es), hook +$hook"
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
need bash python3 curl ip awk sed grep
[ -f "$CONSOLE" ] || denv "console helper missing: $CONSOLE"

ip netns list | grep -q "^$NS\b" || denv "lab namespace $NS missing (run: scripts/openwrt-lab.sh up)"
timeout 5 bash -c "echo > /dev/tcp/127.0.0.1/$CONSOLE_PORT" 2>/dev/null \
    || denv "serial console 127.0.0.1:$CONSOLE_PORT not reachable (lab up?)"
client true 2>/dev/null || denv "cannot execute in namespace $NS (root or passwordless sudo required)"

# The daemon must be running with the queue bound before snapshots mean
# anything. A cold TCG boot autostarts the daemon via rc.local; under
# QEMU TCG this can take 90-150s, so wait up to 180s for the NFQUEUE
# bind log line (the CI boot step also waits on the console, not on the
# daemon).
daemon_ready=0
deadline=$((SECONDS + 180))
while [ $SECONDS -lt $deadline ]; do
    if vm "echo READY_COUNT=\$(grep -c 'NFQUEUE bound' /tmp/goshisen2.log 2>/dev/null || echo 0)" \
        | grep -a -E '^READY_COUNT=' | tail -n1 | grep -qE '^READY_COUNT=[1-9]'; then
        daemon_ready=1
        break
    fi
    sleep 3
done
[ "$daemon_ready" -eq 1 ] || denv "daemon not ready (no NFQUEUE bound in /tmp/goshisen2.log)"

snapshot baseline
peer=$(snap_or_zero baseline peer_portid)
[ "$peer" != "0" ] || denv "queue $QUEUE is not bound in the VM (is the daemon running?)"
note "queue $QUEUE bound (peer_portid=$peer, id_sequence=$(snap_or_zero baseline id_sequence))"

start_server
trap 'stop_server; rm -rf "$WORK"' EXIT
stop_server() { [ -f "$WORK/server.pid" ] && kill "$(cat "$WORK/server.pid")" 2>/dev/null || true; }

run_drop_control
run_accept_control

# Restore the packaged fail-closed policy and leave no test server behind.
apply_policy "$ROOT/openwrt/files/etc/goshisen/policy.yaml" 0
stop_server
trap 'rm -rf "$WORK"' EXIT

note "PASS: DROP blocks the request, ACCEPT permits it, all counters agree; kernel queue drops stayed flat."
exit 0
