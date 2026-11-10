#!/usr/bin/env bash
# Lab supervisor restart loop for GoShiSen-Enforce.
#
# This is a minimal, dependency-free supervisor for the lab: it runs the
# daemon inside the router namespace and restarts it when it exits, so
# a crash is automatically recovered (the daemon reinstalls the hook and
# rebinds the queue on startup).
#
# Usage (as root):
#   scripts/lab.sh up
#   sudo scripts/supervisor.sh            # run in the foreground
#
# The loop keeps running until Ctrl-C; the daemon itself handles
# SIGTERM/SIGINT (graceful shutdown, exit 0), so the loop exits cleanly
# when the daemon shuts down gracefully and restarts only on a crash
# (non-zero exit).
#
# A real deployment should use systemd (deployments/systemd/goshisen.service)
# or a container supervisor instead; this exists for the lab only.

set -u

BIN=${BIN:-./bin/goshisen}
POLICY=${POLICY:-policies/examples/allow-lab.yaml}
IFACE=${IFACE:-gsh-fw1}

if [ "$(id -u)" != 0 ]; then
  echo "supervisor: must run as root" >&2
  exit 1
fi
if ! ip netns list | grep -qx gsh_fw; then
  echo "supervisor: lab router namespace gsh_fw missing (run scripts/lab.sh up)" >&2
  exit 1
fi

echo "supervisor: starting goshisen in gsh_fw (restart on crash)"
while true; do
  ip netns exec gsh_fw "$BIN" \
    --interface "$IFACE" \
    --policy-file "$POLICY" \
    --log-level info
  status=$?
  if [ "$status" -eq 0 ]; then
    echo "supervisor: daemon exited cleanly; stopping loop"
    exit 0
  fi
  echo "supervisor: daemon exited with status $status; restarting in 2s" >&2
  sleep 2
done
