#!/usr/bin/env bash
# release-run-test.sh — regression test for scripts/release-run.sh (P3.11).
#
# Slice 1 contract: --dry-run prints every step with its exact commands and
# expected evidence, validates the tag through the S-003 guard, and mutates
# nothing — no tag, no directory, no working-tree change. --execute must fail
# closed (not implemented in this slice), never half-run the plan.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
RUN="$ROOT/scripts/release-run.sh"
TAG="v9.9.9"

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

# ---- fail-closed argument handling -------------------------------------
run 2
grep_out "missing --tag"

run 2 --tag "$TAG"
grep_out "one of --dry-run / --execute is required"

run 2 --tag "$TAG" --dry-run --execute
grep_out "usage:"

run 2 --tag "$TAG" --frobnicate

# S-003: the tag reaches shell commands later; it is rejected before use.
for bad in 'v1.0;touch /tmp/pwned' 'v1.0$(id)' 'v1.0`id`' 'v1.0|true' 'v1.0 0.2' 'v0.2' '1.2.3'; do
  run 4 --tag "$bad" --dry-run
  grep_out "does not match"
done

# --execute must refuse outright in this slice.
run 3 --tag "$TAG" --execute
grep_out "--execute is not implemented"
deny_out "── step"

# ---- the dry-run plan ---------------------------------------------------
evidence_default="$ROOT/evidence/release-run/$TAG-"
before_status="$(git -C "$ROOT" status --porcelain)"
before_tags="$(git -C "$ROOT" tag -l "$TAG")"

run 0 --tag "$TAG" --dry-run

# every step is present, in order, with its owner/gate lines
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

# the commands are concrete, not hand-waving
grep_out "bash scripts/release-pretag-check.sh --tag $TAG"
grep_out "git tag -a $TAG"
grep_out "gh run watch"
grep_out "scripts/build-apk-release.sh --commit <pin-commit>"
grep_out "check-apk-build-manifest.sh"
grep_out "GATE_S005_CHECKS=1 scripts/release-pin-gate.sh"
grep_out "-f mode=production"
grep_out "gh release upload $TAG"
grep_out "gh release edit $TAG --draft=false"
grep_out "scripts/verify-release-assets.sh --dir <dir>"
grep_out "542bb7ae9058fee9b4d37ed30b0af44f234ea7cb8866daa1ffc71a7bc37c839d"
grep_out "evidence/release-run/$TAG-"
grep_out "<pin-commit>"

# no unexpanded shell variable leaked into the printed commands
deny_out '$TAG'
deny_out '$VER'

# credential-gated steps are labelled, and the hard stops are stated
[ "$(grep -c 'owner:   maintainer' <<<"$OUT")" -eq 7 ] || fail "maintainer steps not labelled"
[ "$(grep -c 'owner:   agent' <<<"$OUT")" -eq 5 ] || fail "agent steps not labelled"
for stop in "step 1: pre-tag gate FAIL" "step 5: A/B hashes differ" "step 6: manifest or pin gate FAIL"; do
  grep_out "$stop"
done

# ---- it mutates nothing -------------------------------------------------
[ "$(git -C "$ROOT" tag -l "$TAG")" = "$before_tags" ] || fail "dry-run created tag $TAG"
[ "$(git -C "$ROOT" status --porcelain)" = "$before_status" ] || fail "dry-run changed the working tree"
if compgen -G "${evidence_default}*" >/dev/null; then
  fail "dry-run wrote evidence under evidence/release-run/"
fi

# --evidence-dir is honoured in the printed plan, still without creating it
run 0 --tag "$TAG" --dry-run --evidence-dir /tmp/release-run-test-evid
grep_out "/tmp/release-run-test-evid"
[ ! -e /tmp/release-run-test-evid ] || fail "dry-run created the evidence dir"

echo "RELEASE-RUN-TEST-PASS"