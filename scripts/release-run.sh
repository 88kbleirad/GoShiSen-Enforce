#!/usr/bin/env bash
# GoShiSen-Enforce — release-run wrapper (P3.11, issue #142).
#
# One script for a whole release run: pre-tag gate -> tag -> release workflow
# -> post-tag pin PR -> apk build A/B -> manifest + pin gates -> unsigned VM
# smoke -> draft upload -> production apk signing -> signed smoke/attach ->
# publish flip -> post-publish sweep.
#
#   --dry-run   print every step, its exact commands and its expected
#               evidence; execute nothing (no tag, build, sign, upload,
#               publish or file written).
#   --execute   run the steps whose owner is `agent`, then stop at the first
#               `maintainer` step (needs credentials) or `manual` step (needs
#               the QEMU lab console) with the exact commands to run. One
#               evidence file per run records step -> command -> output ->
#               sha256 (execute mode only). Resume after a gated step with
#               --from-step N.
#
# The plan is codified from the v0.2.9 run as recorded in
#   docs/release/v0.2.9-notes.md ("Reproduction"), docs/release/v0.2.9-apk-build-recipe.md
#   §9, docs/release/release-pin-gate.md, docs/release-checklist.md
#   §"Release mechanics" / "Post-release verification", and the dispatch inputs
#   of .github/workflows/{release,apk-sign}.yml.
# Where the last run used a driver that is NOT committed (the v0.2.9 VM smoke
# driver), step 7 says so instead of inventing a command.
#
# Usage:
#   scripts/release-run.sh --tag vX.Y.Z --dry-run
#   scripts/release-run.sh --tag vX.Y.Z --execute [--from-step N]
#     [--repo DIR] [--go-root DIR] [--out DIR] [--assets-dir DIR]
#     [--evidence-dir DIR] [--repo-slug OWNER/NAME]
#
# Exit codes:
#   0  PASS (dry-run printed, or every agent step completed)
#   1  a step failed (fail-closed; later steps do not run)
#   2  usage (missing/conflicting arguments)
#   4  tag rejected by scripts/validate-release-tag.sh (S-003)
#   5  stopped at a `maintainer` step: run the printed commands, then re-run
#      with --from-step <next>
#   6  stopped at a `manual` step (interactive lab/smoke), same resume rule
#   7  a step needs an input you did not pass (a <placeholder> would have been
#      executed); nothing was run for that step
#   8  preflight failed (dirty tree, not on main, main != origin/main, or
#      PKG_VERSION != the tag version)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VALIDATE_TAG="$ROOT/scripts/validate-release-tag.sh"

TAG=""
MODE=""
EVID=""
REPO=""
REPO_SLUG="88kbleirad/GoShiSen-Enforce"
FROM_STEP=1
GO_ROOT=""
OUT=""
ASSETS_DIR=""

die() { echo "FAIL: $*" >&2; exit 1; }

usage() {
  cat >&2 <<'EOF'
usage: scripts/release-run.sh --tag vX.Y.Z --dry-run [options]
       scripts/release-run.sh --tag vX.Y.Z --execute [--from-step N] [options]

  --tag vX.Y.Z          the release tag (^v<major>.<minor>.<patch>$)
  --dry-run             print every step, its exact commands and its expected
                        evidence; execute nothing
  --execute             run the `agent` steps, then stop at the first
                        `maintainer`/`manual` step with the exact commands
  --from-step N         start at step N (default 1); use it to resume after a
                        gated step
  --repo DIR            tree the run is performed in (default: this checkout)
  --go-root DIR         verified go1.27.4 tree for the apk build (step 5)
  --out DIR             build output dir (steps 5-6: manifests, apk, payload)
  --assets-dir DIR      dir holding the downloaded release assets (step 12)
  --evidence-dir DIR    evidence dir (execute only; default
                        $HOME/release-evidence/<tag>-<UTC>, deliberately outside
                        the worktree so step 1's clean-tree gate still passes)
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
    --from-step)    [ $# -ge 2 ] || usage; FROM_STEP="$2"; shift 2 ;;
    --from-step=*)  FROM_STEP="${1#*=}"; shift ;;
    --repo)         [ $# -ge 2 ] || usage; REPO="$2"; shift 2 ;;
    --repo=*)       REPO="${1#*=}"; shift ;;
    --repo-slug)    [ $# -ge 2 ] || usage; REPO_SLUG="$2"; shift 2 ;;
    --repo-slug=*)  REPO_SLUG="${1#*=}"; shift ;;
    --go-root)      [ $# -ge 2 ] || usage; GO_ROOT="$2"; shift 2 ;;
    --go-root=*)    GO_ROOT="${1#*=}"; shift ;;
    --out)          [ $# -ge 2 ] || usage; OUT="$2"; shift 2 ;;
    --out=*)        OUT="${1#*=}"; shift ;;
    --assets-dir)   [ $# -ge 2 ] || usage; ASSETS_DIR="$2"; shift 2 ;;
    --assets-dir=*) ASSETS_DIR="${1#*=}"; shift ;;
    --evidence-dir) [ $# -ge 2 ] || usage; EVID="$2"; shift 2 ;;
    --evidence-dir=*) EVID="${1#*=}"; shift ;;
    -h|--help)      usage ;;
    *)              echo "unknown argument: $1" >&2; usage ;;
  esac
done

[ -n "$TAG" ]  || { echo "missing --tag" >&2; usage; }
[ -n "$MODE" ] || { echo "one of --dry-run / --execute is required" >&2; usage; }
[ -x "$VALIDATE_TAG" ] || die "missing $VALIDATE_TAG"
case "$FROM_STEP" in ''|*[!0-9]*) echo "--from-step must be a number" >&2; usage ;; esac
[ "$FROM_STEP" -ge 1 ] && [ "$FROM_STEP" -le 12 ] || { echo "--from-step must be 1..12" >&2; usage; }

# S-003: the tag name reaches shell commands later; reject it before it is used.
bash "$VALIDATE_TAG" "$TAG" || exit 4

VER="${TAG#v}"
UTC="$(date -u +%Y%m%dT%H%M%SZ)"
[ -n "$REPO" ] || REPO="$ROOT"
[ -d "$REPO" ] || die "no such --repo: $REPO"
REPO="$(cd "$REPO" && pwd)"
# Evidence must live OUTSIDE the worktree: step 1's F-01 gate requires the tree
# to be exactly a commit, so a run that writes evidence into it could not tag.
[ -n "$EVID" ] || EVID="${RELEASE_RUN_EVIDENCE:-$HOME/release-evidence}/$TAG-$UTC"
EVIDRUN="$EVID/run.md"

git_repo() { git -C "$REPO" "$@"; }

# Best-effort values for the printed plan; a value that is still unknown stays
# a <placeholder>, and --execute refuses to run a step whose block contains one.
PIN=""
if git_repo rev-parse -q --verify "refs/tags/$TAG^{commit}" >/dev/null 2>&1; then
  PIN="$(git_repo rev-parse "$TAG^{commit}")"
fi
[ -n "$PIN" ]       || PIN='<pin-commit>'
[ -n "$GO_ROOT" ]   || GO_ROOT='<go-root>'
[ -n "$OUT" ]       || OUT='<out>'
[ -n "$ASSETS_DIR" ] || ASSETS_DIR='<dir>'
APK_SHA='<apk-sha256>'
SIGNED_APK='<signed-apk>'

# A build manifest that already exists (a resumed run) supplies the apk hash.
if [ -f "$OUT/build-manifest-A.json" ]; then
  APK_SHA="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["artifacts"]["apk"]["sha256"])' \
    "$OUT/build-manifest-A.json" 2>/dev/null || echo '<apk-sha256>')"
fi

# Placeholders still unknown for this invocation (a <...> value). --execute
# refuses to run a step whose command block still contains one.
UNRESOLVED=""
add_ph() { case "$1" in '<'*) UNRESOLVED="$UNRESOLVED $2" ;; esac; }
add_ph "$PIN" "<pin-commit>"
add_ph "$GO_ROOT" "<go-root>"
add_ph "$OUT" "<out>"
add_ph "$ASSETS_DIR" "<dir>"
add_ph "$APK_SHA" "<apk-sha256>"
add_ph "$SIGNED_APK" "<signed-apk>"

BLOCK=""   # command text of the step being rendered/executed

# ------------------------------------------------------------------ steps
TITLE=(); OWNER=(); MUTATES=(); GATE=()

TITLE[1]="Tag-name guard and F-01 pre-tag gate"
OWNER[1]=agent; MUTATES[1]=no
GATE[1]="the commit to be tagged already carries PKG_VERSION=$VER, PKG_RELEASE set, and the tree is exactly a commit"
cmds_1() {
  cat <<EOF
bash scripts/validate-release-tag.sh $TAG
GATE_REPO=$REPO bash scripts/release-pretag-check.sh --tag $TAG
EOF
}
expect_1() {
  cat <<EOF
OK: release tag '$TAG' matches ^v[0-9]+\\.[0-9]+\\.[0-9]+\$
PASS: pre-tag gate $TAG (PKG_VERSION=$VER, PKG_RELEASE set)
EOF
}
note_1() {
  cat <<'EOF'
refusal: either command non-zero -> STOP. No tag is created (this is the
v0.2.6 defect: the tag shipped PKG_VERSION:=0.2.5 and only a post-tag PR
fixed it).
EOF
}

TITLE[2]="Annotated tag and push"
OWNER[2]=maintainer; MUTATES[2]=yes
GATE[2]="step 1 green, preflight clean"
cmds_2() {
  cat <<EOF
git tag -a $TAG -m "GoShiSen-Enforce $TAG"
git push origin $TAG
EOF
}
expect_2() {
  cat <<EOF
gh run list --workflow release.yml --limit 1 --json databaseId,headSha,status
# one run on the $TAG tag commit; 'validate' then 'build' go green,
# 'sign' pauses on environment 'signing' (required reviewer)
EOF
}
note_2() {
  cat <<'EOF'
mutating: creates the tag (remote + local). Nothing else in the plan runs
before step 1 passes.
EOF
}

TITLE[3]="Release workflow: validate -> build -> sign (draft release)"
OWNER[3]=maintainer; MUTATES[3]=yes
GATE[3]="the sign job is approved in environment 'signing'; the CLI key resolves from the environment secret only"
cmds_3() {
  cat <<EOF
RUN_ID=\$(gh run list --workflow release.yml --limit 1 --json databaseId --jq '.[0].databaseId')
gh run watch "\$RUN_ID" --exit-status
# approval (maintainer, after checking the pending deployment's environment)
gh api repos/$REPO_SLUG/actions/runs/\$RUN_ID/pending_deployments
gh api -X POST repos/$REPO_SLUG/actions/runs/\$RUN_ID/pending_deployments \\
  -F environment_ids[]=<env-id-from-the-list> -F state=approved \\
  -F comment="release-run $TAG"
EOF
}
expect_3() {
  cat <<EOF
OK: trust anchor fingerprint verified e9b4bf11...
VERIFY-PASS
gh release view $TAG --json isDraft,assets    # isDraft=true, 19 CLI assets
EOF
}
note_3() {
  cat <<'EOF'
credential-gated: the wrapper never signs. The job is the only holder of
GH_RELEASE_SIGNING_KEY; the anchor is verified inside the workflow.
EOF
}

TITLE[4]="Post-tag pin PR (openwrt/Makefile only)"
OWNER[4]=maintainer; MUTATES[4]=yes
GATE[4]="step 3 produced the draft; the pin commit is a Makefile-only follow-up on main"
cmds_4() {
  cat <<EOF
# openwrt/Makefile: PKG_VERSION:=$VER  PKG_RELEASE:=1
#                  PKG_SOURCE_VERSION:=$PIN   (the tag commit)
git switch -c pkg/pin-$TAG main
git commit -am "pkg: pin $TAG tag commit in openwrt Makefile"
git push -u origin pkg/pin-$TAG
gh pr create --base main --title "pkg: pin $TAG tag commit in openwrt Makefile" --body "..."
EOF
}
expect_4() {
  cat <<EOF
# required: PR CI green, in particular 'release-pretag-gate (F-01)'
git show $PIN --stat          # openwrt/Makefile only (docs allowed)
git ls-remote --tags origin $TAG      # tag object unchanged
EOF
}
note_4() {
  cat <<'EOF'
A tag cannot contain its own hash, so the pin is the documented post-tag
commit; the gate in step 6 proves source equivalence between pin and tag.
Merge is the maintainer's (repo convention: squash).
EOF
}

TITLE[5]="apk build A/B (reproducibility)"
OWNER[5]=agent; MUTATES[5]=yes
GATE[5]="pin commit from step 4 is on main; ~10-30 min per build, ~5 GB scratch each"
cmds_5() {
  cat <<EOF
scripts/build-apk-release.sh --commit $PIN \\
  --label A --canonical-name \\
  --go-root $GO_ROOT --out $OUT --work $OUT/work-A
scripts/build-apk-release.sh --commit $PIN \\
  --label B \\
  --go-root $GO_ROOT --out $OUT --work $OUT/work-B
scripts/check-apk-build-manifest.sh \\
  --manifest $OUT/build-manifest-A.json --compare $OUT/build-manifest-B.json
EOF
}
expect_5() {
  cat <<'EOF'
OK: A/B manifests agree on inputs and on apk + payload hashes (labels A/B)
APK-BUILD-MANIFEST-CHECK-PASS
<out>/goshisen-<ver>-r1.apk  <out>/daemon-A.bin
<out>/build-manifest-A.json  <out>/build-manifest-B.json
EOF
}
note_5() {
  cat <<'EOF'
refusal: the compare fails ("A/B mismatch on .artifacts.apk.sha256: ... vs ...",
or "both manifests carry label ...") -> STOP. Nothing is signed, uploaded or
published from a non-reproducible build. The build never publishes or signs.
EOF
}

TITLE[6]="Manifest + pin/version gates"
OWNER[6]=agent; MUTATES[6]=no
GATE[6]="step 5 produced byte-identical A/B outputs"
cmds_6() {
  cat <<EOF
APK_TOOL=apk scripts/check-apk-build-manifest.sh \\
  --manifest $OUT/build-manifest-A.json --compare $OUT/build-manifest-B.json \\
  --apk $OUT/goshisen-$VER-r1.apk --payload $OUT/daemon-A.bin
GATE_S005_CHECKS=1 scripts/release-pin-gate.sh \\
  --apk $OUT/goshisen-$VER-r1.apk --tag $TAG \\
  --payload $OUT/daemon-A.bin --manifest $OUT/build-manifest-A.json
EOF
}
expect_6() {
  cat <<EOF
PASS (manifest: self-consistent, recipe == the $TAG recipe, hashes match)
PASS ($TAG: pin ancestor/equal, empty runtime diff, payload --version == $VER,
       filename == $VER-r<PKG_RELEASE>, S-005 clean)
EOF
}
note_6() {
  cat <<'EOF'
refusal: either gate non-zero -> STOP. No apk is uploaded, no signing run is
dispatched. This is the third hard stop named in issue #142.
EOF
}

TITLE[7]="Unsigned VM smoke (privileged QEMU lab)"
OWNER[7]=manual; MUTATES[7]=yes
GATE[7]="step 6 green; a pristine no-daemon image with the v0.2.9 lab profile"
cmds_7() {
  cat <<EOF
sudo scripts/openwrt-lab.sh up
# guest, from the lab console (scripts/openwrt-lab.sh console):
#   apk add --no-network --allow-untrusted /tmp/goshisen-$VER-r1.apk
#   goshisen --version ; sha256sum /usr/sbin/goshisen
#   nft list ruleset | grep -E 'queue (to|num) 10[01]'
#   DROP default: 3 probes blocked ; paired ACCEPT/DROP controls
#   kill -9 <daemon pid>  -> procd respawn, queues rebound, ACCEPT still passes
#   apk del goshisen      -> no 'inet goshisen' table, no queue left
sudo scripts/openwrt-lab.sh down
EOF
}
expect_7() {
  cat <<'EOF'
installed goshisen-<ver>; guest payload sha256 == <out>/daemon-A.bin sha256
queues 100 and 101 bound to the daemon pid; teardown leaves no QEMU/netns/TAP
EOF
}
note_7() {
  cat <<'EOF'
the v0.2.9 run drove this with `run-029-smoke.sh`, a handoff script that is
NOT in the repo (docs/release/v0.2.9-apk-build-recipe.md §9 says so). Step 7 is
therefore `manual`: the wrapper waits for you instead of pretending a committed
driver exists.
EOF
}

TITLE[8]="Put the unsigned apk into the draft release"
OWNER[8]=maintainer; MUTATES[8]=yes
GATE[8]="step 6 green; the workflow fetches the asset by name from the draft, so it must exist first"
cmds_8() {
  cat <<EOF
gh release upload $TAG $OUT/goshisen-$VER-r1.apk
gh release view $TAG --json assets --jq '.assets[].name'
EOF
}
expect_8() {
  cat <<EOF
goshisen-$VER-r1.apk listed on the draft $TAG
EOF
}
note_8() {
  cat <<'EOF'
credential-gated (needs a token with write access). This is why the unsigned
artifact reaches the draft: apk-sign.yml's production mode starts with
  python3 scripts/fetch-release-asset.py <tag> <asset> <asset>
EOF
}

TITLE[9]="Production apk signing dispatch"
OWNER[9]=maintainer; MUTATES[9]=yes
GATE[9]="unsigned apk sha256 known; anchor fingerprint is the APK key (542bb7ae..., never rotated)"
cmds_9() {
  cat <<EOF
gh workflow run apk-sign.yml --ref main \\
  -f mode=production \\
  -f release_tag=$TAG \\
  -f apk_release_asset=goshisen-$VER-r1.apk \\
  -f expected_sha256=$APK_SHA \\
  -f fingerprint=542bb7ae9058fee9b4d37ed30b0af44f234ea7cb8866daa1ffc71a7bc37c839d \\
  -f commit_sha=$PIN
gh run watch "\$(gh run list --workflow apk-sign.yml --limit 1 --json databaseId --jq '.[0].databaseId')" --exit-status
EOF
}
expect_9() {
  cat <<'EOF'
run success; the signed apk + provenance are uploaded as the workflow
artifact 'signed-apks' (the workflow never publishes a release)
EOF
}
note_9() {
  cat <<'EOF'
credential-gated (environment 'signing' holds GH_RELEASE_APK_SIGNING_KEY).
expected_sha256 is mandatory in production: a mismatch fails closed.
EOF
}

TITLE[10]="Signed VM smoke and attach the signed apk"
OWNER[10]=maintainer; MUTATES[10]=yes
GATE[10]="signed apk from step 9; the same lab profile as step 7"
cmds_10() {
  cat <<EOF
# guest: install the anchor, then the SIGNED apk without --allow-untrusted
#   install -m 0644 goshisen-apk.pub.pem /etc/apk/keys/
#   apk add --no-network ./goshisen-$VER-r1.apk      # signature verified
#   upgrade over the previous release; probes must never pass while stopping
gh release upload $TAG $SIGNED_APK --clobber
gh release edit $TAG --notes-file docs/release/$TAG-notes.md
EOF
}
expect_10() {
  cat <<'EOF'
apk verify --keys-dir /etc/apk/keys -> OK
published draft carries the signed goshisen-<ver>-r1.apk and the notes
EOF
}
note_10() {
  cat <<'EOF'
the signed artifact is attached by hand: apk-sign.yml deliberately cannot
publish, so the wrapper prints the upload instead of doing it.
EOF
}

TITLE[11]="Publish flip"
OWNER[11]=maintainer; MUTATES[11]=yes
GATE[11]="steps 3-10 all green; the draft is complete"
cmds_11() {
  cat <<EOF
gh release view $TAG --json isDraft,assets --jq '{draft:.isDraft,n:(.assets|length)}'
gh release edit $TAG --draft=false
EOF
}
expect_11() {
  cat <<EOF
draft=true with 20 assets before the flip; published after it
EOF
}
note_11() {
  cat <<'EOF'
irreversible in practice (a published release may be re-edited, never
silently withdrawn): only the maintainer flips it.
EOF
}

TITLE[12]="Post-publish sweep"
OWNER[12]=agent; MUTATES[12]=no
GATE[12]="the release is public; no credential needed"
cmds_12() {
  cat <<EOF
scripts/verify-release-assets.sh --dir $ASSETS_DIR \\
  --cli-key $ASSETS_DIR/public_key.pem \\
  --apk-anchor keys/goshisen-apk.pub.pem --commit $PIN \\
  --apk-sha256 $APK_SHA
curl -fsSL https://api.github.com/repos/$REPO_SLUG/releases/tags/$TAG
EOF
}
expect_12() {
  cat <<EOF
VERIFY-PASS (exit 0)
draft=false, prerelease=false, 20 assets, tag peel == $PIN
EOF
}
note_12() {
  cat <<'EOF'
refusal: verify-release-assets.sh non-zero (3-8) -> the release is not done;
investigate before touching anything. The API line must show draft=false.
EOF
}

STEPS=12

# --------------------------------------------------------------- rendering
print_step() { # n
  local n="$1"
  printf '\n── step %s — %s\n' "$n" "${TITLE[$n]}"
  printf '   owner:   %s\n' "${OWNER[$n]}"
  printf '   mutates: %s\n' "${MUTATES[$n]}"
  printf '   gate:    %s\n' "${GATE[$n]}"
  printf '   run:\n';    "cmds_$n"   | sed 's/^/     /'
  printf '   expect:\n'; "expect_$n" | sed 's/^/     /'
  printf '   note:\n';   "note_$n"   | sed 's/^/     /'
}

# --------------------------------------------------------------- preflight
preflight_problems() {
  git_repo rev-parse --is-inside-work-tree >/dev/null 2>&1 || { echo "not a git work tree: $REPO"; return; }
  local st; st="$(git_repo status --porcelain)"
  [ -z "$st" ] || echo "working tree not clean: $(printf '%s' "$st" | head -3 | tr '\n' ' ')"
  local br; br="$(git_repo rev-parse --abbrev-ref HEAD 2>/dev/null || echo '?')"
  [ "$br" = "main" ] || echo "HEAD is '$br', not 'main'"
  local m o; m="$(git_repo rev-parse -q --verify refs/heads/main || true)"
  o="$(git_repo rev-parse -q --verify refs/remotes/origin/main || true)"
  if [ -z "$m" ] || [ -z "$o" ]; then echo "cannot resolve main and origin/main"; 
  elif [ "$m" != "$o" ]; then echo "main ($m) != origin/main ($o)"; fi
}

# ----------------------------------------------------------------- evidence
EV_ROWS=0
EVIDROWS=""
EVIDDET=""
ev_start() {
  mkdir -p "$EVID/outputs"
  EVIDROWS="$(mktemp)"; EVIDDET="$(mktemp)"
}
ev_row() { # step owner status exit sha
  printf '| %s | %s | %s | %s | %s |\n' "$1" "$2" "$3" "$4" "$5" >> "$EVIDROWS"
  EV_ROWS=$((EV_ROWS + 1))
}
ev_detail() { # step status "extra" [logfile]
  local n="$1" status="$2" extra="$3" log="${4:-}"
  {
    printf '\n### step %s — %s (%s%s)\n\ncommand:\n\n```\n' "$n" "${TITLE[$n]}" "$status" "$extra"
    printf '%s\n' "$(cmds_"$n")"
    printf '```\n'
    if [ -n "$log" ] && [ -s "$log" ]; then
      printf '\noutput (tail):\n\n```\n'
      tail -n 40 "$log"
      printf '```\n'
    fi
  } >> "$EVIDDET"
}
ev_output() { # step slug rc file -> sha256 on stdout, row + detail recorded
  local n="$1" slug="$2" rc="$3" file="$4" sha
  sha="$(sha256sum "$file" | cut -d' ' -f1)"
  ev_row "$n" "${OWNER[$n]}" "$slug" "$rc" "\`$sha\`"
  ev_detail "$n" "$slug" ", exit $rc, output sha256 $sha" "$file"
  echo "$sha"
}
ev_finish() {
  cat > "$EVIDRUN" <<EOF
# release-run $TAG — $UTC

- repo: \`$REPO\`
- mode: execute (from step $FROM_STEP), github: \`$REPO_SLUG\`
- result: ${halt_reason:-all $STEPS steps ran}${halt_code:+ (exit $halt_code)}
- raw step output: \`outputs/NN-step.log\` next to this file

| step | owner | status | exit | output sha256 |
| --- | --- | --- | --- | --- |
EOF
  cat "$EVIDROWS" >> "$EVIDRUN"
  cat "$EVIDDET" >> "$EVIDRUN"
  rm -f "$EVIDROWS" "$EVIDDET"
}

# ------------------------------------------------------------------ execute
halt_reason=""
halt_code=0
exec_step() { # n
  local n="$1" block log slug
  block="$("cmds_$n")"

  case "${OWNER[$n]}" in
    maintainer)
      printf '\nSTOP — step %s needs a maintainer (credentials / settings).\n' "$n"
      printf 'Run exactly this, then resume with --from-step %s:\n\n' "$((n + 1))"
      printf '%s\n' "$block" | sed 's/^/  /'
      ev_row "$n" maintainer halted - "-"
      ev_detail "$n" halted "" "$block"
      halt_reason="maintainer action required at step $n"; halt_code=5; return 1 ;;
    manual)
      printf '\nSTOP — step %s is manual (interactive lab/smoke).\n' "$n"
      printf 'Run it, then resume with --from-step %s:\n\n' "$((n + 1))"
      printf '%s\n' "$block" | sed 's/^/  /'
      ev_row "$n" manual halted - "-"
      ev_detail "$n" halted "" "$block"
      halt_reason="manual step required at step $n"; halt_code=6; return 1 ;;
  esac

  # An unresolved <placeholder> means an input was not supplied: never run it.
  if printf '%s' "$block" | grep -q '<'; then
    local ph; ph="$(printf '%s' "$block" | grep -o '<[^>]*>' | sort -u | tr '\n' ' ')"
    printf '\nSTOP — step %s needs input: %s\n' "$n" "$ph"
    printf 'Re-run with the matching flag (see --help), then --from-step %s.\n' "$n"
    ev_row "$n" agent input-required - "-"
    ev_detail "$n" input-required ", needs $ph" "$block"
    halt_reason="input required at step $n: $ph"; halt_code=7; return 1
  fi

  slug="$(printf '%02d' "$n")"
  log="$EVID/outputs/$slug-step.log"
  printf '\n── step %s — %s\n' "$n" "${TITLE[$n]}"
  set +e
  ( cd "$REPO" && OUT="$OUT" bash -euo pipefail -c "$block" ) >"$log" 2>&1
  local rc=$?
  set -e
  if [ "$rc" -ne 0 ]; then
    sed 's/^/  /' "$log" | tail -n 25
    ev_output "$n" failed "$rc" "$log" >/dev/null
    halt_reason="step $n failed (exit $rc)"; halt_code=1; return 1
  fi
  ev_output "$n" ok "$rc" "$log" >/dev/null
  sed 's/^/  /' "$log" | tail -n 5
  return 0
}

# --------------------------------------------------------------------- main
if [ "$MODE" = "dry-run" ]; then
  cat <<EOF
release-run — $TAG (dry-run)
  repo:         $REPO
  github:       $REPO_SLUG
  evidence:     $EVID (would be created by --execute; nothing written now)
  unresolved:  ${UNRESOLVED:- none}
EOF
  printf '\npreflight (read-only; --execute refuses to start if any fails)\n'
  cat <<'EOF'
     git status --porcelain            # must be empty
     git rev-parse --abbrev-ref HEAD   # must be main
     git rev-parse main origin/main    # must be equal
   # PKG_VERSION == the tag version is step 1's own claim (F-01 gate)
EOF
  for n in $(seq 1 "$STEPS"); do print_step "$n"; done
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
  printf -- 'Resume semantics of --execute: it runs the `agent` steps and stops at the\n'
  printf -- 'first `maintainer`/`manual` one; re-invoke with --from-step <next>.\n'
  exit 0
fi

# execute mode
problems="$(preflight_problems)"
if [ -n "$problems" ]; then
  printf 'FAIL: preflight (nothing was run)\n' >&2
  printf '%s\n' "$problems" | sed 's/^/  - /' >&2
  exit 8
fi

ev_start
printf 'release-run — %s (execute, from step %s)\n  evidence: %s\n' "$TAG" "$FROM_STEP" "$EVID"

for n in $(seq 1 "$STEPS"); do
  if [ "$n" -lt "$FROM_STEP" ]; then
    ev_row "$n" "${OWNER[$n]}" skipped - "-"
    continue
  fi
  exec_step "$n" || break
done

ev_finish

printf '\n'
if [ -n "$halt_reason" ]; then
  printf 'HALT (exit %s): %s\n' "$halt_code" "$halt_reason"
  printf 'Evidence: %s (%s step rows)\n' "$EVIDRUN" "$EV_ROWS"
  exit "$halt_code"
fi
printf 'RELEASE-RUN-COMPLETE: all %s steps ran.\n' "$STEPS"
printf 'Evidence: %s (%s step rows)\n' "$EVIDRUN" "$EV_ROWS"