# F-02 — CLI release signing key moved behind a protected environment

Issue: [#100](https://github.com/88kbleirad/GoShiSen-Enforce/issues/100).
Scope: the **CLI release** signing key `GH_RELEASE_SIGNING_KEY`
(Ed25519, production fingerprint
`24215e016621925215dec0ad6ea14ac74253a4e97249a973e89d649a2217cca0`,
key ID `f955ede984245d64`). The APK signing key is a different key and a
different environment (`signing`) — see
`docs/release/apk-signing-key-custody.md`.

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
gh api repos/88kbleirad/GoShiSen-Enforce/environments/release-cli/secrets \
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
gh api repos/88kbleirad/GoShiSen-Enforce/environments/release-cli/secrets \
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
| `Environment release-cli is not allowed to deploy to refs/tags/vX.Y.Z` | the environment's deployment policy does not allow that ref | fix the policy in repository settings (add the tag rule); the workflow cannot work around it |
| `FAIL: RELEASE_SIGN_KEY (GH_RELEASE_SIGNING_KEY) is missing` | the environment secret was missing/renamed | re-create the environment secret from the offline backup — **the value must not be pasted anywhere else** |
| `FAIL: RELEASE_SIGN_KEY is not a PEM private key (N chars; …)` / `holds a PUBLIC key PEM` | the stored value is not the raw private-key PEM (base64-encoded, public key, bare body, encrypted or trimmed) — **this is what the first dry run (run `44981019938`) hit** | re-set the environment secret from the offline backup as the verbatim PEM text (copy from the API-free local file, never through a log/artifact); re-run the dry run |
| `ERROR: cannot derive public key from …` / `VERIFY-FAIL` after the shape check passes | the value is PEM but malformed, encrypted, or not a key openssl can read | re-set the environment secret from the offline backup; if the backup itself is suspect, treat as revocation + rotation |
| `FAIL: trust anchor fingerprint mismatch` | the committed anchor changed | restore `keys/goshisen-release.pub.pem` (fingerprint `24215e01...`) |
| `ERROR: key mismatch: … does not match …` | the environment secret holds a **different** key than the committed anchor | confirm which key the environment secret should hold (production CLI key); re-set from the backup, or rotate the anchor in a dedicated PR |
| `VERIFY-FAIL: provenance-mismatch` / `stale-provenance` | the artifact did not come from this tag's build | re-run the workflow; the sign job's checksum gate should have caught a tampered asset first — investigate before re-running |
| Anything else, and the release must ship now | — | roll back to the pre-F-02 workflow: `git revert <F-02 merge commit>` (restores the single-job workflow that reads the repo-level secret), then re-add the repo-level secret from the offline backup with `gh secret set GH_RELEASE_SIGNING_KEY --repo …`. Note: re-adding the repo secret **alone** does not restore old behaviour while the environment secret exists — environment scope wins; the workflow revert is what restores it. |

Rollback is a temporary measure: re-open the F-02 issue with the failure
output, because the repo-level secret is unscoped and is the exposure
this change removed.

## 5. Post-first-release check (after the next `v*` tag)

Run once, after the first real release that goes through the new
workflow, and record the result in issue #100:

- [ ] `gh run view <run-id> --json jobs` shows `validate` → `build` →
      `sign`, with `sign` gated on the `release-cli` environment.
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
- The environment's `can_admins_bypass` is `true`: a repository admin can
  bypass the required reviewer. Keep the admin set small.
- `apk-sign.yml` / environment `signing` are out of scope here and were
  not modified.

## 7. History

| Date | Event | Reference |
| --- | --- | --- |
| 2026-11-21 | issue #100 opened (ordering plan; no secret, workflow or setting changed) | issue #100 |
| 2026-11-22 | maintainer creates environment `release-cli` (required reviewer, `main` + `v*`) and adds the environment secret | issue #100 |
| 2026-11-22 | workflow PR: `release.yml` split into validate/build/sign, `sign` under `release-cli`, dry-run path added, F-02 guard + fixtures added to CI | this document, PR link in issue #100 |
| 2026-11-22 | first dry run (run `44981019938`) dispatched + approved; **failed fail-closed before signing**: `cannot derive public key` ⇒ the environment secret copy is not a PEM private key; `release-sign.sh` now diagnoses secret shape (never value); negative non-main dispatch rejected by the environment policy (run `44981062376`) | issue #100, `docs/release/f02-environment-migration.md` |
