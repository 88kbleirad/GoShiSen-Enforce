#!/usr/bin/env python3
"""Anomaly sweep for the goshisen CLI (read-only subcommands only).

Never runs the daemon or any mutating subcommand (hook cleanup / offload
disable); safe on a shared host. Looks for panics, hangs, silent failures
and secret leakage.
"""
import os
import random
import string
import subprocess
import sys
import tempfile

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
BIN = os.environ.get("GOSHISEN_BIN", "")

if not BIN:
    # Build the CLI under test (default: this checkout) into a temp path.
    out = os.path.join(tempfile.gettempdir(), "goshisen-sweep-under-test")
    subprocess.run(["go", "build", "-o", out, "./cmd/goshisen"], cwd=REPO, check=True)
    BIN = out
T = tempfile.mkdtemp(prefix="gshsweep-")
print(f"binary: {BIN}")
READ_ONLY = [
    ["config", "show"],
    ["config", "check"],
    ["policy", "validate"],
    ["policy", "simulate"],
    ["policy", "dry-run"],
    ["hook", "status"],
    ["offload", "status"],
    ["offload", "observe"],
]


def w(name, data, mode=None):
    p = os.path.join(T, name)
    with open(p, "wb") as f:
        f.write(data if isinstance(data, bytes) else data.encode())
    if mode is not None:
        os.chmod(p, mode)
    return p


# --- fixtures -------------------------------------------------------------
good_policy = "version: 1\ndefault_action: DROP\nrules: []\n"
good_cfg = "policy_file: %s\npolicy_dir: %s\nqueue_num: 100\nqueue_maxlen: 1024\ninterface: lo\napi_port: 0\nmetrics_port: 0\napi_token: SUPER-SECRET-TOKEN-1234\n" % (
    os.path.join(T, "policy.yaml"), os.path.join(T, "store"),
)
p_good_policy = w("policy.yaml", good_policy)
p_good_cfg = w("config.yaml", good_cfg)

nasty_policies = {
    "empty": b"",
    "nul": b"version: 1\n\x00\ndefault_action: DROP\n",
    "bom": b"\xef\xbb\xbfversion: 1\ndefault_action: DROP\n",
    "crlf": b"version: 1\r\ndefault_action: DROP\r\n",
    "tabs": b"version:\t1\ndefault_action:\tDROP\n",
    "dup_keys": b"version: 1\nversion: 2\ndefault_action: DROP\n",
    "deep": ("a: " + "[" * 400 + "]" * 400 + "\n").encode(),
    "huge_scalar": b"version: 1\ndefault_action: DROP\nrules: " + b"x" * 200000 + b"\n",
    "unicode": "version: 1\ndefault_action: DROP\nrules: []\n# \u4e2d\u6587 \U0001f600\n".encode(),
    "bad_version": b"version: 99999\ndefault_action: DROP\n",
    "bad_action": b"version: 1\ndefault_action: MAYBE\n",
    "newline_inject": b"version: 1\ndefault_action: DROP\nrules:\n  - id: x\n    action: ACCEPT\n    description: \"a\\nb\"\n",
    "yaml_anchor": b"a: &x [1,2]\nb: *x\nversion: 1\ndefault_action: DROP\n",
    "python_tag": b"!!python/object/apply:os.system ['true']\n",
    "only_comment": b"# nothing\n",
    "null_rule": b"version: 1\ndefault_action: DROP\nrules: null\n",
    "rule_no_action": b"version: 1\ndefault_action: DROP\nrules:\n  - id: r1\n",
    "weird_ports": b"version: 1\ndefault_action: DROP\nrules:\n  - id: r\n    action: ACCEPT\n    protocol: tcp\n    dst_port: \"0:65536\"\n",
    "ipv6_any": b"version: 1\ndefault_action: DROP\nrules:\n  - id: r\n    action: ACCEPT\n    protocol: tcp\n    src: ::/0\n    dst: any\n",
}
nasty_cfgs = {
    "empty": b"",
    "nul": b"policy_file: /x\x00y\n",
    "huge": b"log_level: " + b"A" * 100000 + b"\n",
    "bad_yaml": b"policy_file: [unclosed\n",
    "dup": b"queue_num: 1\nqueue_num: 2\n",
    "unicode": "interface: \u00e9\u00e8\U0001f600\n".encode(),
    "negative": b"api_port: -1\nmetrics_port: -2\nqueue_num: -5\nqueue_maxlen: -9\n",
    "over_range": b"api_port: 99999\nmetrics_port: 70000\nqueue_num: 65535\nqueue_maxlen: 999999\n",
    "float_ports": b"api_port: 80.5\nmetrics_port: 1e9\n",
    "empty_scalar": b"api_port: \"\"\nmetrics_port: \"\"\n",
    "api_host_nonloop": b"api_host: 0.0.0.0\n",
    "api_host_loop": b"api_host: 127.0.0.1\n",
    "unknown_key": b"nonsense_key: 1\n",
    "tabs": b"policy_file:\t/x\n",
    "bool_weird": b"allow_bypass: maybe\nallow_non_loopback_api_host: 2\n",
    "crlf": b"queue_num: 5\r\n",
    "bom": b"\xef\xbb\xbfqueue_num: 5\n",
}
for n, b in nasty_policies.items():
    w("np_" + n + ".yaml", b)
for n, b in nasty_cfgs.items():
    w("nc_" + n + ".yaml", b)
w("ro.yaml", good_cfg, 0o000)                      # unreadable file
os.symlink(os.path.join(T, "loop_a"), os.path.join(T, "loop_b"))
os.symlink(os.path.join(T, "loop_b"), os.path.join(T, "loop_a"))  # symlink loop
os.makedirs(os.path.join(T, "adir"), exist_ok=True)  # a directory as config

# --- cases ---------------------------------------------------------------
cases = []
for cmd in READ_ONLY:
    cases.append(cmd)
    for n in list(nasty_policies)[:6]:
        cases.append(cmd + ["--policy-file", os.path.join(T, "np_" + n + ".yaml")])
    for n in list(nasty_cfgs)[:6]:
        cases.append(cmd + ["--config", os.path.join(T, "nc_" + n + ".yaml")])
cases += [
    ["--version"], ["-h"], ["--help"], ["-V"],
    ["config", "show", "--config", p_good_cfg],
    ["config", "check", "--config", p_good_cfg],
    ["policy", "validate", "--policy-file", p_good_policy],
    ["policy", "simulate", "--policy-file", p_good_policy, "--src", "10.0.0.1", "--dst", "10.0.0.2"],
    ["policy", "dry-run", "--policy-file", p_good_policy, "--policy-dir", os.path.join(T, "store")],
    ["config", "show", "--config", os.path.join(T, "ro.yaml")],
    ["config", "show", "--config", os.path.join(T, "adir")],
    ["config", "show", "--config", os.path.join(T, "loop_a")],
    ["config", "show", "--config", "/nonexistent/nope.yaml"],
    ["config", "show", "--config", ""],
    ["policy", "validate", "--policy-file", "/dev/null"],
    ["policy", "validate", "--policy-file", "/dev/urandom"],
    ["policy", "simulate", "--policy-file", p_good_policy],          # missing --src/--dst
    ["policy", "simulate", "--policy-file", p_good_policy, "--src", "not-an-ip", "--dst", "also-bad"],
    ["policy", "simulate", "--policy-file", p_good_policy, "--src", "10.0.0.1", "--dst", "10.0.0.2", "--protocol", "icmp"],
    ["policy", "simulate", "--policy-file", p_good_policy, "--src", "10.0.0.1", "--dst", "10.0.0.2", "--dest-port", "0"],
    ["policy", "simulate", "--policy-file", p_good_policy, "--src", "10.0.0.1", "--dst", "10.0.0.2", "--dest-port", "65536"],
    ["policy", "dry-run", "--policy-file", p_good_policy, "--policy-dir", "/proc/self/nope"],
    ["hook", "status", "--config", p_good_cfg],
    ["offload", "status"],
    ["unknown-command"], ["-x"], ["--bogus-flag"],
    ["config", "show", "--unknown-flag"],
    ["policy", "validate", "extra-positional"],
    ["config"], ["policy"], ["hook"],
    ["config", "show", "--api-token", "leak?"],
]
rnd = random.Random(1234)
alphabet = string.ascii_letters + string.digits + "=/.:-'\" \\t\\u00e9"
for _ in range(120):
    cases.append(["".join(rnd.choice(alphabet) for _ in range(rnd.randint(0, 24)))])

# --- run -----------------------------------------------------------------
fails = []
panics = []
hangs = []
leaks = []
for args in cases:
    try:
        p = subprocess.run([BIN] + args, capture_output=True, timeout=15, text=True)
    except subprocess.TimeoutExpired:
        hangs.append(args)
        print("HANG:", args)
        continue
    out = (p.stdout or "") + (p.stderr or "")
    low = out.lower()
    if "panic:" in low or "goroutine " in low:
        panics.append((args, out[:400]))
        print("PANIC:", args, "->", out[:200].replace("\n", " "))
    if p.returncode not in (0, 1, 2, 3):
        fails.append((args, p.returncode, out[:200]))
        print("ODD EXIT", p.returncode, args, out[:160].replace("\n", " "))
    if "SUPER-SECRET-TOKEN-1234" in out:
        leaks.append((args, out[:200]))
        print("SECRET LEAK:", args)

print("\n=== SUMMARY ===")
print("cases:", len(cases))
print("panics:", len(panics), "| hangs:", len(hangs), "| odd exits:", len(fails), "| secret leaks:", len(leaks))
for a, o in panics[:5]:
    print(" panic case:", a)
print("SWEEP-OK" if not (panics or hangs or fails or leaks) else "SWEEP-PROBLEMS")
