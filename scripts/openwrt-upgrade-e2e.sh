#!/usr/bin/env bash
# GoShiSen-Enforce — OpenWrt apk upgrade e2e (issue #57).
#
# Proves, on a fresh OpenWrt 25.12.6 x86/64 VM, that upgrading the
# goshisen apk switches the RUNNING daemon to the new binary with NO
# manual restart, and that no unprotected/unqueued window ever opens:
#
#   1. install the PUBLIC signed old apk (--old-apk), enable, start;
#   2. baseline: deny policy blocks (probe exit != 0), allow policy
#      passes (HTTP 200 + marker) — both directions verified;
#   3. restore deny, start continuous deny probes, `apk add --no-network
#      [--allow-untrusted]` the new apk (--new-apk);
#   4. assert every deny probe stayed blocked (0 passes), queues 100/101
#      remain bound, and the running daemon is the NEW binary:
#      pid changed, `/proc/<pid>/exe` sha256 == on-disk
#      `/usr/sbin/goshisen` sha256 == new apk payload sha256, and
#      `/usr/sbin/goshisen --version` reports the new version;
#   5. positive control on the NEW binary: allow policy + SIGHUP →
#      probes pass (HTTP 200).
#
# The new apk (unsigned, pre-release test build) is installed with
# `--allow-untrusted` ONLY when --allow-untrusted is passed; the old
# signed apk is never installed with it. This is a pre-release test of
# an unsigned build — never a production install path.
#
# Prerequisites:
#   - lab up: scripts/openwrt-lab.sh up (client ns gsh_owrt_lan,
#     serial console 127.0.0.1:5555), image provisioned via
#     openwrt-image-prep.sh --no-daemon (kmods + network baked, no
#     daemon autostart — the daemon lifecycle here is procd/apk),
#   - root (or sudo -n) for the client-namespace curl,
#   - python3, curl, wget, ip on the host,
#   - the anchor file (--anchor), the old apk (--old-apk), the new
#     apk (--new-apk) reachable from the host.
#
# Exit codes:
#   0  PASS
#   1  FAIL
#   3  ENV
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
CONSOLE="$ROOT/scripts/openwrt-console.py"
NS=${OPENWRT_LAB_NS:-gsh_owrt_lan}
CONSOLE_PORT=${OPENWRT_CONSOLE_PORT:-5555}
SRV_PORT=${OPENWRT_HTTP_PORT:-8088}
SRV_HOST=127.0.0.1
SRV_IP=10.0.2.2
MARKER=GSH-ALLOW-PROOF
QUEUE=100
QUEUE6=101
SERVE_PORT=8099
OLD_APK=""; NEW_APK=""; ANCHOR=""; ALLOW_UNTRUSTED=0
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

note() { echo "[upgrade-e2e] $*"; }
fail() { echo "[upgrade-e2e] FAIL: $*" >&2; exit 1; }
denv() { echo "[upgrade-e2e] ENV: $*" >&2; exit 3; }

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

client() {
    if [ "$(id -u)" = 0 ]; then ip netns exec "$NS" "$@"
    else sudo -n ip netns exec "$NS" "$@"; fi
}

while [ $# -gt 0 ]; do
    case "$1" in
        --old-apk) OLD_APK=$2; shift 2 ;;
        --new-apk) NEW_APK=$2; shift 2 ;;
        --anchor) ANCHOR=$2; shift 2 ;;
        --allow-untrusted) ALLOW_UNTRUSTED=1; shift ;;
        *) echo "usage: openwrt-upgrade-e2e.sh --old-apk FILE --new-apk FILE --anchor FILE [--allow-untrusted]" >&2; exit 2 ;;
    esac
done

[ -n "$OLD_APK" ] || denv "--old-apk is required (public signed apk)"
[ -f "$OLD_APK" ] || denv "old apk not found: $OLD_APK"
[ -n "$NEW_APK" ] || denv "--new-apk is required (fixed build apk)"
[ -f "$NEW_APK" ] || denv "new apk not found: $NEW_APK"
[ -n "$ANCHOR" ] || denv "--anchor is required (public anchor)"
[ -f "$ANCHOR" ] || denv "anchor not found: $ANCHOR"

OLD_VER=$(basename "$OLD_APK" | sed -nE 's/^goshisen-([^/]+)\.apk$/\1/p')
NEW_VER=$(basename "$NEW_APK" | sed -nE 's/^goshisen-([^/]+)\.apk$/\1/p')
[ -n "$OLD_VER" ] || fail "cannot parse version from old apk filename: $OLD_APK"
[ -n "$NEW_VER" ] || fail "cannot parse version from new apk filename: $NEW_APK"
OLD_APK_SHA=$(sha256sum "$OLD_APK" | awk '{print $1}')
NEW_APK_SHA=$(sha256sum "$NEW_APK" | awk '{print $1}')
ANCHOR_SHA=$(sha256sum "$ANCHOR" | awk '{print $1}')
note "old apk goshisen-$OLD_VER sha256 $OLD_APK_SHA"
note "new apk goshisen-$NEW_VER sha256 $NEW_APK_SHA"
note "anchor sha256 $ANCHOR_SHA"

need bash python3 curl ip wget awk sed grep
[ -f "$CONSOLE" ] || denv "console helper missing: $CONSOLE"
ip netns list | grep -q "^$NS\b" || denv "lab namespace $NS missing (run: scripts/openwrt-lab.sh up)"
timeout 5 bash -c "echo > /dev/tcp/127.0.0.1/$CONSOLE_PORT" 2>/dev/null \
    || denv "serial console 127.0.0.1:$CONSOLE_PORT not reachable (lab up?)"
client true 2>/dev/null || denv "cannot execute in namespace $NS (root or passwordless sudo required)"

# ---------------------------------------------------------------------------
# Serve the apk/anchor/policies to the VM over the WAN slirp path
# (10.0.2.2 == host loopback).
# ---------------------------------------------------------------------------
mkdir -p "$WORK/www"
cp "$OLD_APK" "$WORK/www/old.apk"
cp "$NEW_APK" "$WORK/www/new.apk"
cp "$ANCHOR" "$WORK/www/anchor.pem"
cp "$ROOT/tests/openwrt_integration/policies/allow-lab-http.yaml" "$WORK/www/allow.yaml"
cp "$ROOT/tests/openwrt_integration/policies/deny-lab-http.yaml" "$WORK/www/deny.yaml"
printf '%s\n' "$MARKER" > "$WORK/www/proof.txt"
# Serve the apk/anchor/policies to the VM over the WAN slirp path
# (10.0.2.2 == host loopback), exactly like the proof server on
# $SRV_PORT — no netns needed for the VM to fetch files.
if [ "$(id -u)" = 0 ]; then pkill -f "http.server $SERVE_PORT" 2>/dev/null || true
else sudo -n pkill -f "http.server $SERVE_PORT" 2>/dev/null || true; fi
sleep 0.5
( cd "$WORK/www" && nohup python3 -m http.server "$SERVE_PORT" --bind 127.0.0.1 \
    --directory "$WORK/www" > "$WORK/serve.log" 2>&1 & echo $! > "$WORK/serve.pid" )
sleep 1
kill -0 "$(cat "$WORK/serve.pid")" 2>/dev/null || fail "file server did not start"
# VM must fetch files from the host before we rely on them.
vm "mkdir -p /etc/apk/keys; \
    wget -q -O /tmp/old.apk http://10.0.2.2:$SERVE_PORT/old.apk; \
    wget -q -O /tmp/new.apk http://10.0.2.2:$SERVE_PORT/new.apk; \
    wget -q -O /tmp/anchor.pem http://10.0.2.2:$SERVE_PORT/anchor.pem; \
    wget -q -O /tmp/allow.yaml http://10.0.2.2:$SERVE_PORT/allow.yaml; \
    wget -q -O /tmp/deny.yaml http://10.0.2.2:$SERVE_PORT/deny.yaml; \
    wc -c /tmp/old.apk /tmp/new.apk /tmp/anchor.pem" \
    | grep -aE '/tmp/(old|new)\.apk' >/dev/null || fail "VM cannot fetch files from host"

stop_serve() { [ -f "$WORK/serve.pid" ] && kill "$(cat "$WORK/serve.pid")" 2>/dev/null || true; }
stop_probes() {
    for f in "$WORK"/*.pid; do
        [ -e "$f" ] || continue
        kill "$(cat "$f")" 2>/dev/null || true
    done
}
# On any exit (incl. FAIL) report the probe stats when a probe loop ran:
# the fail-open question (did a deny probe pass during the upgrade) is
# answered even when a later assertion fails.
report_probes() {
    if [ -f "$WORK/upgrade.probes" ]; then
        local t p b
        t=$(wc -l < "$WORK/upgrade.probes" 2>/dev/null || echo 0)
        p=$(awk -F'\t' '$3=="PASS"{n++} END{print n+0}' "$WORK/upgrade.probes" 2>/dev/null || echo 0)
        b=$(awk -F'\t' '$3=="BLOCKED"{n++} END{print n+0}' "$WORK/upgrade.probes" 2>/dev/null || echo 0)
        echo "[upgrade-e2e] upgrade probe stats (recorded even on FAIL): $t total, $p passed, $b blocked" >&2
    fi
}
trap 'report_probes; stop_serve; stop_probes; stop_server; rm -rf "$WORK"' EXIT

# ---------------------------------------------------------------------------
# VM state helpers
# ---------------------------------------------------------------------------
queue_peer() { # queue_peer <queue> — peer_portid or 0
    vm "awk '\$1==$1{print \$2}' /proc/net/netfilter/nfnetlink_queue" \
        | grep -aE '^[0-9]+$' | tail -n1
}

daemon_pid() {
    vm "ps w | grep goshisen | grep -v grep" \
        | grep -a -oE '^[[:space:]]*[0-9]+' | tr -d ' ' | tail -n1
}

exe_sha() { # exe_sha <pid>
    vm "sha256sum /proc/$1/exe 2>/dev/null || echo MISSING" \
        | grep -aE '^[0-9a-f]{64}' | tail -n1 | cut -d' ' -f1
}

exe_link() { # exe_link <pid> — /proc/<pid>/exe target
    vm "readlink /proc/$1/exe" | grep -a '/usr/sbin/goshisen' | tail -n1
}

# apply_policy <host-policy-file> <expected-rule-count> — same reload
# gate as the main e2e: write policy, SIGHUP, wait for the daemon log
# (logd: the daemon runs under procd here, stdout goes to syslog, not
# /tmp/goshisen2.log) to show the reload with the right rule count
# BEFORE generating traffic.
apply_policy() {
    local file=$1 want=$2 line args=() pid before now deadline
    [ -f "$file" ] || fail "policy file missing: $file"
    while IFS= read -r line; do
        case "$line" in ''|'#'*) continue ;; esac
        case "$line" in *"'"*) fail "policy line contains a single quote: $line" ;; esac
        args+=("'$line'")
    done < "$file"
    vm "printf '%s\n' ${args[*]} > /etc/goshisen/policy.yaml; wc -l < /etc/goshisen/policy.yaml" >/dev/null
    pid=$(daemon_pid)
    [ -n "$pid" ] || fail "no goshisen daemon process in VM"
    before=$(vm "echo RELOAD_COUNT=\$(logread | grep -Ec 'policy (reloaded|published)\" .*source=sighup .*rules=$want([^0-9]|\$)')" \
        | grep -aE '^RELOAD_COUNT=' | tail -n1 | sed 's/^RELOAD_COUNT=//')
    before=${before:-0}
    vm "kill -HUP $pid" >/dev/null
    deadline=$((SECONDS + 30))
    while [ $SECONDS -lt $deadline ]; do
        now=$(vm "echo RELOAD_COUNT=\$(logread | grep -Ec 'policy (reloaded|published)\" .*source=sighup .*rules=$want([^0-9]|\$)')" \
            | grep -aE '^RELOAD_COUNT=' | tail -n1 | sed 's/^RELOAD_COUNT=//')
        now=${now:-0}
        [ "$now" -gt "$before" ] && { note "policy applied: rules=$want"; return 0; }
        sleep 2
    done
    fail "policy did not reload to rules=$want within 30s; daemon log: $(vm 'logread | grep goshisen | tail -5')"
}

# probe_deny runs one curl from the LAN client; PASS only when the
# request completes with the marker body, BLOCKED otherwise.
probe_deny() {
    set +e
    local out
    out=$(client curl -sS --http1.1 --connect-timeout 2 --max-time 5 \
        -w $'\nHTTP_CODE:%{http_code}' \
        "http://$SRV_IP:$SRV_PORT/proof.txt" 2>&1)
    local rc=$?
    set -e
    if [ "$rc" -eq 0 ] && printf '%s' "$out" | grep -q "$MARKER"; then
        echo PASS
    else
        echo BLOCKED
    fi
}

# run_probes <tag> <count> <interval-s> — background deny probe loop
# writing tab-separated <i> <ts> <result> lines to $WORK/$tag.probes.
run_probes() {
    local tag=$1 count=$2 interval=$3 i=0
    : > "$WORK/$tag.probes"
    (
        while [ "$i" -lt "$count" ]; do
            i=$((i + 1))
            ts=$(date +%s.%N)
            r=$(probe_deny)
            printf '%s\t%s\t%s\n' "$i" "$ts" "$r" >> "$WORK/$tag.probes"
            sleep "$interval"
        done
    ) &
    echo $! > "$WORK/$tag.pid"
}

count_probe_results() { # count_probe_results <tag> <result>
    awk -F'\t' -v r="$2" '$3==r{n++} END{print n+0}' "$WORK/$1.probes" 2>/dev/null || echo 0
}

# ---------------------------------------------------------------------------
# 1. Anchor + old apk install (public signed; never --allow-untrusted)
# ---------------------------------------------------------------------------
note "setup: anchor + old apk (goshisen-$OLD_VER)"
vm "cp /tmp/anchor.pem /etc/apk/keys/goshisen-apk.pub.pem; ls -l /etc/apk/keys/goshisen-apk.pub.pem" \
    | grep -q 'goshisen-apk.pub.pem' || fail "anchor install failed"
vm "apk verify /tmp/old.apk" | grep -a -q 'OK' || fail "old apk signature verify failed (anchor?)"
vm "apk add --no-network /tmp/old.apk > /tmp/install-old.txt 2>&1; echo APKRC=\$?" \
    | grep -a -q 'APKRC=0' || fail "old apk install failed"
vm "apk list --installed | grep goshisen" | grep -a -q "$OLD_VER" \
    || fail "installed version is not $OLD_VER"

# ---------------------------------------------------------------------------
# 2. Enable + start (procd), wait for queues 100/101 bound
# ---------------------------------------------------------------------------
note "setup: enable + start daemon (procd)"
vm "uci set goshisen.main.enabled=1; uci commit goshisen; /etc/init.d/goshisen enable; /etc/init.d/goshisen start; echo STARTED" | grep -q STARTED \
    || fail "daemon start command failed"
p100=0; p101=0
deadline=$((SECONDS + 300))
while [ $SECONDS -lt $deadline ]; do
    p100=$(queue_peer 100)
    p101=$(queue_peer 101)
    [ "$p100" != "0" ] && [ -n "$p100" ] && [ "$p101" != "0" ] && [ -n "$p101" ] && break
    sleep 3
done
[ "$p100" != "0" ] && [ -n "$p100" ] || fail "queue 100 not bound after start (peer=$p100)"
[ "$p101" != "0" ] && [ -n "$p101" ] || fail "queue 101 not bound after start (peer=$p101)"
OLD_PID=$(daemon_pid)
[ -n "$OLD_PID" ] || fail "no running daemon after start"
OLD_EXE=$(exe_sha "$OLD_PID")
OLD_LINK=$(exe_link "$OLD_PID")
note "old daemon pid $OLD_PID exe $OLD_EXE link $OLD_LINK"

# ---------------------------------------------------------------------------
# Marker server (host loopback, aliased at 10.0.2.2 in the VM) — same
# pattern as the main e2e: a probe that reaches the server through the
# forward path proves ACCEPT; a blocked probe proves DROP.
# ---------------------------------------------------------------------------
start_server() {
    if [ "$(id -u)" = 0 ]; then pkill -f "http.server $SRV_PORT" 2>/dev/null || true
    else sudo -n pkill -f "http.server $SRV_PORT" 2>/dev/null || true; fi
    sleep 0.5
    printf '%s\n' "$MARKER" > "$WORK/proof.txt"
    ( cd "$WORK" && nohup python3 -m http.server "$SRV_PORT" --bind "$SRV_HOST" \
        --directory "$WORK" > "$WORK/server.log" 2>&1 & echo $! > "$WORK/server.pid" )
    sleep 1
    kill -0 "$(cat "$WORK/server.pid")" 2>/dev/null || fail "marker server did not start"
    vm "wget -q -O- http://$SRV_IP:$SRV_PORT/proof.txt" | grep -q "$MARKER" \
        || fail "marker server not reachable from the VM (checked $SRV_IP:$SRV_PORT)"
}
stop_server() { [ -f "$WORK/server.pid" ] && kill "$(cat "$WORK/server.pid")" 2>/dev/null || true; }

# ---------------------------------------------------------------------------
# 3. Baseline: deny blocks, allow passes (both directions)
# ---------------------------------------------------------------------------
start_server
note "baseline: deny policy blocks"
apply_policy "$ROOT/openwrt/files/etc/goshisen/policy.yaml" 0
for i in 1 2 3; do
    [ "$(probe_deny)" = BLOCKED ] || fail "baseline deny probe $i PASSED (must be blocked)"
done
note "baseline: allow policy passes"
apply_policy "$WORK/www/allow.yaml" 2
for i in 1 2; do
    [ "$(probe_deny)" = PASS ] || fail "baseline allow probe $i BLOCKED (must pass)"
done

# ---------------------------------------------------------------------------
# 4. Upgrade with continuous deny probes
# ---------------------------------------------------------------------------
apply_policy "$ROOT/openwrt/files/etc/goshisen/policy.yaml" 0
note "upgrade: continuous deny probes (60 x 1s) during apk add"
run_probes upgrade 60 1

if [ "$ALLOW_UNTRUSTED" -eq 1 ]; then
    note "upgrade: apk add --no-network --allow-untrusted new.apk (PRE-RELEASE UNSIGNED TEST BUILD)"
    vm "apk add --no-network --allow-untrusted /tmp/new.apk > /tmp/upgrade-out.txt 2>&1; echo APKRC=\$?" \
        | grep -a -q 'APKRC=0' || fail "new apk install command failed"
else
    vm "apk add --no-network /tmp/new.apk > /tmp/upgrade-out.txt 2>&1; echo APKRC=\$?" \
        | grep -a -q 'APKRC=0' || fail "new apk install command failed"
fi
UP_OUT=$(vm "cat /tmp/upgrade-out.txt" 45)
printf '%s\n' "$UP_OUT" | grep -a -q "goshisen-$NEW_VER" \
    || fail "new apk install output missing package line: $(printf '%s\n' "$UP_OUT" | tail -3)"
printf '%s\n' "$UP_OUT" | grep -a -q 'post-upgrade' \
    || fail "post-upgrade script did not run: $(printf '%s\n' "$UP_OUT" | tail -3)"
printf '%s\n' "$UP_OUT" | grep -a -q 'already exists' \
    && fail "post-upgrade errored: chain already exists (boot-guard not idempotent)"
printf '%s\n' "$UP_OUT" > "$WORK/upgrade.log"

# Wait for the probe loop to finish (up to 90s).
deadline=$((SECONDS + 90))
while [ $SECONDS -lt $deadline ] && kill -0 "$(cat "$WORK/upgrade.pid")" 2>/dev/null; do sleep 2; done
kill "$(cat "$WORK/upgrade.pid")" 2>/dev/null || true

PROBE_TOTAL=$(wc -l < "$WORK/upgrade.probes" 2>/dev/null || echo 0)
PROBE_PASS=$(count_probe_results upgrade PASS)
PROBE_BLOCKED=$(count_probe_results upgrade BLOCKED)
note "upgrade probes: $PROBE_TOTAL total, $PROBE_PASS passed, $PROBE_BLOCKED blocked"
[ "$PROBE_PASS" -eq 0 ] || fail "UPGRADE FAIL-OPEN: $PROBE_PASS deny probes passed during upgrade"
[ "$PROBE_TOTAL" -gt 0 ] || fail "no probes recorded during upgrade (harness bug)"

# ---------------------------------------------------------------------------
# 5. Assert the running daemon is the NEW binary, no manual restart
# ---------------------------------------------------------------------------
NEW_PID=$(daemon_pid)
[ -n "$NEW_PID" ] || fail "no running daemon after upgrade"
[ "$NEW_PID" != "$OLD_PID" ] || fail "daemon pid unchanged after upgrade ($OLD_PID) — old binary still running"
NEW_EXE=$(exe_sha "$NEW_PID")
[ -n "$NEW_EXE" ] || fail "cannot read /proc/$NEW_PID/exe"
NEW_LINK=$(exe_link "$NEW_PID")
NEW_DISK=$(vm "sha256sum /usr/sbin/goshisen" | grep -aE '^[0-9a-f]{64}' | tail -n1 | cut -d' ' -f1)
note "new pid $NEW_PID exe $NEW_EXE link $NEW_LINK; disk /usr/sbin/goshisen $NEW_DISK"
[ "$NEW_LINK" != *deleted* ] || fail "running exe target is a deleted inode"
[ "$NEW_EXE" = "$NEW_DISK" ] || fail "running exe sha != on-disk sha (running old deleted inode?)"
[ "$NEW_EXE" != "$OLD_EXE" ] || fail "running binary sha still the old binary's"
vm "/usr/sbin/goshisen --version" | grep -a -q "$NEW_VER" \
    || fail "/usr/sbin/goshisen --version does not report $NEW_VER"

# The new apk's payload daemon hash (host-side, SDK apk extract).
NEW_APK_DAEMON=$( (cd "$WORK" && rm -rf apk-x && mkdir apk-x && cd apk-x \
    && /data/owrt-sdk/staging_dir/host/bin/apk extract --allow-untrusted "$WORK/new.apk" >/dev/null 2>&1 \
    && sha256sum usr/sbin/goshisen | awk '{print $1}') || echo UNKNOWN )
if [ "$NEW_APK_DAEMON" != UNKNOWN ]; then
    [ "$NEW_EXE" = "$NEW_APK_DAEMON" ] \
        || fail "running binary sha ($NEW_EXE) != new apk payload sha ($NEW_APK_DAEMON)"
    note "new apk payload daemon sha $NEW_APK_DAEMON"
fi

p100=$(queue_peer 100); p101=$(queue_peer 101)
[ "$p100" != "0" ] && [ -n "$p100" ] || fail "queue 100 not bound after upgrade"
[ "$p101" != "0" ] && [ -n "$p101" ] || fail "queue 101 not bound after upgrade"
note "queues after upgrade: 100 peer=$p100, 101 peer=$p101"

# ---------------------------------------------------------------------------
# 6. Positive control on the new binary
# ---------------------------------------------------------------------------
note "positive control on new binary: allow policy passes"
apply_policy "$WORK/www/allow.yaml" 2
for i in 1 2 3; do
    [ "$(probe_deny)" = PASS ] || fail "post-upgrade allow probe $i BLOCKED (new binary not enforcing)"
done
note "positive control: deny policy blocks again"
apply_policy "$ROOT/openwrt/files/etc/goshisen/policy.yaml" 0
[ "$(probe_deny)" = BLOCKED ] || fail "post-upgrade deny probe PASSED (must be blocked)"

stop_serve
trap 'rm -rf "$WORK"' EXIT

note "PASS: upgrade switched running daemon to new binary (pid $OLD_PID -> $NEW_PID; exe $NEW_EXE); 0/$PROBE_TOTAL deny probes passed during upgrade; queues 100/101 bound; positive control on new binary passed."
exit 0
