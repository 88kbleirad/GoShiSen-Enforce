# Phase 8.3 — NFQUEUE BIND ENODEV Root Cause Diagnosis

Date: 2026-11-10. Branch: `phase7-baseline`.

## Verdict: **PENDING probe evidence** (updated on completion)

---

## 1. Run 7 failure summary

All 21 privileged integration tests failed at `nfqueue: bind queue 0: netlink receive: no such device` (ENODEV) even after correcting the attribute type numbers. The kernel also logged `netlink: 8 bytes leftover after parsing attributes in process 'goshisen'` per daemon start.

## 2. Hypothesis (before probe)

The `struct nlattr` header (`nla_len`/`nla_type`) is read by the kernel directly off the wire in **native byte order** (plain C struct access; see `__nla_put`/`__nla_reserve` in `lib/nlattr.c`). The encoder wrote these fields **big-endian** (`binary.BigEndian`), so on a little-endian host the kernel read `nla_len=0x0008` as `0x0800` (2048 → `nla_ok` false/skip) and `nla_type=0x0001` as `0x0100` (256 > maxtype → skipped) → cmd attribute never recognized → `cmd==NULL` → `-ENODEV`, and the parser consumed nothing (8 bytes leftover warning).

## 3. Probe environment

- VM: Debian 13 (trixie), kernel 6.12.116+deb13-amd64, QEMU TCG.
- Probe: `tests/bindprobe` (class `packet.Open` with `debugWire` enabled).
- Harness: boot-time service runs `/root/run-bindprobe.sh` → `/root/bindprobe 0` → prints wire bytes, exit status, `/proc/net/netfilter/nfnetlink_queue` → poweroff.

## 4. Expected wire (after fix, little-endian host)

```
nlmsghdr: len=28, type=0x0302 (NFQNL_MSG_CONFIG), flags=1, seq, pid
nfgenmsg: 02 00 00 00            (family=AF_INET, version=0, res_id=0)
attr:     08 00 01 00 01 00 02 00  (nla_len=8 NATIVE, nla_type=1 NATIVE,
                                     command=BIND(1), pad, pf=AF_INET)
```

## 5. Result

(insert probe output)

## 6. Fixes

- `internal/packet/capture.go`: `attr()`/`parseAttrs()` → native byte order for `struct nlattr` header fields (payloads keep UAPI endianness).
- `internal/packet/netlink_test.go`: golden tests updated to native-endian headers; endianness guard.
