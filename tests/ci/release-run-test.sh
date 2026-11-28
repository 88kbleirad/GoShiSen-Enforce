#!/usr/bin/env bash
# release-run-test.sh — regression test for scripts/release-run.sh (P3.11).
#
# Contract, both modes:
#   --dry-run  prints every step with its exact commands and expected evidence,
#              validates the tag through the S-003 guard, and mutates nothing —
#              no tag, no directory, no working-tree change.
#   --execute  runs the `agent` steps, stops at the first `maintainer`/`manual`
#              step with the exact commands, refuses to run a step whose inputs
#              are missing, and writes one evidence file per run. A failing gate
#              stops the run (fail-closed) and never tags anything.
#
# Execution is exercised against scratch git fixtures (GATE_REPO/--repo), so no
# real release, tag, credential or lab is touched.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
RUN="$ROOT/scripts/release-run.sh"
TAG="v9.9.9"
VER="${TAG#v}"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }

# run <expected-exit> <args...> — captures stdout+stderr into $OUT
run() {
  local want="$1"; shift
  local rc=0
  OUT="$(bash "$RUN" "$@" 2>&1)" || rc=$?
  [ "$rc" -eq "$want" ] || { printf '%s\n' "$OUT" >&2; fail "exit $rc, want $want for: $*"; }
}

grep_out() { grep -qF -- "$1" <<<"$OUT" || { printf '%s\n' "$OUT" >&2; fail "output missing: $1"; }; }
deny_out() { ! grep -qF -- "$1" <<<"$OUT" || { printf '%s\n' "$OUT" >&2; fail "output must not contain: $1"; }; }

# make_fixture <dir> <pkg-version> — a clean git repo on main (origin/main in
# sync) carrying openwrt/Makefile and a symlink to the real scripts, which is
# what step 1 (validate + F-01 gate) inspects.
make_fixture() {
  local fx="$1" ver="$2"
  rm -rf "$fx" "$fx.origin"
  mkdir -p "$fx/openwrt"
  git init -q -b main "$fx"
  git -C "$fx" config user.email t@example.com
  git -C "$fx" config user.name t
  printf 'PKG_VERSION:=%s\nPKG_RELEASE:=1\n' "$ver" > "$fx/openwrt/Makefile"
  ln -s "$ROOT/scripts" "$fx/scripts"
  git -C "$fx" add -A
  git -C "$fx" commit -qm "fixture $ver"
  git init -q --bare "$fx.origin"
  git -C "$fx" remote add origin "$fx.origin"
  git -C "$fx" push -q -u origin main
}

# ---- fail-closed argument handling -------------------------------------
run 2
grep_out "missing --tag"

run 2 --tag "$TAG"
grep_out "one of --dry-run / --execute is required"

run 2 --tag "$TAG" --dry-run --execute
grep_out "usage:"

run 2 --tag "$TAG" --frobnicate
run 2 --tag "$TAG" --dry-run --from-step 0
grep_out "1..12"

# S-003: the tag reaches shell commands later; it is rejected before use.
for bad in 'v1.0;touch /tmp/pwned' 'v1.0$(id)' 'v1.0`id`' 'v1.0|true' 'v1.0 0.2' 'v0.2' '1.2.3'; do
  run 4 --tag "$bad" --dry-run
  grep_out "does not match"
done

# ---- the dry-run plan ---------------------------------------------------
evidence_default="${RELEASE_RUN_EVIDENCE:-$HOME/release-evidence}/$TAG-"
before_status="$(git -C "$ROOT" status --porcelain)"
before_tags="$(git -C "$ROOT" tag -l "$TAG")"

run 0 --tag "$TAG" --dry-run

n=0
for title in \
  "Tag-name guard and F-01 pre-tag gate" \
  "Annotated tag and push" \
  "Release workflow: validate -> build -> sign (draft release)" \
  "Post-tag pin PR (openwrt/Makefile only)" \
  "apk build A/B (reproducibility)" \
  "Manifest + pin/version gates" \
  "Unsigned VM smoke (privileged QEMU lab)" \
  "Put the unsigned apk into the draft release" \
  "Production apk signing dispatch" \
  "Signed VM smoke and attach the signed apk" \
  "Publish flip" \
  "Post-publish sweep"
do
  n=$((n + 1))
  grep_out "── step $n — $title"
done
grep_out "DRY-RUN: 12 steps planned, 0 commands executed, 0 files written."
grep_out "Resume semantics of --execute"

# the commands are concrete, not hand-waving
grep_out "bash scripts/release-pretag-check.sh --tag $TAG"
grep_out "git tag -a $TAG"
grep_out "gh run watch"
grep_out "scripts/build-apk-release.sh --commit <pin-commit>"
grep_out "--manifest <out>/build-manifest-A.json --compare <out>/build-manifest-B.json"
deny_out "import json"          # the A/B compare is the committed gate, not a snippet
grep_out "A/B mismatch on .artifacts.apk.sha256"
grep_out "check-apk-build-manifest.sh"
grep_out "GATE_S005_CHECKS=1 scripts/release-pin-gate.sh"
grep_out "-f mode=production"
grep_out "gh release upload $TAG"
grep_out "gh release edit $TAG --draft=false"
grep_out "scripts/verify-release-assets.sh --dir <dir>"
grep_out "542bb7ae9058fee9b4d37ed30b0af44f234ea7cb8866daa1ffc71a7bc37c839d"
grep_out "release-evidence/$TAG-"
grep_out "<pin-commit>"

# no unexpanded shell variable leaked into the printed commands
deny_out '$TAG'
deny_out '$VER'

# owners are labelled: 4 agent, 7 maintainer (credentials), 1 manual (lab)
[ "$(grep -c 'owner:   maintainer' <<<"$OUT")" -eq 7 ] || fail "maintainer steps not labelled"
[ "$(grep -c 'owner:   agent' <<<"$OUT")" -eq 4 ] || fail "agent steps not labelled"
[ "$(grep -c 'owner:   manual' <<<"$OUT")" -eq 1 ] || fail "manual step not labelled"
for stop in "step 1: pre-tag gate FAIL" "step 5: A/B hashes differ" "step 6: manifest or pin gate FAIL"; do
  grep_out "$stop"
done

# ---- dry-run mutates nothing --------------------------------------------
[ "$(git -C "$ROOT" tag -l "$TAG")" = "$before_tags" ] || fail "dry-run created tag $TAG"
[ "$(git -C "$ROOT" status --porcelain)" = "$before_status" ] || fail "dry-run changed the working tree"
if compgen -G "${evidence_default}*" >/dev/null; then
  fail "dry-run wrote evidence under evidence/release-run/"
fi
run 0 --tag "$TAG" --dry-run --evidence-dir "$TMP/never"
grep_out "$TMP/never"
[ ! -e "$TMP/never" ] || fail "dry-run created the evidence dir"

# ---- execute: preflight refuses a tree it must not work in ---------------
FX="$TMP/fx-ok"
make_fixture "$FX" "$VER"
echo dirty > "$FX/openwrt/untracked"
run 8 --tag "$TAG" --execute --repo "$FX" --evidence-dir "$TMP/ev1"
grep_out "working tree not clean"
[ ! -e "$TMP/ev1" ] || fail "refused run still wrote evidence"

# a detached/other branch is refused too
git -C "$FX" switch -q -c feature/x
rm -f "$FX/openwrt/untracked"
run 8 --tag "$TAG" --execute --repo "$FX" --evidence-dir "$TMP/ev1"
grep_out "not 'main'"
git -C "$FX" switch -q main

# ---- execute: F-01 gate failure stops the run, nothing is tagged ---------
make_fixture "$FX" "0.0.1"
run 1 --tag "$TAG" --execute --repo "$FX" --evidence-dir "$TMP/ev1"
grep_out "PKG_VERSION=0.0.1 does not match tag version"
grep_out "HALT (exit 1)"
[ "$(git -C "$FX" tag -l | wc -l)" -eq 0 ] || fail "a failing run created a tag"
grep -q '| 1 | agent | failed | 1 |' "$TMP/ev1/run.md" || fail "evidence lacks the failed step row"
[ -s "$TMP/ev1/outputs/01-step.log" ] || fail "no raw output for the failed step"

# ---- execute: pass step 1, then stop at the first maintainer step --------
make_fixture "$FX" "$VER"
run 5 --tag "$TAG" --execute --repo "$FX" --evidence-dir "$TMP/ev1"
grep_out "STOP — step 2 needs a maintainer"
grep_out "git tag -a $TAG"
grep_out "resume with --from-step 3"
grep_out "HALT (exit 5)"
grep -q '| 1 | agent | ok | 0 |' "$TMP/ev1/run.md" || fail "step 1 not recorded as ok"
grep -q '| 2 | maintainer | halted | - |' "$TMP/ev1/run.md" || fail "step 2 not recorded as halted"
grep -q "git tag -a $TAG" "$TMP/ev1/run.md" || fail "the halted step's commands are not recorded"
grep -q '^```$' "$TMP/ev1/run.md" || fail "evidence fences are broken"
[ -s "$TMP/ev1/outputs/01-step.log" ] || fail "no raw output for step 1"
[ "$(grep -c '^| 3 ' "$TMP/ev1/run.md" || true)" -eq 0 ] || fail "steps after the halt were recorded"
# the sha256 in the evidence is the sha256 of the captured output
want="$(sha256sum "$TMP/ev1/outputs/01-step.log" | cut -d' ' -f1)"
grep -q "$want" "$TMP/ev1/run.md" || fail "evidence sha256 does not match the output file"

# ---- execute: resume with --from-step records the skipped prefix ---------
rm -rf "$TMP/ev1"
run 5 --tag "$TAG" --execute --repo "$FX" --from-step 2 --evidence-dir "$TMP/ev1"
grep_out "HALT (exit 5)"
grep -q '| 1 | agent | skipped | - |' "$TMP/ev1/run.md" || fail "step 1 not recorded as skipped"
[ ! -e "$TMP/ev1/outputs/01-step.log" ] || fail "a skipped step produced output"

# ---- execute: a step whose inputs are missing never runs -----------------
run 7 --tag "$TAG" --execute --repo "$FX" --from-step 5 --evidence-dir "$TMP/ev2"
grep_out "needs input"
grep_out "go-root"
grep -q '| 5 | agent | input-required | - |' "$TMP/ev2/run.md" || fail "missing-input step not recorded"
[ ! -e "$TMP/ev2/outputs/05-step.log" ] || fail "a missing-input step ran anyway"

# ... and the same step runs when the inputs are given (dry-run shape check
# only: the real build needs the SDK image and ~30 min, so this asserts the
# placeholder guard, not a build).
run 0 --tag "$TAG" --dry-run --repo "$FX" --go-root /tmp/go --out /tmp/out
grep_out "--go-root /tmp/go --out /tmp/out"
deny_out "<go-root>"

# ---- the signing dispatch's expected_sha256 comes from the build manifest --
OUTDIR="$TMP/out"; mkdir -p "$OUTDIR"
APKSHA=1111111111111111111111111111111111111111111111111111111111111111
cat > "$OUTDIR/build-manifest-A.json" <<JSON
{"artifacts":{"apk":{"sha256":"$APKSHA"},
              "payload":{"sha256":"2222222222222222222222222222222222222222222222222222222222222222"}}}
JSON
run 0 --tag "$TAG" --dry-run --repo "$FX" --out "$OUTDIR" --assets-dir "$TMP/assets" --go-root /tmp/go
grep_out "-f expected_sha256=$APKSHA"
grep_out "--apk-sha256 $APKSHA"
deny_out "<apk-sha256>"

# ---- execute refuses an agent step whose inputs are unset ----------------
run 7 --tag "$TAG" --execute --repo "$FX" --from-step 12 --evidence-dir "$TMP/ev3"
grep_out "STOP — step 12 needs input"
grep_out "<dir>"

echo "RELEASE-RUN-TEST-PASS"