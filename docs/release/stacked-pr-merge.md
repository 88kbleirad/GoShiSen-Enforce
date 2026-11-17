# Stacked PR Resolution & Merge Readiness

Date: 2026-11-11; merged 2026-11-12 (record updated 2026-11-15).
Repository: `88kbleirad/GoShiSen-Enforce`.

## Status

**COMPLETE** — both stacked PRs merged and main validated. The pre-merge
audit below (sections A–F) is preserved as-is; section G now records the
executed merge/verification results.

## A. PR dependency (verified)

- Shared ancestor: `1fa98bf6b4860dbf3c117e3cd3f93f0b351a2711` (PR #2
  head; confirmed ancestor of PR #3 via `git merge-base --is-ancestor`).
- `phase9-packaging..phase10-openwrt` = 13 commits, all OpenWrt feature
  work: hook scope + queue config, NFQUEUE CT-glue fallback, image-prep,
  lab scripts, e2e, Go wrapper, CI job, docs/tests.
- PR #3 therefore contains the complete PR #2 changeset.
- Required merge order: **PR #2 first, then PR #3** (do not merge PR #3
  first).

## B. PR #2 — Release Packaging

- Head: `1fa98bf` (5 commits), base `main`, OPEN, MERGEABLE, CLEAN.
- CI all PASS: `42834327626`, `42834337925` (build/vet/test,
  privileged integration, package+archives).
- Reviewed: reproducible CGO-disabled archives (linux/amd64+arm64),
  checksums, SBOM, version stamping, `release.yml` scoped to
  `contents: write` (tag-only), no secrets committed, no release
  published by the workflow.
- Local: `tests/package` PASS; version stamp `0.2.0` verified.
- Verdict: **READY_TO_MERGE** (pending authorization).

## C. PR #3 — OpenWrt Integration

- Head: `4f4ba3e` (18 commits) — 13 over PR #2 + PR #2's 5.
- CI all PASS (final): push `42971442815`, PR `42971449501`
  (build/vet/test, package, privileged integration, **openwrt e2e**).
- OpenWrt e2e log (PR run) verified: image download+checksum
  (`f8538784…`), kmod install, QEMU boot, queue 100 bound, DROP control
  (curl 28, +7, hook +7, 0 queue drops), ACCEPT control (HTTP 200 +
  `GSH-ALLOW-PROOF`, +13 accept [new +1, established +12], hook +13),
  counter identity, policy restored, zero skips.
- Effective diff after PR #2 merges into main = only the OpenWrt work
  (24 files, +2200). No rebase required post-merge (stacked branch);
  verify GitHub mergeability refresh.
- Verdict: **READY_TO_MERGE** (pending authorization).

## D. Final main validation (after both merges — not yet performed)

Requires: build/vet/test, packaging, privileged integration, openwrt
e2e all PASS on the merged main commit; zero mandatory skips.

## E. Security review (unchanged invariants)

- Fail-closed: `NFQA_CFG_F_FAIL_OPEN` never requested (unit-tested).
- CT-glue fallback scoped to `EOPNOTSUPP` only; other netlink errors are
  fatal.
- Missing CT metadata → INVALID → DROP (no bypass).
- OpenWrt fw4 untouched; hook is separate `ip goshisen` table at
  priority filter+1, policy accept, no NAT chains.
- No host firewall flush; loop-device cleanup scoped to owned device.
- No secrets/debug binaries committed. v0.1.0 tag unchanged
  (`83a51b6` → `17490fd`).

## E2. nft queue syntax bug (fixed on this branch)

The hook emitted legacy `queue num N`; nft >= 1.1 rejects it ("Could not
process rule: No such file or directory"), so the table was created
without a queue rule and `hook status` reported `table goshisen exists
but queue rule not found`. Fixed: both scopes now emit `queue to N`
(valid on nft >= 1.0). Verified on the OpenWrt lab (nft 1.1.6): hook
installs with `queue to 100`, NFQUEUE binds. Commit `c95db49`.
Host lab/preflight hooks updated to `queue to` too.

## F. Release status

- v0.1.0 preserved and published.
- Next version recommendation: after merges, v0.2.0 (or
  `v0.2.0-rc.1` prerelease) per repo policy; the `0.2.0-openwrt`
  development string alone is not authorization to publish. No version
  published during this phase.

## G. Remaining actions (authorization required) — EXECUTED

1. Merge PR #2 (`gh pr merge 2 --merge`).
2. Verify main CI (Step E).
3. Re-evaluate PR #3 effective diff + refresh checks.
4. Merge PR #3 (`gh pr merge 3 --merge`).
5. Verify final main CI (Step J), record final main SHA + run IDs.
6. Commit this doc post-merge with the record (if authorized).

## H. Post-merge execution record (2026-11-12)

| Item | Result |
| --- | --- |
| PR #3 merged | `265902f` — "Merge pull request #3 from 88kbleirad/phase10-openwrt" — 2026-11-12T04:36:31Z |
| PR #2 closed | head `1fa98bf` merged into main via PR #3 (PR #3 contained the complete PR #2 changeset); GitHub auto-closed #2 at 2026-11-12T04:36:34Z |
| Merge order deviation | PR #3 merged first; the required order (PR #2 first) was moot because PR #3 already contained PR #2's entire changeset — net main tree identical |
| Final main CI (Step E) | `43155920898` (CI, 265902f) success; `43156372716` (CI, push) success; `43156372764` (release.yml `package+verify+publish`, push) success — all jobs PASS, zero skips |
| Main final SHA | `265902f7a0c908be1effa6292522ff0449ae1f7` |
| Tag | `v0.2.0` → `265902f` (tagged 2026-11-12) |
| Follow-on releases | v0.2.1 (`9ca7133`), v0.2.2 (`c5de6e8`), v0.2.3 (`8b55953` merge; released 2026-11-15) — each with green CI, evidence docs, and release workflow runs |
| Evidence index | rows for phases 9/10/11 documented in `docs/release/evidence-index.md` |

Post-merge verification of the merged main commit (Step D requirements) is
recorded in `docs/release/openwrt-validation.md` (all 4 CI job
classes PASS on main) and in `docs/release/evidence-index.md` §4.
