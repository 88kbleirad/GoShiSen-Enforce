# Phase 8.1 — NFQUEUE Environment Recovery & Privileged Validation

Date: 2026-11-09. Branch: `phase7-baseline` (commits through `39f3e22` + CI guard `cbd4aee`).

## Verdict: **PENDING integration results** (updated when the VM run completes)

---

## 1. Execution environment

| Item | Value |
| --- | --- |
| Host | Linux 6.18.60-instabox (x86_64) — **lacks NFQUEUE** (`CONFIG_NETFILTER_NETLINK_QUEUE is not set`, no module tree) |
| Execution target | **Disposable VM** created with `debvm-create`/`debvm-run` (QEMU 10, TCG software emulation — no KVM on the host) |
| Guest | Debian trixie (13), kernel **6.12.116+deb13-amd64**, 2 GiB RAM, 4 vCPU |
| NFQUEUE capability (guest) | `CONFIG_NETFILTER_NETLINK_QUEUE=m`, `CONFIG_NFT_QUEUE=m`, `CONFIG_NF_TABLES=m`, `CONFIG_NF_CONNTRACK=m`; `modprobe nfnetlink_queue` exit 0 |
| Toolchain (guest) | Go 1.25.0 (exact declared floor), offline module cache pre-seeded |
| Isolation | guest-only lab (`gsh_*` netns/veth inside the VM); host network/firewall untouched; VM disposable |

## 2. Kernel capability evidence (guest, captured by harness)

```
CONFIG_NETFILTER_NETLINK_QUEUE=m
CONFIG_NFT_QUEUE=m
CONFIG_NF_TABLES=m
CONFIG_NF_CONNTRACK=m
modprobe-nfnetlink_queue-exit:0
```

Functional probe: `scripts/lab.sh up` succeeded in-run 1 (`LAB-UP-EXIT:0`), proving the queue rule installs on guest kernels (the host kernel failed the same rule).

## 3. Test isolation

- `scripts/lab.sh` uses unique `gsh_`-prefixed namespaces/veths; the hook table lives inside `gsh_fw` only.
- Host's pre-existing Docker nft tables were never touched (verified `nft list tables` before/after host-side attempts).
- Cleanup is always executed by the harness (`scripts/lab.sh down` in the same script, then poweroff).

## 4. Commands executed (guest harness)

```bash
go mod verify
scripts/lab.sh up
make integration
scripts/lab.sh down
ip netns list ; nft list tables
```

## 5. Go toolchain regression (host, Go 1.25.0)

| Check | Result |
| --- | --- |
| `go mod verify` | PASS |
| `gofmt -l .` | clean |
| `go vet ./...` | PASS |
| `go test ./...` | PASS (integration skips non-root) |
| `go test -race ./...` | PASS |
| `go build ./...` | PASS |
| `git diff --check` | clean |

## 6. Integration results

### Run 4 (first full suite attempt — harness bug, not product bug)

- Guest harness ran `lab.sh up` **before** `make integration`; each integration test manages its own lab lifecycle, so the pre-created namespaces made every test fail at `lab up: namespaces already present (3)`.
- Outcome: 0 PASS / 21 FAIL (all failed at setup, none skipped), `make` exit 2. The daemon built successfully; `go test` ran the real suite in the guest (no skips).
- **Proof captured**: kernel NFQUEUE functional (lab up OK, module loads OK, daemon built, tests executed).

### Run 5 (final, expected)

- Harness fixed: no pre-up; tests own the lab; final `lab.sh down` + state dump.
- Results appended below on completion.

## 7. CI status

- `.github/workflows/ci.yml`: Go 1.25 for both jobs; integration job now **fails on any SKIP** (`cbd4aee`); `ubuntu-latest` runners (kvm/root/NFQUEUE) are expected to run the suite.
- Local CI state: unverified (branch unpushed; no authorization to push).

## 8. Remaining blockers/risks

- VM run in progress.
- Need explicit integration PASS/FAIL/SKIP matrix from the guest.
- CI (GitHub) verification still requires push authorization.
