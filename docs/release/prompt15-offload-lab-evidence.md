# Offload Detect + Disable — OpenWrt Lab Evidence (Prompt 15)

**Date:** 2026-11-13 (UTC)
**Status:** PASS — live OpenWrt QEMU lab verified detect, disable (verified),
enforce fail-closed, watchdog re-disable, and escape-hatch semantics.

---

## 1. Environment inventory (target: OpenWrt 25.12.6, QEMU TCG)

| Item | Value |
| --- | --- |
| Kernel | 6.12.112 |
| Distro | OpenWrt 25.12.6 (`r33311-981c978eff`) |
| Interfaces | `br-lan`, `eth0`, `eth1`, `lo` |
| Driver (br-lan) | virtio (`/sys/class/net/br-lan/device/driver` → virtio0/virtio1) |
| ethtool | **not installed** in the VM — no HW offload features enumerable; no HW-offload verification claim |
| bridge-nf sysctl | n/a (no bridge netfilter module loaded in VM) |
| `flow_offloading` (UCI) | `firewall.@defaults[0].flow_offloading='1'` |
| fw4 flowtable | `table inet fw4 { flowtable ft { hook ingress priority filter; devices = { "br-lan", "eth0" } } }` |
| Flow-offload rule | `meta l4proto { tcp, udp } flow add @ft` (fw4 forward, handle 91) |

**Limit recorded:** QEMU TCG offloads 0 packets; flowtable presence is detected
and disabled, but packet bypass itself is unobservable here. No claim of
hardware offload verification (backlog #3 remains open).

## 2. Detection (live)

`goshisen offload status`:

```
offload: ENABLED (enforcement gap): flow-offload rule inet/fw4/forward:
  meta l4proto { tcp, udp } flow add @ft # handle 91; flowtable inet/fw4/ft
```

Correctly identifies both the rule and the flowtable object.

## 3. Disable (live, verified)

`goshisen offload disable`:

```
offload: disabled (verified)     # exit 0
```

After: `nft list flowtables` shows `table inet fw4 {}` (flowtable `ft` gone),
and `nft -a list ruleset | grep 'flow add @ft'` returns nothing. Only the
goshisen table remains. Idempotent: running again is a no-op `disabled`.

## 4. Daemon lifecycle (live, `offload_policy=enforce`)

1. Boot with `flow_offloading=1` → fw4 creates flowtable.
2. `uci set goshisen.main.offload_policy=enforce`, start service.
3. Log sequence:
   ```
   level=INFO msg="offload_policy=enforce: flow offloading disabled (verified)" detail="flow offload disabled"
   level=INFO msg="nftables hook installed" table=goshisen scope=openwrt
   level=INFO msg="NFQUEUE bound" queue=100 maxlen=1024
   ```
4. Result: service `running`; `nft list flowtables` empty; ready API
   `{"status":"ready"}`; queue 100 bound; hook present
   (forward, priority filter+1, queue 100/101).
5. Traffic: policy allows 10.0.3.0/24→10.0.2.2:8088; daemon verdict metrics
   show accepted TCP packets passing through the queue (enforcement active).

## 5. Watchdog re-disable (live)

After startup, re-enabled `flow_offloading=1` + `fw4 restart` → flowtable
recreated. Within one watchdog interval the daemon re-detected and re-disabled:

```
goshisen_flow_offload_disables_total{result="success"} 2   # second disable
goshisen_flow_offload_state 0
```

Flowtable confirmed gone again; service continued running.

## 6. Fail-closed `refuse` (live)

With flowtable present and `offload_policy=refuse`, service fails to start and
logs:

```
flow offloading is active but offload_policy=refuse: established flows bypass
the queue (enforcement gap); disable flow offloading or set
offload_policy=warn/readiness (flow-offload rule inet/fw4/forward:
meta l4proto { tcp, udp } flow add @ft # handle 36; flowtable inet/fw4/ft)
```

Service state: `not running` (repeated attempts all fail closed; hook never
installed).

## 7. Escape hatch (live)

`offload_policy=enforce` + `allow_bypass=1` with flowtable present: service
starts and, because the disable **succeeded** in this run, logs the normal
"disabled (verified)" path — the bypass only engages when disable fails (its
only purpose). State metric returns to 0; service running.

## 8. Boot ordering

Image prep installs boot guard (S12), fw4 (S19), daemon (S90). The flowtable is
created by fw4 at boot *before* the daemon starts, so the daemon's startup
detect sees it — this is exactly the ordering the enforce path must handle, and
it does (disable verified before hook install). No offload re-enables after
bind: the watchdog covers post-startup reconfiguration.

## 9. Findings fixed during validation

1. **init script YAML bool:** `allow_bypass: $(config_get ...)` rendered `0`
   which Go YAML rejected (`cannot unmarshal !!int 0 into bool`) → daemon failed
   to start. Fixed (`b2e35c2`): init renders true/false.

## 10. What was NOT verified (honest limits)

- Hardware driver flow offload (virtio/QEMU has none; ethtool absent) — the
  nft-observable offload is the verified scope.
- Packet bypass itself under an active flowtable (TCG offloads 0 packets).
- `enforce` fail-closed on disable-failure live (covered by unit tests; the lab
  disable always succeeds) — the live fail-closed proof is the `refuse` case.

## 11. Evidence location

- Sandbox: lab image `/tmp/owrt-offload2.img` (deleted after validation);
  serial transcripts in this record; `~/gs-deploy` code head `b2e35c2`.
- Local `make check` + `go test ./internal/... ./tests/...` pass (11 packages).
- Note: instabox host nft (non-VM) rejects `queue to N` syntax (older nft),
  causing `TestBuildSyntaxValidated` to fail on that host only — CI ubuntu
  runner + OpenWrt VM (nft 1.1+) are authoritative; hook install verified live.
