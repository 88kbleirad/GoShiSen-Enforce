# Baseline v0.2.0 — GoShiSen-Enforce

Date: 2026-11-13 (UTC)
Task: `Task_15_11_2026.md` — **file not found** in repo, home dir, or git history.
Baseline performed per prompt instructions at HEAD `bfbf813`.

## Repo state

| Item | Value |
|---|---|
| Root | `/home/dariel/GoShiSen-Enforce` |
| Branch | `docs/phase14-v021-notes` |
| Head | `bfbf813` — docs: v0.2.1 draft release notes + release signing proposal |
| Describe | `v0.2.0-12-gbfbf813` |
| v0.2.0 tag | `265902f` — Merge PR #3 (phase10-openwrt) |
| Commits ahead of tag | 12 |
| Commits behind tag | 0 |
| Uncommitted | **0** (working tree clean, no untracked files beyond `evidence/`) |
| Ahead of `origin/main` | 6 (unpushed) |
| Remote | https://github.com/88kbleirad/GoShiSen-Enforce.git |

## Change classification (relative to v0.2.0)

**Committed (12 commits, on `docs/phase14-v021-notes`, 6 unpushed to origin):**
- `8aaac74` docs: README OpenWrt section; Phase 11 release gates (from main via PR #10)
- `c28c27f` fix(package): re-pin PKG_SOURCE_VERSION to v0.2.0 (merged via PR #11)
- `a64e4ec` docs: reconcile IPv6 claims / apk provenance (Phase 12.1)
- `c953595` docs: apk reproducibility wording (Phase 13 Step 1)
- `432c41d` Merge PR #11 fix/package-pin-v0.2.0
- `07751a7` Merge PR #10 docs/phase12-release
- `a99d940` feat(config): opt-in flow-offload policy (warn\|refuse\|readiness)
- `1bbdc07` fix(app): recover readiness when offload clears (offload_policy=readiness)
- `42e54d8` feat(openwrt): plumb offload_policy through UCI into daemon YAML
- `958c5bf` docs: flow-offload policy — user guide, OpenWrt UCI, security, phase status
- `9394e0a` docs: restore v0.2.0 release notes to published tag content
- `bfbf813` docs: v0.2.1 draft release notes + release signing proposal

18 files changed vs tag (676 insertions, 54 deletions): README, SECURITY, configs/goshisen.yaml,
docs/design/{architecture,mvp-scope,signing-proposal}, docs/{openwrt,release-checklist,user-guide},
docs/release/v0.2.1-notes, internal/app{,test}, internal/config{,test}, openwrt/{Makefile, UCI,
init.d}, tests/package/openwrt_init_test.

**Uncommitted:** none.
**Not started:** v0.2.1 release work (draft notes only at `bfbf813`); release signing proposal
(signing-proposal.md) unaudited; tag/publish not performed.

## Environment (machine)

| Item | Value |
|---|---|
| OS | Linux 7.3.3-1-cachyos (x86_64) |
| Go | go1.27.3-X:nodwarf5 linux/amd64 (go.mod requires 1.25.0; CI uses 1.25) |
| nft | nftables v1.1.7 (Commodore Bullmoose #8) |
| UID/GID | 1000/1000 (non-root; no CAP_NET_ADMIN) |
| Docker | 29.9.3 build 5457199aa5 |

## Test results

| Check | Result | Evidence |
|---|---|---|
| `make check` (fmt+vet+test+build+static) | PASS, exit 0 | `make-check.log` |
| `go test -count=1 ./...` (fresh) | PASS, 5.43s wall | `go-test-fresh.log` |
| `go test -race -count=1 ./internal/... ./tests/package/` | PASS, 7.00s wall | (inline) |
| `go test -count=1 -v ./tests/integration/` | 21/21 SKIP (root-gated) | skip reason: "integration tests require root for CAP_NET_ADMIN" |
| Fuzz | not run (baseline; Parser fuzz run is separate) | — |

Test inventory: unit tests in `internal/{api,app,cli,config,hook,metrics,packet,policy}`;
integration `tests/integration/` (21 tests, skip non-root); OpenWrt integration
`tests/openwrt_integration/` (skip non-root); `tests/package/` (no skip).
**No Go benchmarks exist in the repo** (`grep 'func Benchmark'` — empty), so a benchmark
baseline is not reproducible; the test suite wall-time baseline above is the benchmark.

## Built artifact

```
bin/goshisen  v0.2.0-12-gbfbf813  (static, 7,835,772 bytes)
sha256  63ec91cc9c2debc7abb31ac110c13c4e08ca8333c0b377055edbe1068d89b3f5
```

## Known limitations (v0.2.0, from published release notes)

- arm64 binaries built/verified statically only; runtime requires arm64 host.
- Container image not published; see `docs/container-feasibility.md`.
- `nft` must be present on target host.
- IPv6 ACCEPT stateful limited on kernels without IPv6 conntrack-attr delivery
  (INVALID → DROP; stateless IPv6 works).
- Flow-offload: detection only (WARN + gauge), never modifies firewall.
- OpenWrt lab flow-offload bypass impact unobservable (0 packets offloaded under QEMU TCG).
- Root-gated integration suite cannot run on this box; CI privileged job is the gate.

## Reproduce

```sh
git checkout bfbf813        # or use HEAD
make check
go test -count=1 ./...
go test -race -count=1 ./internal/... ./tests/package/
go test -count=1 -v ./tests/integration/   # skips 21/21 as non-root
```
