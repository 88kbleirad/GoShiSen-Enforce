# F-02 — CLI release signing key moved behind a protected environment

> **STATUS 2026-11-23: REINSTATED — the gate is `environment: signing`.**
> `release.yml`'s `sign` job now declares `environment: signing` (the same
> environment `apk-sign.yml` uses: required reviewer `88kbleirad`, deployment
> policies `main` branch + `v*` tag), so a tag release — and the dry run —
> pauses for approval before the key is resolved. **Current state and the exact
> remaining steps are in §0.** Sections §1 onward stay as the record of the
> earlier `release-cli` attempt (nine dry runs, all fail-closed, leak scans
> clean) and of its withdrawal; wherever they say `release-cli`, they describe
> that withdrawn design, not today's. Deleting an environment also deletes its
> secrets and protection rules, and a workflow that declares a missing
> environment makes GitHub create it WITHOUT protection rules (the job then
> falls back to the repository-level secret) — which is why the gate is
> declared only for an environment that exists with rules.

Issue: [#100](https://github.com/88kbleirad/GoShiSen-Enforce/issues/100).
Scope: the **CLI release** signing key `GH_RELEASE_SIGNING_KEY`
(Ed25519, production fingerprint
`24215e016621925215dec0ad6ea14ac74253a4e97249a973e89d649a2217cca0`,
key ID `f955ede984245d64`). The APK signing key is a different key and a
different environment (`signing`) — see
`docs/release/apk-signing-key-custody.md`.

## 0. Current state (2026-11-23) — gate reinstated in `signing`

> **2026-11-28 — the CLI anchor was rotated.** The records in this section
> describe the pre-rotation state (key `24215e01...`). The committed anchor and
> every fingerprint pin now carry the new key `e9b4bf11...`
> (key ID `bdda690b97a4f49e`), the retired anchor stays committed as
> `keys/goshisen-release-2026-11-13.pub.pem`, and the environment secret in
> `signing` holds the new key. Full record incl. the repository-level copy
> removal and the dry-run proof: `docs/release/release-key-rotation-2026-11-28.md`.

| Item | State |
| --- | --- |
| `release.yml` `sign` job | `environment: signing`; only `sign` may reference the key, only `sign` may write |
| Environment `signing` | required reviewer `88kbleirad` (`can_admins_bypass: true`, `prevent_self_review: false` — so the approval is deliberate confirmation, not two-person review; see §0.3); deployment policies `main` (branch) + `v*` (**tag**), so tag releases pass the policy check |
| Environment secret in `signing` | `GH_RELEASE_APK_SIGNING_KEY` only — **`GH_RELEASE_SIGNING_KEY` is not there yet** |
| Repository-level `GH_RELEASE_SIGNING_KEY` | still present; it **is** the production CLI key (run `45001752175` signed and verified against the anchor). Remove it only after a green dry run proves the environment scope |
| Environment `release-cli` | stray leftover: no protection rules, no policies, no secrets. Delete it in Settings → Environments; nothing declares it, and re-declaring it would let GitHub auto-create it unprotected |
| F-02 guard | rule `environment-scoped` (the `sign` job must declare `environment: signing`) + negative fixture `bad-sign-no-environment.yml`; `secret-scope` still confines the key to `sign` in every scanned workflow |

### 0.1 Ordering (blocked on the key's value)

GitHub never returns a secret value, so the environment secret cannot be
copied out of the repository scope. The value must come from the offline
backup (or a rotation):

```sh
# 1. verify the backup offline first — prints only size/header/fingerprint
bash scripts/check-backup-key.sh --key /path/to/cli-key.pem     # expect: MATCH + exit 0
# 2. store it in the environment (the value is never echoed)
gh secret set GH_RELEASE_SIGNING_KEY --env signing \
  --repo 88kbleirad/GoShiSen-Enforce < /path/to/cli-key.pem
# 3. names only: the environment must list it, the repository must still list it
gh api repos/88kbleirad/GoShiSen-Enforce/environments/signing/secrets --jq '.secrets[].name'
gh secret list --repo 88kbleirad/GoShiSen-Enforce
# 4. dry run (needs the required-reviewer approval)
gh workflow run release.yml --ref main -f dry_run=true
# 5. after a green run: delete ONLY the repository-level copy (§3), then dispatch
#    a second dry run — its success is the proof the key resolves from the
#    environment scope. Record both run IDs + a leak scan in issue #100.
```

If the backup no longer exists, rotate: `openssl genpkey -algorithm ed25519`,
store the new key in the environment secret, and replace
`keys/goshisen-release.pub.pem` in a dedicated PR — and, in the same PR, the two
**hardcoded** copies of the expected fingerprint (keep the old anchor committed
so past releases still verify):

| Where | Literal (before → after the 2026-11-28 rotation) |
| --- | --- |
| `.github/workflows/release.yml` (the `sign` job's anchor gate, `expected=`) | `24215e01...` → `e9b4bf11...` |
| `scripts/check-backup-key.sh` (`EXPECTED=`, the offline check's default) | `24215e01...` → `e9b4bf11...` |
| `tests/package/production_signing_test.go` (`wantPubFingerprint` / `wantKeyID`) | `24215e01...` / `f955ede984245d64` → `e9b4bf11...` / `bdda690b97a4f49e` |
| `tests/ci/release-workflow-guard-test.sh` (the dry-run output must print the anchor fingerprint) | `24215e01...` → `e9b4bf11...` |

**Corrected 2026-11-28:** this table previously listed only the two literals
(workflow + offline check). Two more copies live in the test fixtures above, and
they are hard pins too — a rotation that misses them fails CI
(`production_signing` unit test and `release-workflow-guard`) even though the
workflow itself is correct. A full rotation = all four sites + docs.

There is **no** `keys/release-anchor-fingerprint.txt` on `main` — that name does
not exist in the repository, and the fingerprint has no single source file. The
workflow literal is authoritative (it fails closed on mismatch before the key is
touched); the script literal is the offline check's default and must be updated
with it, or `check-backup-key.sh` will reject the rotated key as "not the
production CLI key". An earlier revision of this section (and the unmerged
`feat/f02-anchor-fingerprint-source`) proposed such a single-source file; it was
never merged, so treat both literals as the source of truth.

### 0.2 Exposure note — sharing `signing` with the APK workflow

Environment secrets are readable by **every** job that declares the
environment. `apk-sign.yml`'s `sign` job declares `environment: signing` and
checks out + builds the repository before it signs, so once
`GH_RELEASE_SIGNING_KEY` lives there that job can read it too (and
release.yml's `sign` job can read `GH_RELEASE_APK_SIGNING_KEY`). Both
workflows gate on the same required reviewer, so this is a deliberate
trade-off (one environment to manage) rather than a slip; a dedicated
environment — as the withdrawn `release-cli` was — keeps the two keys apart.
`can_admins_bypass` is `true` on `signing`: a repository admin can bypass the
reviewer. Keep the admin set minimal.

### 0.3 What the approval gate does and does not guarantee

The section title ("protected environment") can read as stronger than what is
configured, so state both properties plainly. Both come from the `signing`
environment's settings (Settings → Environments → `signing`; alternatively
`gh api repos/88kbleirad/GoShiSen-Enforce/environments/signing`, names and
booleans only) as last inspected on **2026-11-23** — the API needs a
credential, so this is a recorded observation, not something a reader can
re-verify from an unauthenticated checkout. Re-read it before relying on it.

**1. Secret-scope resolution is not fail-closed — an environment declaration
does not pin the key's provenance.** GitHub resolves an environment secret in
preference to a same-named repository secret, but when the environment does
*not* define the name, the job silently falls back to the repository-level
secret. So while `GH_RELEASE_SIGNING_KEY` exists at repository scope (it does)
and is absent from `signing` (it is, §0), a tag run's `sign` job resolves the
**repository** copy after the approval wait: the approval is real, but the key
used is the unscoped one. This is the same resolution order that produced the
demonstrated fail-open on 2026-11-22 (§1b: an environment with no secret fell
back to the repository copy and signed without any approval). The gate only
becomes fail-closed for the key's provenance once the environment secret is set
*and* the repository-level copy is deleted (§3); the second dry run is the proof,
and until then the repository-level copy must be treated as the effective key.

**2. The approval is a deliberate-confirmation step, not two-person control.**
Environment `signing` has required reviewer `88kbleirad` with
`prevent_self_review: false` and `can_admins_bypass: true`. Consequences:

- the account that pushed the tag (or dispatched the run) may approve its own
  deployment, so there is no independent second party in the loop;
- a repository admin can bypass the reviewer entirely.

What the gate therefore guarantees: nothing is signed without a human decision
on that specific run, and an unattended tag push cannot publish. What it does
not guarantee: a second pair of eyes, or that a compromised maintainer account
cannot self-approve. Treat it as a deliberate-click speed bump; keep `main`
protected (review-required) and the admin set minimal if independence matters.

## 1. State at handover (2026-11-22)

Read-only API inspection (`gh api`, names only — no value was ever read):

| Item | State |
| --- | --- |
| Environment `release-cli` | exists; required reviewer: `88kbleirad`; deployment policies: `main` (**type: branch**), `v*` (**type: branch** — see the action item below) |
| Environment secret in `release-cli` | `GH_RELEASE_SIGNING_KEY` (name only) |
| Repository-level secret | `GH_RELEASE_SIGNING_KEY` — **still present**, removed by the maintainer **after** the dry run passes |
| `apk-sign.yml` / environment `signing` | untouched (`GH_RELEASE_APK_SIGNING_KEY`); its policies are `main` (branch) + `v*` (**type: tag**) |
| Offline private-key backup | held by the maintainer |

**Maintainer action required before the next tag release (not a
workflow fix — settings only):** GitHub matches deployment policies per
ref type ("Name patterns must be configured for branches or tags
individually", *Managing environments for deployment*), so a `v*`
policy of type **branch** does not authorize a `refs/tags/vX.Y.Z`
deployment. `release-cli` currently has `v*` as a branch policy, while
the APK environment correctly has `v*` as a **tag** policy. Real
releases run on tags, so until the `v*` policy is re-added as type
**tag** (Settings → Environments → `release-cli` → Deployment branches
and tags), a tag-triggered `release.yml` run will be blocked at the
environment step — fail-closed, but it will not create the draft. The
dry run on `main` is unaffected.

GitHub resolves environment secrets in preference to same-named
repository secrets. So the moment `release.yml`'s `sign` job declares
`environment: release-cli`, the job uses the environment copy; the
repository-level copy is redundant for this workflow and is only a
residual exposure (any other workflow or job in the repo could still
read it).

## 1b. Resolution (2026-11-22, verified): the repository-level secret is the correct key

With the environment gate withdrawn, a `workflow_dispatch` dry run on `main`
(head `fb7aed5`) succeeded — run **45001752175**:

```
OK: private key matches public key keys/goshisen-release.pub.pem
VERIFY-PASS: all artifacts verified with keys/goshisen-release.pub.pem
OK: release artifacts signed and verified (key removed)
OK: dry-run signature verified (public key fingerprint 24215e01...)
```

leak scan: 0 PEM private-key blocks, `RELEASE_SIGN_KEY: ***` masked — clean.

So the **repository-level** `GH_RELEASE_SIGNING_KEY` is the production CLI key
(it matches the committed anchor), and the *environment* copy that was pasted
into `release-cli` was the wrong/mangled value all along — that, not the key
backup, was the blocker for every failed dry run.

**Fail-open demonstrated (why the gate had to be withdrawn rather than left
declared):** dispatching that run auto-created `release-cli` with **no
protection rules** (`rules: []`) and — because the environment holds no secret —
the job fell back to the repository-level secret and signed **without any
approval**. A tag release in that configuration would have published a signed
draft unattended. After this withdrawal PR merges, `release.yml` no longer
references the environment, so it cannot be auto-created by the workflow; the
stray environment should still be deleted in repository settings.

## 2. Why the workflow is shaped this way

`validate` → `build` → `sign`:

* `build` never sees the key and cannot write to the repository
  (`permissions: contents: read`), so a compromised build step (or a
  poisoned tag name reaching a shell) cannot spend the signing key.
* `sign` runs under `release-cli`, so the run pauses for the required
  reviewer's approval before the key is resolved, and only a ref allowed
  by the environment's deployment policy (main, `v*`) can get that far.
* The key is passed to exactly one step, through that step's `env`, and
  never interpolated into a shell body (`scripts/release-sign.sh` fails
  closed on a missing key, writes it only to the runner temp dir, and
  removes it via `trap`).
* Before signing, the job re-verifies that every downloaded asset matches
  the build job's `checksums.txt` and that
  `keys/goshisen-release.pub.pem` has the production fingerprint
  `24215e01…`, so neither a tampered artifact nor a swapped anchor can be
  signed.
* `tests/ci/release-workflow-guard-test.sh` (CI job
  `release-workflow-guard (F-02)`) fails the PR if any of that structure
  regresses.

## 2b. Checking a backup on a personal machine (offline, value-safe)

Before re-setting the environment secret, verify the file you are about to
paste with the committed anchor — on your own machine, no network, no CI:

```sh
# prints size/header/public fingerprint only; never the key value
bash scripts/check-backup-key.sh --key /path/to/cli-key.pem
# expect: MATCH: this is the production CLI release key (24215e01...)
#         (exit 0)
```

Exit codes: `0` = key matches the production CLI anchor — safe to set;
`1` = readable key but the wrong one (it will name which committed anchor it
belongs to, e.g. `goshisen-apk.pub.pem`); `2` = usage/missing; `3` = not a
readable PEM private key (quoted/escaped/corrupted/encrypted) with hints.
`--expect <sha256>` checks an explicit fingerprint (useful after rotation).
Regression-covered by `tests/ci/check-backup-key-test.sh` (CI job
`check-backup-key (F-02)`).

## 3. Removing the repository-level secret (maintainer, after the dry run passed)

Prerequisite: the F-02 dry run (workflow_dispatch on `main`,
`dry_run: true`) has succeeded and its run ID is recorded in issue #100.

**Note on the first dry run (2026-11-22, run `44981019938`):** it was
approved and reached signing, but **failed fail-closed** before anything
was signed: `ERROR: cannot derive public key from
/home/runner/work/_temp/goshisen-release-key.pem` — i.e. the
`release-cli` **environment copy** of `GH_RELEASE_SIGNING_KEY` is not a
parseable PEM private key (empty, base64-encoded, public key, or other).
The repository-level copy is unaffected (the v0.2.7 tag release used it
successfully). `scripts/release-sign.sh` now diagnoses the secret's
*shape* (length + category only, never the value) so a re-run tells us
exactly what is wrong. The maintainer must re-set the environment secret
from the offline backup before the dry run is re-run; do **not** copy
the repository secret's value through any log/artifact in the process.

Also confirm (names only) that the environment secret is present with the
same name:

```sh
# 1. Confirm the environment copy exists (names only; never values).
gh api repos/88kbleirad/GoShiSen-Enforce/environments/signing/secrets \
  --jq '.secrets[].name'
# expect: GH_RELEASE_SIGNING_KEY

# 2. Confirm both scopes currently list the name.
gh secret list --repo 88kbleirad/GoShiSen-Enforce
# expect: GH_RELEASE_SIGNING_KEY

# 3. Delete ONLY the repository-level copy.
gh secret delete GH_RELEASE_SIGNING_KEY --repo 88kbleirad/GoShiSen-Enforce

# 4. Verify (again names only): repo level must no longer list it, the
#    environment must still list it.
gh secret list --repo 88kbleirad/GoShiSen-Enforce          # expect: no GH_RELEASE_SIGNING_KEY
gh api repos/88kbleirad/GoShiSen-Enforce/environments/signing/secrets \
  --jq '.secrets[].name'                                    # expect: GH_RELEASE_SIGNING_KEY
```

Do not delete the environment secret, and never print or export a value.
After step 3, `release.yml` remains fully functional: the `sign` job
resolves the key from the environment scope.

## 4. Rollback if the next real release fails

Symptom → cause → action:

| Symptom | Likely cause | Action |
| --- | --- | --- |
| The run is waiting on the environment | the required-reviewer approval is pending (by design) | approve the deployment; nothing to roll back |
| `Environment signing is not allowed to deploy to refs/tags/vX.Y.Z` | the environment's deployment policy does not allow that ref | fix the policy in repository settings (add the `v*` tag rule); the workflow cannot work around it |
| `FAIL: RELEASE_SIGN_KEY (GH_RELEASE_SIGNING_KEY) is missing` | the environment secret was missing/renamed | re-create the environment secret from the offline backup — **the value must not be pasted anywhere else** |
| `FAIL: RELEASE_SIGN_KEY is not a PEM private key (N chars; …)` / `holds a PUBLIC key PEM` | the stored value is not the raw private-key PEM (base64-encoded, public key, bare body, encrypted or trimmed) — **this is what the first dry run (run `44981019938`) hit** | re-set the environment secret from the offline backup as the verbatim PEM text (copy from the API-free local file, never through a log/artifact); re-run the dry run |
| `WARN: repaired paste damage …` then normal signing | the stored value had paste damage (surrounding quotes, literal `\n` escapes, CRLF, stray leading/trailing whitespace) | none — the key parses after the repair, and it still has to match the anchor before signing. Re-set the secret from the exact file contents next maintenance window |
| `FAIL: RELEASE_SIGN_KEY has a PEM private-key header but openssl cannot read it` + `metadata: N chars, L line(s), CR=C, openssl=parse-failed` | a PEM private-key header is present, openssl cannot load it, and the safe repairs (quotes / literal `\n` / CRLF / whitespace) did not help — truncated body, corrupted base64, or an unsupported format. **This is what dry runs `44989394642` (attempts 1–2) and `44991076551` hit** | use the metadata to pick the cause: `L=1` ⇒ the value is one line (the PEM was collapsed; re-set from the file), `CR>0` ⇒ CRLF damage (a repair should have handled it), plausible length + still failing ⇒ corrupted/unsupported ⇒ re-set from the offline backup; treat a suspect backup as revocation + rotation |
| `FAIL: RELEASE_SIGN_KEY is a PASSPHRASE-ENCRYPTED key; this path cannot unlock it` | the secret holds `-----BEGIN ENCRYPTED PRIVATE KEY-----` | export the key **unencrypted** for this secret (the runner has no passphrase), or treat as revocation + rotation |
| `metadata: 121 chars, 2 line(s), CR=0, openssl=parse-failed` (the shape of dry run `44992807857`) | the value is a valid PEM **wrapped in quotes** (plus a stray trailing space) — e.g. pasted as `"$(cat key.pem)"` or from a JSON/`gh secret set` echo. Reproduced locally: canonical Ed25519 PEM = 119 chars / 3 lines / parses; quoted + trailing space = **121 chars / 2 lines / fails** | re-set the secret unquoted (or let the built-in repair handle it — it strips quotes/whitespace/escapes and re-validates with openssl before signing) |
| same fingerprint, and the repair still fails | the wrapping quotes may be **mismatched** (`"…'` or `'…"`, with or without a trailing space) — the surviving shapes after the first repair merged | fixed in the release path: quotes are stripped per end (no matching requirement) and, if the line structure was collapsed, the PEM is reconstructed from the fence + base64 payload; both still require openssl to parse *and* the anchor to match before signing |
| `ERROR: cannot derive public key from …` | the same class reached before the explicit parse probe existed (releases before this change) | re-set the environment secret from the offline backup as the file's exact contents |
| `FAIL: trust anchor fingerprint mismatch` | the committed anchor changed | restore `keys/goshisen-release.pub.pem` (fingerprint `24215e01...`) |
| `ERROR: key mismatch: … does not match …` (+ `FAIL: GH_RELEASE_SIGNING_KEY holds a key that belongs to: <anchor>`) | the environment secret holds a **different** key than the committed anchor (typically the APK signing key pasted by mistake) | read the named anchor: if it is `goshisen-apk.pub.pem`, the APK key was pasted into the CLI secret — re-set from the CLI offline backup; if it names nothing, the backup/secret is not the CLI key. Rotation of the anchor itself needs a dedicated PR |
| `VERIFY-FAIL: provenance-mismatch` / `stale-provenance` | the artifact did not come from this tag's build | re-run the workflow; the sign job's checksum gate should have caught a tampered asset first — investigate before re-running |
| Anything else, and the release must ship now | — | roll back to the pre-F-02 workflow: `git revert <F-02 merge commit>` (restores the single-job workflow that reads the repo-level secret), then re-add the repo-level secret from the offline backup with `gh secret set GH_RELEASE_SIGNING_KEY --repo …`. Note: re-adding the repo secret **alone** does not restore old behaviour while the environment secret exists — environment scope wins; the workflow revert is what restores it. |

Rollback is a temporary measure: re-open the F-02 issue with the failure
output, because the repo-level secret is unscoped and is the exposure
this change removed.

## 5. Post-first-release check (after the next `v*` tag)

Run once, after the first real release that goes through the new
workflow, and record the result in issue #100:

- [ ] `gh run view <run-id> --json jobs` shows `validate` → `build` →
      `sign`, with `sign` gated on the `signing` environment.
- [ ] The `sign` job's log shows the approval wait and then
      `OK: trust anchor fingerprint verified 24215e01…`, `OK: downloaded
      assets match build checksums`, `OK: release artifacts signed and
      verified (key removed)`.
- [ ] `gh secret list` no longer lists `GH_RELEASE_SIGNING_KEY` at
      repository level, and the environment listing still does.
- [ ] The created release is a **draft** (`gh release view <tag>
      --json isDraft`) and its assets verify:
      `scripts/verify-signatures.sh --dist <download-dir>
      --public-key keys/goshisen-release.pub.pem --require-sign`.
- [ ] The `release-workflow-guard (F-02)` job ran green on the commit
      that was tagged.
- [ ] Rotate/retire check: no workflow log, artifact or release asset
      contains private-key material (search the run log for
      `PRIVATE KEY` / base64 key bodies).

## 6. Residual risks

- Any **other** workflow that later references
  `secrets.GH_RELEASE_SIGNING_KEY` outside the `sign` job is caught by
  the F-02 guard on PRs — but the guard only covers `.github/workflows`;
  a new workflow file added after merge is checked on the next PR run.
- Repository-level secrets are readable by any job of any workflow in
  the repository, so removal (step 3) — not the environment alone — is
  what ends the exposure.
- Contributors with **write** access can push branches and open PRs;
  environment approval still gates signing, and `main` should keep
  requiring review + the release-path status checks.
- The environment's `can_admins_bypass` is `true` and `prevent_self_review` is
  `false`: a repository admin can bypass the required reviewer, and the trigger
  account can approve its own deployment. The approval is deliberate
  confirmation, not two-person control — see §0.3. Keep the admin set small.
- `apk-sign.yml` / environment `signing` are out of scope here and were
  not modified.

## 7. History

| Date | Event | Reference |
| --- | --- | --- |
| 2026-11-21 | issue #100 opened (ordering plan; no secret, workflow or setting changed) | issue #100 |
| 2026-11-22 | maintainer creates environment `release-cli` (required reviewer, `main` + `v*`) and adds the environment secret | issue #100 |
| 2026-11-22 | workflow PR: `release.yml` split into validate/build/sign, `sign` under `release-cli`, dry-run path added, F-02 guard + fixtures added to CI | this document, PR link in issue #100 |
| 2026-11-22 | first dry run (run `44981019938`) dispatched + approved; **failed fail-closed before signing**: `cannot derive public key` ⇒ the environment secret copy is not a PEM private key; `release-sign.sh` now diagnoses secret shape (never value); negative non-main dispatch rejected by the environment policy (run `44981062376`) | issue #100, `docs/release/f02-environment-migration.md` |
| 2026-11-22 | dry runs `44985559779` (not a PEM private key — 67 chars, no header), `44988649004` (valid PEM, **wrong key**: `key mismatch`), `44989394642` (PEM header present but **openssl cannot read it**, attempts 1–2) — all fail-closed before signing; diagnostics extended accordingly (secret shape; named anchor on mismatch; value-safe file metadata) | issue #100, `docs/release/f02-recheck-evidence.md` |
| 2026-11-22 | **WITHDRAWN**: the maintainer deleted environment `release-cli` (environments now: `github-pages`, `signing`) and the `environment: release-cli` gate was removed from release.yml; the key is repository-scoped again and signing is not approval-gated (only `sign` may reference the key, per the F-02 guard). Reinstatement steps are in the §2c banner at the top | issue #100, PR for the removal |
| 2026-11-22 | dry run `44991076551` failed the same *unreadable PEM* class ⇒ `release-sign.sh` now **repairs common paste damage** (surrounding quotes, literal `\n` escapes, CRLF, stray whitespace) with a value-safe `WARN: repaired paste damage …` notice, still gated by openssl parse + anchor match; a passphrase-encrypted key is refused with its own message. Guard fixtures cover escaped/quoted PEM (must pass) and the encrypted header (must fail) | issue #100, PR #117 |
| 2026-11-22 | dry run `44992807857` (head `e494f2a`) reported `121 chars, 2 line(s), CR=0, openssl=parse-failed` ⇒ root cause pinned by reproduction: a **quoted** PEM (canonical 119/3 lines vs 121/2 quoted) and the first quote-strip was too strict; the repair is now a repeat-trim + strip-quotes + unescape + drop-CR loop validated by openssl, with a guard fixture for the quoted+trailing-whitespace shape | issue #100 |
| 2026-11-22 | after that repair merged, dry run `44994859221` (head `faf1d66`) reported the identical fingerprint ⇒ the surviving shapes are **mismatched-quote** wrappers (`"...'` ± space) and **collapsed line structure**; the repair now strips quotes per end and reconstructs a canonical PEM from the fence + payload (both openssl-validated, anchor-checked), with fixtures for mismatched quotes and a fully collapsed one-line PEM | issue #100, PR #119 |
| 2026-11-23 | **REINSTATED in a different environment**: `release.yml`'s `sign` job declares `environment: signing` (same environment as `apk-sign.yml`: required reviewer + `main`/`v*` policies), so the approval gate is back without recreating the withdrawn `release-cli`. The F-02 guard gained the `environment-scoped` rule (`sign` must declare `environment: signing`) plus the `bad-sign-no-environment.yml` negative fixture — the #125 regression (a `sign` job that silently loses the gate) now fails CI. Remaining before the repository-level secret can go: the key's value in the environment secret (offline backup or rotation), a green dry run, then the removal in §3 | issue #100, see §0 |
