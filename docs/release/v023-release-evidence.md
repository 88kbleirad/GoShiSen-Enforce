# v0.2.3 release execution evidence

Date: 2026-11-15. Role: release engineer (packaging/signing release).

## A. Pre-merge state

| Item | Value |
| --- | --- |
| main (pre-work) | `f0552f8` (PR #39) |
| PR #40 | ci(apk-sign): production gate — SDK container signing of draft-release apk + strict sha256 |
| PR #40 head | `1efdb4b` (rebased onto main; conflict in `expected_sha256`/`apk_hint` inputs resolved in favour of the branch design) |
| PR #40 CI | push `43675107347` 6/6 SUCCESS, PR `43675103068` |
| PR #40 merge | `8b5595317a1aa6ed1b60c223d8f9e9449865fe01` (merge commit, 2026-11-15T04:09:09Z) |
| main CI on merge | run `43675490193` — 6/6 SUCCESS on `8b55953` |

## B. Mirror hash (dry-run gate)

The v0.2.3 pin requires `PKG_MIRROR_HASH`. Verified in the **official** SDK
container `openwrt/sdk:x86_64-25.12.6` (CI now pins it by digest —
`openwrt/sdk@sha256:a17d6436f7add9207513b6edd0370f5a1aeeceeedf3c9080de8909112892f261`):

- `make package/feeds/goshisen/download V=s` → `1867a2dec7ac45393b2ea075f9016d3988a72d8961dedc1b75e9e766ec79f35c`
- Matches `openwrt/Makefile` exactly.
- The host SDK on this workstation produces `2ed64a1e…` from the same pin
  (tar 1.35 / zstd 1.5.7 vs container tar 1.34 / zstd 1.4.8); the container
  value is canonical, as recorded when the hash was pinned.

## C. Dry-run package build (checklist gate)

- Recipe: `openwrt/Makefile` at `8b55953` (PKG_VERSION 0.2.3, PKG_RELEASE 1,
  PKG_SOURCE_VERSION c5de6e8).
- `make package/feeds/goshisen/compile V=s` in the SDK (official 25.12.6).
- Artifact: `goshisen-0.2.3-r1.apk`
  - apk sha256 `d0a66f461129e89b9eadb97e0a057efa24adaf656799bf9853c3cfe8c7ef12d9`
  - payload daemon sha256 `57aef2cf558d80ed8d51c9e7f746021e101cde7a9427fc6432eaab69735b89c0`
  - `goshisen --version` → `0.2.3`; static x86-64 ELF.
- Second independent compile produced the **same apk sha256**
  (`d0a66f46…`) — build reproducibility confirmed for this commit.
- Evidence: `/data/prompt40-v023-dryrun/dry-run-evidence.md`.

## D. Tag + draft release

- Annotated tag `v0.2.3` → `8b5595317a1aa6ed1b60c223d8f9e9449865fe01`.
- `release.yml` run `43679091427` on the tag → SUCCESS; created **draft**
  release `v0.2.3` with 19 signed CLI assets (tarballs, checksums, SBOM,
  sigs, provenance, public_key.pem).
- The apk is uploaded to the draft as `goshisen-0.2.3-r1.apk`
  (sha256 `d0a66f46…`, verified after download).
- `checksums.txt` is intentionally left as produced by CI (2 signed entries):
  v0.2.2, the previous signed release, does the same, and the file carries a
  detached signature that must stay valid. The apk's hash is recorded here,
  in the gate provenance, and in the release notes.

## E. Production APK signing

| Step | Result |
| --- | --- |
| Dispatch (from tag) `43679509334` | FAILED — SDK container apt repos are stale (404 on curl/jq); no signing attempted (fail-closed) |
| Fix PR #41 | `scripts/fetch-release-asset.py` (python3 + urllib, no apt); adds `release_tag` input |
| Dispatch (from `main`, `release_tag=v0.2.3`) | pending |

## F. Notes / differences to explain

- PR #41 (CI-only: workflow + helper script) lands on main *after* the tag.
  `git diff 8b55953 main` therefore touches `.github/workflows/apk-sign.yml`
  and adds `scripts/fetch-release-asset.py` — no runtime, packaging-recipe, or
  artifact change. This matches the established pattern (v0.2.2's tag is also
  a single CI-green merge commit with later docs/CI PRs on main).
