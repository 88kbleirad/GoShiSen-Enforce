# Proposal — v0.2.3 packaging/signing release: version alignment

**Status: PROPOSAL — no tag created. Awaiting owner confirmation.**
**Base:** main `4233e2e` (post Prompt 39; CI 43620712710 6/6).

## 1. Decision (owner)

Option (b): new CLI release tag **`v0.2.3`** as a packaging/signing
release. **No runtime change since v0.2.2** (`internal/`+`cmd/` diff
from `c5de6e8` → `4233e2e` is empty). Explicitly stated in the notes.

## 2. Why a new tag (rationale)

- Published releases are immutable: v0.2.2 assets/checksums are not
  modified; the APK anchor, runbook, and verification docs live on main,
  not in the v0.2.2 tag.
- The release tag, the apk package version, and the version the daemon
  reports must agree.

## 3. Version alignment proposal

| Component | Current | Proposed |
| --- | --- | --- |
| CLI release tag | v0.2.2 | **v0.2.3** |
| PKG_VERSION | 0.2.0 | **0.2.3** |
| PKG_RELEASE | 5 | **1** |
| Daemon reported version (ldflags) | `0.2.0` | **`0.2.3`** (== PKG_VERSION) |
| PKG_SOURCE_VERSION (daemon pin) | `c5de6e8` (v0.2.2 tag commit) | **unchanged `c5de6e8`** |
| PKG_MIRROR_HASH | `c5017b88…` | unchanged (same pin) |

### Pin without a self-referential problem

`PKG_SOURCE_VERSION` is the **daemon source SHA**, independent of the
package version strings. For the v0.2.3 release the daemon pin must be an
ancestor of (or equal to) the release commit with a verified mirror hash.
Two safe options:

- **A (recommended):** keep the pin at `c5de6e8` (the v0.2.2 tag
  commit = the runtime that v0.2.3 ships; the v0.2.3 tag commit itself
  is a descendant that adds packaging/signing/docs). The release commit
  then *contains* the pin pointing at its ancestor — no self-reference,
  mirror hash unchanged (`c5017b88…`, SDK-verified).
- B: point the pin at the v0.2.3 tag commit (the commit carrying the
  recipe). That creates a **self-referential** cycle (the tag commit's
  tree contains a Makefile that pins the tag commit) and forces a *new*
  mirror-hash derivation for a tree that includes the new PKG_VERSION;
  the daemon source is identical either way. Not recommended.

**Recommendation: Option A** — pin stays `c5de6e8` (v0.2.2 source),
`PKG_VERSION=0.2.3`, `PKG_RELEASE=1`.

### Runtime byte-identity with v0.2.2

- The daemon binary inside the proposed `0.2.3-r1` apk would be built
  from the **same source pin `c5de6e8`** with the **same Go toolchain /
  SDK** — i.e., **byte-identical runtime** to what v0.2.2 shipped (same
  source + same compiler/flags). Only the version *stamp* changes
  (`0.2.3` vs `0.2.0`) because `PKG_VERSION` feeds the ldflags; the
  stamp is data, not code. The package files (recipe) are the same r5
  recipe.
- Evidence frame: rebuild of the same pin in Prompt 30/32 produced
  byte-identical apks; the only variable here is the version string in
  the ldflags.

### apk upgrade ordering (verified with apk-tools 3.0.5)

| Order | Result |
| --- | --- |
| `0.2.0-r2` (published v0.2.0) < `0.2.0-r5` | `0.2.0-r5` newer |
| `0.2.0-r5` < `0.2.3-r1` | `0.2.3-r1` newer |
| `0.2.0-r5` < `0.2.3` | `0.2.3` newer |
| `0.2.3-r1` > `0.2.3` | release suffix newer |

So a clean **upgrade path exists** from the published `0.2.0-r2` and
from `0.2.0-r5` to `0.2.3-r1`. (Downgrade r5→r4 caveat from Prompt 30
is unchanged.)

## 4. Remaining mismatches (documented in notes if accepted)

- The v0.2.2 *CLI* artifacts (tarballs) report `v0.2.2`; the v0.2.3
  CLI artifacts will report `v0.2.3`; the daemon inside the *package*
  reports `0.2.3` (package version, not CLI version). The package version
  is intentionally independent of the CLI release (existing policy); for
  this release they are aligned (both 0.2.3), which is the point.
- The upstream "version" in the apk is `0.2.3-r1`; the daemon binary's
  `--version` prints `0.2.3` (no `-r1` suffix). This is expected
  (package release suffix is an apk concept, not a binary stamp).

## 5. Release history statement (for the notes)

| Tag | Published | Assets | APK present |
| --- | --- | --- | --- |
| v0.2.0 | 2026-11-12 | checksums.txt, tarballs amd64/arm64, sbom.spdx.json, **goshisen-0.2.0-r2.apk** | **yes — unsigned** |
| v0.2.1 | 2026-11-13 | checksums.txt, tarballs amd64/arm64, sbom.spdx.json | no |
| v0.2.2 | 2026-11-14 | 19 signed CLI artifacts (tarballs/checksums/SBOM + sigs + provenance + public_key.pem) | no |
| v0.2.3 (proposed) | draft | tarballs + checksums + SBOM + **signed apk 0.2.3-r1** + provenance + sigs + public_key.pem | yes — signed |

CLI release key (`GH_RELEASE_SIGNING_KEY`) and the APK key
(`GH_RELEASE_APK_SIGNING_KEY` / anchor `goshisen-apk.pub.pem`,
fingerprint `542bb7ae…`) are distinct; never interchanged.
