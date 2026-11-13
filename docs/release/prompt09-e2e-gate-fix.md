# Prompt 09 — OpenWrt e2e reload gate fix (report)

**Verdict: DONE** (fix verified live + CI green; PR #20 open for review; no tag/release touched)

## Root cause (traced from the failing CI log)

The gate counted publish events with:
`grep -Ec 'policy (reloaded|published)" .*rules=N'` inside the VM. This ERE:

1. **No numeric boundary** — `rules=1` matched `rules=10` (prefix match), so a
   publish of a 10-rule policy satisfied a rules=1 gate.
2. **Source-blind** — `policy published ... source=api` (REST publish) counted as a
   SIGHUP reload success.
3. **Order-coupled within the line** — `.*` only tolerated fields between `"` and
   `rules=`, not reordering; a wrapped/truncated line could hide a successful publish.
4. The host `vm()` helper used `tr -cd '\11\12\15\40-\176\200-\377'`, which **kept**
   every byte ≥ 0x80. Invalid UTF-8 from the raw QEMU serial (e.g. `0xff 0xfe
   0xfd` seen in CI) survived into transcripts; `'${now:-0}'` then silently turned a
   garbled read into 0 → the gate timed out even though the daemon had published.

## Fix (scripts/openwrt-e2e.sh)

- Gate ERE now requires the daemon's SIGHUP publish, with a full-token boundary:
  `policy (reloaded|published)" .*source=sighup .*rules=N([^0-9]|$)`
- `vm()` sanitizes by **replacement** (`tr -c '[:print:]\n\r\t' '?'`), so every
  non-printable byte becomes a stable `?` instead of a stray high byte; CR stripped.
- before/after count delta retained → an old event never satisfies the *current*
  SIGHUP.

## Regression tests (new; run in the plain CI `build, vet, test` job — no VM)

- `tests/openwrt_integration/gate_harness_test.go` — regex-level: clean publish,
  reload-requested ignored, rules boundary (10≠1), invalid UTF-8, old events.
- `tests/openwrt_integration/gate_shell_test.go` — drives the **exact** grep(1)
  invocation used inside the VM plus the **exact** `tr -c` sanitizer pipeline,
  including the 0xff/0xfe/0xfd + NUL bytes from the failing CI transcript.

All 13 gate tests PASS; `go test ./... -count=1` PASS; `make check` PASS.

## Live verification (instabox sandbox, OpenWrt 25.12.6, QEMU TCG, dual-bridge lab)

First run (gate ERE fix, pre-sanitizer) and second run (both fixes) both PASS:

```
[e2e] DROP control PASS: curl exit=28, daemon drop verdicts +7, hook +7, queue drops +0
[e2e] ACCEPT control PASS: curl exit=0, HTTP 200, marker body, accept verdicts +11 (new +1, established +10), hook +11
[e2e] PASS: DROP blocks the request, ACCEPT permits it, all counters agree; kernel queue drops stayed flat.
```

Evidence: `/data/phase16-openwrt-compat-artifacts/e2e-gate-fix.txt`
(sha256 `c9d9f96c…`).

## CI (PR #20, head `c92ea5e`)

- `build, vet, test` — pass (includes the 13 new gate tests)
- `package + verify release archives` — pass
- `integration (real netns lab, privileged)` — pass
- `openwrt e2e (QEMU lab, privileged)` — pass (confirmed on the PR head)

Run IDs (head `c92ea5e`): 43433459371 / 43433469580 (push + pull_request).
Final docs-head `4a72717` (this report): 43434133060 / 43434143326 — both success,
4/4 jobs, 0 skips.

## No release changes

Tag `v0.2.1` and its assets are untouched. Fix is isolated to the harness
(`scripts/openwrt-e2e.sh`) + new tests; it does not alter the daemon or the
published release.
