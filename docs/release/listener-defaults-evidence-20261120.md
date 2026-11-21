# Listener defaults — measured evidence (2026-11-20)

Scope: resolve the contradiction between "the HTTP listeners are disabled
by default" (release notes/advisory wording) and the listener defaults in
`internal/config/config.go` (API `127.0.0.1:8080`, metrics
`127.0.0.1:9090`, an explicit `0` disabling them). Everything below was
**measured**, not read from source.

## Environment

| Item | Value |
| --- | --- |
| OpenWrt guest | 25.12.6 x86/64, kernel `6.12.112`, QEMU TCG (`-machine pc -m 512 -smp 2`), single tap + LAN netns `gsh_owrt_lan`, serial `127.0.0.1:5555` |
| Base image | official `openwrt-25.12.6-x86-64-generic-ext4-combined.img.gz`, sha256 `f8538784d0288b4133a99ac2a68443e4922ffde6343e80df56a5882364b9d499`, provisioned by `scripts/openwrt-image-prep.sh --download --no-daemon` (NFQUEUE kmods for ABI `6.12.112-1-b4125196ecec75391093e1e2edeba490`) |
| Sockets | `netstat -ltnp` inside the guest; HTTP probes = raw HTTP/1.1 over busybox `nc` |
| Repo revision under test (main build) | `41d28d73adf82bf5cc3a39f1096f80ea05ec10fc` (`goshisen 0.2.7-dev-41d28d7`, sha256 `aed281b412821a476412f8394982b2406a431078b9f1d84f43c9effa7df401c7`) |
| Host kernel | `Linux instabox-run-api 6.18.60-instabox` (no `nfnetlink_queue`; the daemon cannot complete startup on the host, hence the QEMU guest for every live measurement) |

Artifacts (sha256 verified on disk before use):

| Artifact | sha256 | payload `usr/sbin/goshisen` sha256 |
| --- | --- | --- |
| `goshisen-linux-amd64.tar.gz` (v0.2.5) | `f08b965ea28fc474b63a19e6435290d34fe90458f9eabc12e5e4aa76fd40dd80` | binary `95b9fc3ee6f9598849236d22929c1bcae34618f638b3bd9bbff7597f9b016ec4` |
| `goshisen-linux-amd64.tar.gz` (v0.2.6) | `eb45768931f080c8b9584a5a7edca007fd1ed0ac18919075f65f4a67d66a978d` (binary) | same file |
| `goshisen-0.2.0-r2.apk` | `b4925a60a594159809bd03c10f73843ca3be8ba6748e6343dcc0e1af00a9c2af` | `a00e4a52626a1f5203255c01ce92c8b94dcb04eaef3c5310f9c27a00136cf6d1` |
| `goshisen-0.2.3-r1.apk` | `948e2e1de454c159bed81bfe845bad160dcf9454238b532b2a87f2e335b0cc6a` | `57aef2cf558d80ed8d51c9e7f746021e101cde7a9427fc6432eaab69735b89c0` |
| `goshisen-0.2.5-r1.apk` | `4495344ec1be11cac9fc7c96c7cdcb72b5222e613121d20f6043c76262357c7b` | `c117a9aafe4b50313640006b67e53c45e5ab2e1ae8569e804d4b633f1a42c051` (matches published) |
| `goshisen-0.2.6-r1.apk` (signed) | `6e9a102afb545894e3ac0a7d92e778d6bdbac97f1ac7b038a166ed023f23c86c` (matches published) | `4ebf71b0cc5911518b9765f18f79aadee3d36d66e387cc657fcc3824e393f179` (matches published) |
| apk anchor `keys/goshisen-apk.pub.pem` | `e3bf8bc384bfe34bd2074b2f663baf7198651ab0ebe4554fe2de3e01436a819c` | – |

apk payloads were extracted with apk-tools 3.0.5 out of
`openwrt/sdk@sha256:a17d6436…` (no host apk-tools); the guest payload
hashes were additionally re-checked against `/usr/sbin/goshisen` after
`apk add`.

## 1. CLI archive, no config / no env / no flags

```
BINARY=/tmp/goshisen-v025      CMDLINE=/tmp/goshisen-v025 --policy-file /tmp/policy.yaml --interface lo
tcp 0 0 127.0.0.1:8080 0.0.0.0:* LISTEN 12541/goshisen-v025
tcp 0 0 127.0.0.1:9090 0.0.0.0:* LISTEN 12541/goshisen-v025
GET  /healthz :8080 -> HTTP/1.1 200 OK        GET  /readyz :8080 -> HTTP/1.1 200 OK
GET  /metrics :8080 -> HTTP/1.1 200 OK        GET  /v1/policy :8080 -> HTTP/1.1 404 Not Found
POST /v1/policy (no token) :8080 -> HTTP/1.1 401 Unauthorized
GET  /metrics :9090 -> HTTP/1.1 200 OK
```

v0.2.6 is identical (`eb457689…`, pid 13361): both ports on loopback.

## 2. Knob matrix (CLI archive)

Rendered command lines and sockets per case; `ss`/`netstat` output taken
2 s after start, `/proc/<pid>/cmdline` recorded per case.

| Case (v0.2.5 and v0.2.6 behave identically) | Listening sockets |
| --- | --- |
| `GOSHISEN_API_PORT=0 GOSHISEN_METRICS_PORT=0` | none (all probes refused) |
| YAML `api_port: 0` + `metrics_port: 0` (`--config`) | `127.0.0.1:8080`, `127.0.0.1:9090` — **file `0` ignored** |
| YAML `api_port:` / `metrics_port:` (empty value) | `127.0.0.1:8080`, `127.0.0.1:9090` |
| `--api-port 0 --metrics-port 0` | `127.0.0.1:8080`, `127.0.0.1:9090` — **flag `0` ignored** |
| `--api-port 0` only | `127.0.0.1:8080`, `127.0.0.1:9090` |
| YAML `api_port: ""` | none — daemon refuses to start (`yaml: unmarshal errors … cannot unmarshal !!str`) |
| `GOSHISEN_API_PORT=` / `GOSHISEN_METRICS_PORT=` (empty) | `127.0.0.1:8080`, `127.0.0.1:9090` (empty env is "unset") |
| file `0` + env `GOSHISEN_API_PORT=9101 GOSHISEN_METRICS_PORT=0` | `127.0.0.1:9101` only |
| file `9100`/`9091` + env `GOSHISEN_API_PORT=0` + `--metrics-port 0` | `127.0.0.1:9091` only |
| file `0` + `--api-port 9102 --metrics-port 0` | `127.0.0.1:9102`, `127.0.0.1:9090` |

Precedence proof (`goshisen config show`):

```
api_host:     "127.0.0.1" (from default)
api_port:     8080 (from default)          metrics_port: 9090 (from default)
--- file with api_port: 0 / metrics_port: 0
api_port:     8080 (from default)          metrics_port: 9090 (from default)   # file 0 not applied
--- file 9100/9091 + env GOSHISEN_API_PORT=0 + --api-port 9102 --metrics-port 0
api_port:     9102 (from flag)             metrics_port: 0 (from flag)
```

Layer order measured: **defaults < file < env < flags**.

Fixed daemon (main, `0.2.7-dev-41d28d7`) repeats of the same matrix: file
`0`, empty YAML value, and `--api-port 0`/`--metrics-port 0` **do**
disable; unset still falls back to 8080/9090; `--api-port 0` alone leaves
only `127.0.0.1:9090`.

## 3. OpenWrt fresh install (packaged config untouched, service enabled)

Both installs start the same way:

```
DAEMON_PID=3007         CMDLINE=/usr/sbin/goshisen --config /var/run/goshisen/config.yaml
UCI: goshisen.main.api_port='0'  goshisen.main.metrics_port='0'   (packaged defaults)
RENDERED_CONFIG: … interface: br-lan / api_port: 0 / metrics_port: 0
LISTEN: tcp 0 0 127.0.0.1:9090 0.0.0.0:* LISTEN 3007/goshisen
        tcp 0 0 127.0.0.1:8080 0.0.0.0:* LISTEN 3007/goshisen
GET /healthz 8080 -> 200 OK   GET /readyz 8080 -> 200 OK   GET /metrics 8080 -> 200 OK
GET /v1/policy 8080 -> 404    POST /v1/policy 8080 (no token) -> 401 Unauthorized
GET /metrics 9090 -> 200 OK
```

| Install | pkg | payload sha256 | listeners |
| --- | --- | --- | --- |
| fresh, signed | `goshisen-0.2.5-r1` | `c117a9aa…` | `127.0.0.1:8080`, `127.0.0.1:9090` |
| fresh, signed | `goshisen-0.2.6-r1` | `4ebf71b0…` | `127.0.0.1:8080`, `127.0.0.1:9090` |
| fresh, unsigned (`--allow-untrusted`) | `goshisen-0.2.0-r2` | `a00e4a52…` | `127.0.0.1:8080`, `127.0.0.1:9090` |
| fresh, signed | `goshisen-0.2.3-r1` | `57aef2cf…` | `127.0.0.1:8080`, `127.0.0.1:9090` |

## 4. OpenWrt upgrade paths and degraded configs

| Scenario | Result |
| --- | --- |
| 0.2.3-r1 running with listeners up → `apk add goshisen-0.2.6-r1` (postinst restart, config preserved) | after upgrade `DAEMON_PID=3266` `CMDLINE=/usr/sbin/goshisen --config /var/run/goshisen/config.yaml`; `127.0.0.1:8080` + `127.0.0.1:9090` listening; `GET /healthz` 200; `POST /v1/policy` 401; UCI still `api_port='0' metrics_port='0'`; `/etc/config/goshisen.apk-new` written |
| 0.2.0-r2 / 0.2.3-r1 → 0.2.5-r1 / 0.2.6-r1 with the config as shipped (`'0'`) | same two listeners after each restart |
| UCI `api_port`/`metrics_port` **absent** (older/minimal config) | init script aborts: `goshisen: api_port '' out of range`; no daemon, no listener; a stale `/var/run/goshisen/config.yaml` may remain from the previous render |
| UCI options present but **empty** (`option api_port ''`) | identical abort, no listener |
| `/etc/config/goshisen` **missing** | `goshisen: service is disabled (goshisen.main.enabled=0)`; no daemon, no listener |

Rendered procd command line in every starting case:
`/usr/sbin/goshisen --config /var/run/goshisen/config.yaml` (the init
script always renders `api_port:`/`metrics_port:` from UCI).

## 5. Conclusion

* The built-in default is **listeners on, loopback only**; "disabled by
  default" was wrong for the CLI archives and for an enabled OpenWrt
  service on every release ≤ v0.2.6.
* On ≤ v0.2.6 the only knobs that actually disable a listener are
  `GOSHISEN_API_PORT=0` / `GOSHISEN_METRICS_PORT=0` or not running the
  daemon (`goshisen.main.enabled=0`); the UCI option, the YAML key and
  the CLI flag are ignored.
* The package cannot start a listener without the operator enabling the
  service (shipped `enabled '0'`, no autostart), so there is no silent
  listening case — but an enabled service always exposes both loopback
  listeners and the UCI option cannot turn them off. Tracked as its own
  issue (proposed init-script fix: always render explicit port values,
  default `0`; migrate configs without the options).
* Post-v0.2.6 (`main`) honours an explicit `0` in every layer; that fix
  is **not in any published artifact** (v0.2.7 candidate).
