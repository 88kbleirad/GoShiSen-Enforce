# Proposal — v0.2.8 scope (after v0.2.7)

**Status: IN PROGRESS (updated 2026-11-21, after the post-v0.2.7 audit
and D1/D2 implementation).** Written after the v0.2.7 release was
published (2026-11-21) and its post-publish self-test loop (rounds 1–3:
deps, static, race/fuzz, HTTP probes, release chain, CLI anomaly sweep).
Priority order is risk-weighted: **P1 = security/correctness**,
**P2 = regression coverage**, **P3 = platform/ops**.

Base: `main` at `f9ca4f3` (docs/changelog post-publish), plus the
post-release hardening already merged (#102 store perms, #104 bounded
reads, #105–#107 CI sweep/fuzz/staticcheck/govulncheck, #108 UCI
api_token) and the open PRs listed below.

## What is IN v0.2.8 (decided)

| Item | State |
| --- | --- |
| Bounded reads for config/CLI/daemon/store (P1.1) | **merged** (#104); FIFO-swap TOCTOU closed in #112 |
| UCI `api_token` (P1.2, issue #103) | **merged** (#108); file-permission follow-up in #109 |
| D1 read-auth opt-ins (P1.3, issue #99) | **PR #110** (`require_token_for_reads`, `metrics_token_required`; opt-in, default off) |
| D2 govulncheck modes + weekly sweep (P2/CI) | **PR #111** (warn-only on PRs; blocking in release gates; weekly dedup issue) |
| Sweep/fuzz in CI, staticcheck baseline, govulncheck in CI (P2.5–P2.8) | **merged** (#105, #106, #107) |
| `internal/store` tests (P2.7) | **merged** with #102/#104 (perms, non-regular files) |

Still to land in v0.2.8 (maintainer decision): signed tags (P1.4) and
F-02 (P3.10) require anchor/environment configuration on GitHub.

## What moved to v0.2.9 (needs infrastructure, not code)

| Item | Why it moves | Prerequisite |
| --- | --- | --- |
| arm64 hardware verification (P3.9) | TCG-only today; a real board or a self-hosted arm64 runner is needed | hardware/runner |
| `scripts/release-run.sh` wrapper (P3.11) | the flow was re-run by hand this session; the wrapper should be written against a *release rehearsal*, not invented | one more release run to codify |
| Upgrade e2e in CI (P3.12) | needs published v(N-1)+v(N) artifacts and a privileged runner budget | scheduled job budget |
| F-02 environment scoping (P3.10) | maintainer-only GitHub settings (create/move secret, reviewers, branch policy) | maintainer action on #100 |
| Signed tags + `GATE_TAG_SIGN_CHECK=1` (P1.4) | SSH `allowed_signers` must exist first | maintainer key setup |
| Metrics for reload rejections / read-bounded (P3.13) | useful but not security-critical; keep scope tight for a hardening release | — |

## Where v0.2.7 left us (measured, not assumed)

| Area | State |
| --- | --- |
| Reachable CVEs | 0 (govulncheck source + binary on tag, main, both CLI archives, apk payload) |
| Race / fuzz | `go test -race` green; FuzzParse 8.2 M, FuzzParseAttrs 12.5 M, FuzzCompile 167 k execs, no crash |
| Static | `go vet` clean; staticcheck: only test-file findings; gosec: store perms fixed (PR #102); rest triaged FP |
| Anomaly sweep (published CLI, 257 cases) | 1 real defect → **fixed in PR #104** (unbounded `os.ReadFile` on `/dev/urandom`: ∞ hang, ~1 GiB/s RSS); 0 panics, 0 secret leaks |
| Open security/functional gaps | issue #103 (`api_token` unreachable on OpenWrt), unauth GETs on `/v1/policy` + `/v1/policy/versions` (documented, decision A/B), unsigned annotated tags |

## P1 — security & correctness

1. **Bounded reads everywhere operator input is read (PR #104 — merge
   first).** The sweep's single defect class is "operator points a path
   at something that is not a regular file". PR #104 covers config +
   policy (CLI and daemon). **Extension for v0.2.8:** the storage
   readers (`internal/store`: `active.json`, version files) and
   `internal/packet/qstats` (procfs) still use unbounded `os.ReadFile`.
   Local write access to `policy_dir` makes the API read a FIFO/device;
   use `config.ReadFileBounded` (or a small `internal/safefile` package)
   there too, and add a *reload-under-hostile-store* test.
2. **Close issue #103 — UCI `api_token`.** Implemented in PR #108:
   `option api_token ''` packaged, rendered by `render_config()` into
   `/var/run/goshisen/config.yaml`, which is now `chmod 600` (the
   rendered file used to inherit the umask — 0644). Tokens are
   restricted to a YAML-plain-safe charset `A-Za-z0-9._-` (start aborts
   naming the option otherwise), a warn fires when the API listener is
   enabled without a token (read-only API), and contract tests cover
   absent/empty/set tokens, the 0600 mode, and rejections. Docs EN + VI
   updated.
3. **Decide the read-endpoint question (issue from the v0.2.7 run).**
   Recommended default: when `api_token` is configured, require it for
   `GET /v1/policy` and `GET /v1/policy/versions` too (keep `/healthz`,
   `/readyz`, `/metrics` open). This is a breaking change for scripts
   that read policies unauthenticated; ship it with a documented
   `--api-allow-unauthenticated-reads` opt-out, and only in a minor
   release with a migration note.
4. **Signed tags + CI verification.** `docs/release/signed-tags-plan.md`
   has the design (SSH `allowed_signers`); the release-pin gate already
   supports `GATE_TAG_SIGN_CHECK=1`. Enabling it turns "annotated but
   unsigned" from a known limitation into a gate.

## P2 — regression coverage that would have caught the anomaly

5. **Promote the throwaway sweep into the repo.** Today it lives outside
   the tree (`/data/v027-run/cli-anomaly-sweep.py`). Add
   `scripts/cli-anomaly-sweep.py` (or a Go test `internal/cli/robustness_test.go`)
   covering: unknown flags/commands, empty/unreadable/directory/symlink-loop
   paths, YAML bombs (`!!python/...`, anchors, NUL/BOM/CRLF, deep nesting),
   numeric over/underflow, unicode, and **non-regular files**, asserting:
   exit ∈ {0,1,2}, no panic string, no hang (per-case timeout), and **no
   `api_token` value in any output**. Wire it as a CI job — it is ~16 s.
6. **Upstream the config fuzz targets.** `FuzzConfigQueueRanges`,
   `FuzzConfigPorts`, `FuzzConfigMisc` were written as throwaway harnesses
   (they write the fuzzed text to `t.TempDir()` and call the real `Load()`);
   commit them so `go test -fuzz` covers the config layer, and add a short
   `-fuzztime=30s` CI job for all five fuzzers (smoke, not a soak).
7. **`internal/store` test gap.** The package had **no tests** before
   `store_test.go` (added with PR #102). Add: version validation, write
   atomicity (tmp+rename, no clobber), `active.json` corruption handling,
   `List`/`NextVersion` bounds, and the hostile-store (FIFO) case from (1).
8. **Staticcheck clean-up.** Six findings are test-only
   (`server_test.go` U1000/SA4006 ×2, `overflow_test.go` SA4006,
   `gate_shell_test.go` SA1011, `netlink_test.go` SA4016). Fix them and
   add `staticcheck` to CI so the baseline stays empty — otherwise a real
   finding hides in the noise.

## P3 — platform & operations

9. **arm64 reality check.** arm64 is only verified under QEMU TCG. Options:
   run the apk smoke on a real arm64 board in CI (self-hosted runner), or
   publish an explicit "arm64: TCG-verified only" banner in the release
   notes (already there) **and** add a cross-compile + `go test -race`
   arm64 job (fast, catches endian/alignment bugs).
10. **Scope the CLI signing key (F-02).** `GH_RELEASE_SIGNING_KEY` is
    repo-level; the apk key is already environment-scoped with reviewers.
    Move the CLI key into `signing` and scope the `release.yml` sign step
    (issue draft `/data/v027/f02-signing-env-issue.md`).
11. **Release-pipeline ergonomics.** The 20-asset release + DRAFT flow is
    scripted but manual; a `scripts/release-run.sh --tag vX.Y.Z
    --dry-run|--execute` wrapper (tag → pin PR → dual SDK build → gates →
    sign → attach → notes → verify, with per-step authorization prompts)
    would cut a 2-hour run to a review of the plan.
12. **Upgrade e2e in CI.** Issue #57's regression class (upgrade restart)
    is verified per release by hand. Add a scheduled/`workflow_dispatch`
    job: install published v(N-1) apk, upgrade to the built v(N), assert
    continuous probes + queue rebind.
13. **Metrics/observability.** Add counters for policy-reload outcomes
    already partly present (`goshisen_policy_update_total`) — extend with
    reload *rejections* by reason (parse vs compile vs store), and a
    `goshisen_file_read_bytes_total` gauge for the new bounded reader so a
    hit cap/`not a regular file` rejection is visible in Prometheus.

## Suggested v0.2.8 shape

- **Ship:** (1) extension, (2) #103, (5), (6), (7), (8), (13).
- **Ship behind a flag / with migration note:** (3).
- **Ship if the maintainer configures the anchors:** (4), (10).
- **Plan, don't rush:** (9), (11), (12) — each needs infrastructure the
  repo does not have yet (self-hosted arm64, scheduled e2e).

## Out of scope

- LuCI app, IPv6 NAT, multi-queue sharding, eBPF datapath.
- Anything that changes the on-disk policy schema (needs its own
  migration + rollback story).
