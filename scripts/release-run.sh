#!/usr/bin/env bash
# GoShiSen-Enforce — release-run wrapper (P3.11, issue #142).
#
# Slice 1 — `--dry-run` only:
#   prints, per step, the exact command lines and the evidence that step
#   must produce, and executes NOTHING (no tag, build, sign, upload or
#   publish; no file written). `--execute` is deliberately not implemented
#   in this slice and exits 3.
#
# The plan is codified from the v0.2.9 run as recorded in
#   docs/release/v0.2.9-notes.md ("Reproduction"), docs/release/v0.2.9-apk-build-recipe.md
#   §9, docs/release/release-pin-gate.md and docs/release-checklist.md
#   §"Release mechanics" / "Post-release verification".
# Where the last run used a driver that is NOT committed (the v0.2.9 VM smoke
# driver), the step says so instead of inventing a command.
#
# Usage:
#   scripts/release-run.sh --tag vX.Y.Z --dry-run [--evidence-dir DIR] [--repo-slug OWNER/NAME]
#
# Exit codes:
#   0  PASS (dry-run plan printed; nothing executed)
#   1  internal error (unexpected)
#   2  usage (missing/conflicting arguments)
#   3  --execute is not implemented in this slice
#   4  tag rejected by scripts/validate-release-tag.sh (S-003)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VALIDATE_TAG="$ROOT/scripts/validate-release-tag.sh"

TAG=""
MODE=""
EVID=""
REPO_SLUG="88kbleirad/GoShiSen-Enforce"

die() { echo "FAIL: $*" >&2; exit 1; }

usage() {
  cat >&2 <<'EOF'
usage: scripts/release-run.sh --tag vX.Y.Z --dry-run [options]

  --tag vX.Y.Z          the release tag (must match ^v<major>.<minor>.<patch>$)
  --dry-run             print every step, its exact commands and its expected
                        evidence; execute nothing (this is the implemented mode)
  --execute             run the steps that need no credential — NOT IMPLEMENTED
                        in this slice (issue #142); exits 3
  --evidence-dir DIR    where --execute would write the per-run evidence file
                        (default: evidence/release-run/<tag>-<UTC timestamp>)
  --repo-slug O/N       GitHub slug used in the printed gh commands
                        (default: 88kbleirad/GoShiSen-Enforce)
  -h, --help            this text
EOF
  exit 2
}

while [ $# -gt 0 ]; do
  case "$1" in
    --tag)          [ $# -ge 2 ] || usage; TAG="$2"; shift 2 ;;
    --tag=*)        TAG="${1#*=}"; shift ;;
    --dry-run)      [ -z "$MODE" ] || [ "$MODE" = dry-run ] || usage; MODE="dry-run"; shift ;;
    --execute)      [ -z "$MODE" ] || [ "$MODE" = execute ] || usage; MODE="execute"; shift ;;
    --evidence-dir) [ $# -ge 2 ] || usage; EVID="$2"; shift 2 ;;
    --evidence-dir=*) EVID="${1#*=}"; shift ;;
    --repo-slug)    [ $# -ge 2 ] || usage; REPO_SLUG="$2"; shift 2 ;;
    --repo-slug=*)  REPO_SLUG="${1#*=}"; shift ;;
    -h|--help)      usage ;;
    *)              echo "unknown argument: $1" >&2; usage ;;
  esac
done

[ -n "$TAG" ]  || { echo "missing --tag" >&2; usage; }
[ -n "$MODE" ] || { echo "one of --dry-run / --execute is required" >&2; usage; }
[ -x "$VALIDATE_TAG" ] || die "missing $VALIDATE_TAG"

# S-003: the tag name reaches shell commands later; reject it before it is used.
bash "$VALIDATE_TAG" "$TAG" || exit 4

VER="${TAG#v}"
UTC="$(date -u +%Y%m%dT%H%M%SZ)"
[ -n "$EVID" ] || EVID="$ROOT/evidence/release-run/$TAG-$UTC"

if [ "$MODE" = "execute" ]; then
  cat >&2 <<EOF
FAIL: --execute is not implemented in this slice (issue #142, slice 1 = --dry-run).

Nothing was run. The plan below is what --execute is meant to drive; every
'maintainer' step would stop with the exact command for you to run:

  scripts/release-run.sh --tag $TAG --dry-run
EOF
  exit 3
fi

# ---------------------------------------------------------------- rendering
STEPS=0
step() { # n title owner mutates gate
  STEPS="$1"
  printf '\n── step %s — %s\n' "$1" "$2"
  printf '   owner:   %s\n' "$3"
  printf '   mutates: %s\n' "$4"
  printf '   gate:    %s\n' "$5"
}
run_block()    { printf '   run:\n';    sed 's/^/     /'; }
expect_block() { printf '   expect:\n'; sed 's/^/     /'; }
note_block()   { printf '   note:\n';   sed 's/^/     /'; }

cat <<EOF
release-run — $TAG (dry-run)
  repo:         $ROOT
  github:       $REPO_SLUG
  evidence:     $EVID (would be created by --execute; nothing written now)
  placeholders: <pin-commit> tag commit · <out> build output dir ·
                <scratch-A|B> build scratch dirs · <run-id> workflow run id ·
                <apk-sha256> sha256 from build-manifest-A.json ·
                <dir> directory holding the 20 release assets
EOF

printf '\npreflight (read-only; --execute refuses to start if any fails)\n'
cat <<'EOF'
     git status --porcelain            # must be empty
     git rev-parse --abbrev-ref HEAD   # must be main
     git fetch origin && git rev-parse main origin/main   # must be equal
     sed -n 's/^PKG_VERSION[[:space:]]*:=?[[:space:]]*//p' openwrt/Makefile | head -1
                                       # must equal the tag without its "v"
EOF

step 1 "Tag-name guard and F-01 pre-tag gate" agent no \
  "the commit to be tagged already carries PKG_VERSION=${VER}, PKG_RELEASE set, and the tree is exactly a commit"
run_block <<EOF
bash scripts/validate-release-tag.sh $TAG
bash scripts/release-pretag-check.sh --tag $TAG
EOF
expect_block <<EOF
OK: release tag '$TAG' matches ^v[0-9]+\\.[0-9]+\\.[0-9]+\$
PASS: pre-tag gate $TAG (PKG_VERSION=${VER}, PKG_RELEASE set)
EOF
note_block <<'EOF'
refusal: either command non-zero -> STOP. No tag is created (this is the
v0.2.6 defect: the tag shipped PKG_VERSION:=0.2.5 and only a post-tag PR
fixed it).
EOF

step 2 "Annotated tag and push" maintainer yes \
  "step 1 green, preflight clean"
run_block <<EOF
git tag -a $TAG -m "GoShiSen-Enforce $TAG"
git push origin $TAG
EOF
expect_block <<EOF
gh run list --workflow release.yml --limit 1 --json databaseId,headSha,status
# one run on the $TAG tag commit; 'validate' then 'build' go green,
# 'sign' pauses on environment 'signing' (required reviewer)
EOF
note_block <<'EOF'
mutating: creates the tag (remote + local). Nothing else in the plan runs
before step 1 passes.
EOF

step 3 "Release workflow: validate -> build -> sign (draft release)" maintainer yes \
  "the sign job is approved in environment 'signing'; the CLI key resolves from the environment secret only"
run_block <<EOF
RUN_ID=\$(gh run list --workflow release.yml --limit 1 --json databaseId --jq '.[0].databaseId')
gh run watch "\$RUN_ID" --exit-status
# approval (maintainer, after checking the pending deployment's environment)
gh api repos/$REPO_SLUG/actions/runs/\$RUN_ID/pending_deployments
gh api -X POST repos/$REPO_SLUG/actions/runs/\$RUN_ID/pending_deployments \\
  -F environment_ids[]=<env-id-from-the-list> -F state=approved \\
  -F comment="release-run $TAG"
EOF
expect_block <<EOF
OK: trust anchor fingerprint verified e9b4bf11...
VERIFY-PASS
gh release view $TAG --json isDraft,assets    # isDraft=true, 19 CLI assets
EOF
note_block <<'EOF'
credential-gated: the wrapper never signs. The job is the only holder of
GH_RELEASE_SIGNING_KEY; the anchor is verified inside the workflow.
EOF

step 4 "Post-tag pin PR (openwrt/Makefile only)" maintainer yes \
  "step 3 produced the draft; the pin commit is a Makefile-only follow-up on main"
run_block <<EOF
# openwrt/Makefile: PKG_VERSION:=${VER}  PKG_RELEASE:=1
#                  PKG_SOURCE_VERSION:=<pin-commit>   (the tag commit)
git switch -c pkg/pin-$TAG main
git commit -am "pkg: pin $TAG tag commit in openwrt Makefile"
git push -u origin pkg/pin-$TAG
gh pr create --base main --title "pkg: pin $TAG tag commit in openwrt Makefile" --body "..."
EOF
expect_block <<EOF
# required: PR CI green, in particular 'release-pretag-gate (F-01)'
git show <pin-commit> --stat          # openwrt/Makefile only (docs allowed)
git ls-remote --tags origin $TAG      # tag object unchanged
EOF
note_block <<'EOF'
A tag cannot contain its own hash, so the pin is the documented post-tag
commit; the gate in step 6 proves source equivalence between pin and tag.
Merge is the maintainer's (repo convention: squash).
EOF

step 5 "apk build A/B (reproducibility)" agent yes \
  "pin commit from step 4 is on main; ~10-30 min per build, ~5 GB scratch each"
run_block <<EOF
scripts/build-apk-release.sh --commit <pin-commit> \\
  --mirror-hash <sha256> --label A --canonical-name \\
  --go-root /data/toolchain/go1.27.4 --out <out> --work <scratch-A>
scripts/build-apk-release.sh --commit <pin-commit> \\
  --mirror-hash <sha256> --label B \\
  --go-root /data/toolchain/go1.27.4 --out <out> --work <scratch-B>
jq -r '.artifacts.apk.sha256, .artifacts.payload.sha256' \\
  <out>/build-manifest-A.json
jq -r '.artifacts.apk.sha256, .artifacts.payload.sha256' \\
  <out>/build-manifest-B.json
EOF
expect_block <<'EOF'
two identical apk sha256 lines and two identical payload sha256 lines
<out>/goshisen-<ver>-r1.apk  <out>/daemon-A.bin
<out>/build-manifest-A.json  <out>/build-manifest-B.json
EOF
note_block <<'EOF'
refusal: any hash differs -> STOP. Nothing is signed, uploaded or published
from a non-reproducible build. The build never publishes or signs.
EOF

step 6 "Manifest + pin/version gates" agent no \
  "step 5 produced byte-identical A/B outputs"
run_block <<EOF
APK_TOOL=apk scripts/check-apk-build-manifest.sh \\
  --manifest <out>/build-manifest-A.json --compare <out>/build-manifest-B.json \\
  --apk <out>/goshisen-${VER}-r1.apk --payload <out>/daemon-A.bin
GATE_S005_CHECKS=1 scripts/release-pin-gate.sh \\
  --apk <out>/goshisen-${VER}-r1.apk --tag $TAG \\
  --payload <out>/daemon-A.bin --manifest <out>/build-manifest-A.json
EOF
expect_block <<EOF
PASS (manifest: self-consistent, recipe == the $TAG recipe, hashes match)
PASS ($TAG: pin ancestor/equal, empty runtime diff, payload --version == ${VER},
       filename == ${VER}-r<PKG_RELEASE>, S-005 clean)
EOF
note_block <<'EOF'
refusal: either gate non-zero -> STOP. No apk is uploaded, no signing run is
dispatched. This is the third hard stop named in issue #142.
EOF

step 7 "Unsigned VM smoke (privileged QEMU lab)" agent yes \
  "step 6 green; a pristine no-daemon image with the v0.2.9 lab profile"
run_block <<'EOF'
sudo scripts/openwrt-lab.sh up
# guest, from the lab console (scripts/openwrt-console.py):
#   apk add --no-network --allow-untrusted /tmp/goshisen-<ver>-r1.apk
#   goshisen --version ; sha256sum /usr/sbin/goshisen
#   nft list ruleset | grep -E 'queue (to|num) 10[01]'
#   DROP default: 3 probes blocked ; paired ACCEPT/DROP controls
#   kill -9 <daemon pid>  -> procd respawn, queues rebound, ACCEPT still passes
#   apk del goshisen      -> no 'inet goshisen' table, no queue left
sudo scripts/openwrt-lab.sh down
EOF
expect_block <<'EOF'
installed goshisen-<ver>; guest payload sha256 == <out>/daemon-A.bin sha256
queues 100 and 101 bound to the daemon pid; teardown leaves no QEMU/netns/TAP
EOF
note_block <<'EOF'
the v0.2.9 run drove this with `run-029-smoke.sh`, a handoff script that is
NOT in the repo (docs/release/v0.2.9-apk-build-recipe.md §9 says so). The
wrapper prints the committed lab surface and the checks; it does not pretend
a committed driver exists.
EOF

step 8 "Put the unsigned apk into the draft release" maintainer yes \
  "step 6 green; the workflow fetches the asset by name from the draft, so it must exist first"
run_block <<EOF
gh release upload $TAG <out>/goshisen-${VER}-r1.apk
gh release view $TAG --json assets --jq '.assets[].name'
EOF
expect_block <<EOF
goshisen-${VER}-r1.apk listed on the draft $TAG
EOF
note_block <<'EOF'
credential-gated (needs a token with write access). This is why the unsigned
artifact reaches the draft: apk-sign.yml's production mode starts with
  python3 scripts/fetch-release-asset.py <tag> <asset> <asset>
EOF

step 9 "Production apk signing dispatch" maintainer yes \
  "unsigned apk sha256 known; anchor fingerprint is the APK key (542bb7ae..., never rotated)"
run_block <<EOF
gh workflow run apk-sign.yml --ref main \\
  -f mode=production \\
  -f release_tag=$TAG \\
  -f apk_release_asset=goshisen-${VER}-r1.apk \\
  -f expected_sha256=<apk-sha256> \\
  -f fingerprint=542bb7ae9058fee9b4d37ed30b0af44f234ea7cb8866daa1ffc71a7bc37c839d \\
  -f commit_sha=<pin-commit>
gh run watch "\$(gh run list --workflow apk-sign.yml --limit 1 --json databaseId --jq '.[0].databaseId')" --exit-status
EOF
expect_block <<'EOF'
run success; the signed apk + provenance are uploaded as the workflow
artifact 'signed-apks' (the workflow never publishes a release)
EOF
note_block <<'EOF'
credential-gated (environment 'signing' holds GH_RELEASE_APK_SIGNING_KEY).
expected_sha256 is mandatory in production: a mismatch fails closed.
EOF

step 10 "Signed VM smoke and attach the signed apk" maintainer yes \
  "signed apk from step 9; the same lab profile as step 7"
run_block <<EOF
# guest: install the anchor, then the SIGNED apk without --allow-untrusted
#   install -m 0644 goshisen-apk.pub.pem /etc/apk/keys/
#   apk add --no-network ./goshisen-${VER}-r1.apk      # signature verified
#   upgrade over the previous release; probes must never pass while stopping
gh release upload $TAG <signed-apk> --clobber
gh release edit $TAG --notes-file docs/release/$TAG-notes.md
EOF
expect_block <<'EOF'
apk verify --keys-dir /etc/apk/keys -> OK
published draft carries the signed goshisen-<ver>-r1.apk and the notes
EOF
note_block <<'EOF'
the signed artifact is attached by hand: apk-sign.yml deliberately cannot
publish, so the wrapper prints the upload instead of doing it.
EOF

step 11 "Publish flip" maintainer yes \
  "steps 3-10 all green; the draft is complete"
run_block <<EOF
gh release view $TAG --json isDraft,assets --jq '{draft:.isDraft,n:(.assets|length)}'
gh release edit $TAG --draft=false
EOF
expect_block <<EOF
draft=true with 20 assets before the flip; published after it
EOF
note_block <<'EOF'
irreversible in practice (a published release may be re-edited, never
silently withdrawn): only the maintainer flips it.
EOF

step 12 "Post-publish sweep" agent no \
  "the release is public; no credential needed"
run_block <<EOF
# download the 20 assets of $TAG into <dir> (CLI assets + public_key.pem + apk)
scripts/verify-release-assets.sh --dir <dir> --cli-key <dir>/public_key.pem \\
  --apk-anchor keys/goshisen-apk.pub.pem --commit <pin-commit> \\
  --apk-sha256 <apk-sha256>
curl -fsSL https://api.github.com/repos/$REPO_SLUG/releases/tags/$TAG
EOF
expect_block <<EOF
VERIFY-PASS (exit 0)
draft=false, prerelease=false, 20 assets, tag peel == <pin-commit>
EOF

printf '\nhard stops (never crossed automatically)\n'
cat <<'EOF'
   - step 1: pre-tag gate FAIL             -> no tag
   - step 5: A/B hashes differ             -> no gate, no upload, no signing
   - step 6: manifest or pin gate FAIL     -> no upload, no signing
   - any step: failure stops the run; a later step never "carries on"
EOF

printf '\n'
printf 'DRY-RUN: %s steps planned, 0 commands executed, 0 files written.\n' "$STEPS"
printf 'Nothing was tagged, built, signed, uploaded, published or modified.\n'
printf -- '--execute is not implemented in this slice (issue #142).\n'