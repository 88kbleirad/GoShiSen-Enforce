#!/usr/bin/env python3
"""Interact with the OpenWrt QEMU serial console.

Usage:
  openwrt-console.py --port 5555 --cmd "uname -a"
  openwrt-console.py --port 5555 --wait "login:" --timeout 180
  openwrt-console.py --port 5555 --cmd "nft list ruleset" --expect "table"

Connects to the QEMU telnet serial server (raw TCP), optionally waits
for a marker, sends a command, waits for an expected output, and prints
the session transcript.
"""
import argparse
import socket
import sys
import time


def recv_until(sock, marker, timeout, transcript):
    sock.settimeout(timeout)
    buf = b""
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            chunk = sock.recv(4096)
        except socket.timeout:
            break
        if not chunk:
            break
        buf += chunk
        transcript += chunk
        if marker and marker.encode() in buf:
            return buf
    return buf


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--port", type=int, default=5555)
    ap.add_argument("--wait", default=None, help="marker to wait for")
    ap.add_argument("--timeout", type=float, default=30)
    ap.add_argument("--cmd", action="append", default=[],
                    help="command to send (repeatable)")
    ap.add_argument("--expect", action="append", default=[],
                    help="expected output marker (repeatable)")
    ap.add_argument("--enter", action="store_true", help="send Enter first")
    args = ap.parse_args()

    transcript = bytearray()
    try:
        s = socket.create_connection(("127.0.0.1", args.port), timeout=5)
    except OSError as e:
        print(f"connect failed: {e}", file=sys.stderr)
        sys.exit(1)

    def send(data):
        s.sendall(data)
        time.sleep(0.5)

    if args.enter:
        send(b"\r")
    if args.wait:
        recv_until(s, args.wait, args.timeout, transcript)
    for c in args.cmd:
        send(c.encode() + b"\r")
    # collect output
    time.sleep(1.0)
    recv_until(s, None, 3.0, transcript)
    s.close()

    text = transcript.decode(errors="replace")
    sys.stdout.write(text)
    missing = [e for e in args.expect if e not in text]
    if missing:
        print(f"\n[openwrt-console] missing expected markers: {missing}",
              file=sys.stderr)
        sys.exit(1)
    print(f"\n[openwrt-console] done (got {len(text)} chars)")


if __name__ == "__main__":
    main()
