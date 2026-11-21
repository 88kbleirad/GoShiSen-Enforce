#!/usr/bin/env bash
# release-pretag-check-test.sh — F-01 regression tests for the pre-tag
# pin/version agreement gate (scripts/release-pretag-check.sh).
#
# Each test builds a scratch git repository with a synthetic
# openwrt/Makefile and runs the gate against it (GATE_REPO points at
# the scratch repo). This keeps the gate deterministic and independent
# of the live repository's history.
#
# The gate must fail for the right reason in each negative case (wrong
# version, missing release, dirty tree, missing Makefile) and pass in
# the matching cases (clean tree at the tag version; --commit pointing
# at the tag candidate commit).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
GATE="$ROOT/scripts/release-pretag-check.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }

# new_repo <version> <release> creates a scratch repo at a clean commit
# carrying openwrt/Makefile with the given PKG_VERSION/PKG_RELEASE.
# Prints the repo dir.
new_repo() {
  local version="$1" release="$2"
  local dir="$TMP/repo-$(date +%s%N)-$RANDOM"
  mkdir -p "$dir/openwrt"
  printf 'PKG_NAME:=goshisen\nPKG_VERSION:=%s\nPKG_RELEASE:=%s\n' "$version" "$release" > "$dir/openwrt/Makefile"
  git -C "$dir" init -q -b main
  git -C "$dir" config user.email test@example.com
  git -C "$dir" config user.name "Gate Test"
  git -C "$dir" config commit.gpgsign false
  git -C "$dir" add openwrt/Makefile
  git -C "$dir" commit -q -m makefile
  printf '%s' "$dir"
}

# run_gate <repo> [args...] runs the gate against the scratch repo.
run_gate() {
  local repo="$1"; shift
  (cd "$repo" && GATE_REPO="$repo" bash "$GATE" "$@")
}

# --- positive: clean tree at the tag version passes ---
POS="$(new_repo 0.2.7 1)"
run_gate "$POS" --tag v0.2.7 >/dev/null \
  || fail "matching version/clean tree was REJECTED"

# --- negative: wrong PKG_VERSION (the v0.2.6 defect) must fail ---
WRONG="$(new_repo 0.2.6 1)"
if run_gate "$WRONG" --tag v0.2.7 >/dev/null 2>&1; then
  fail "PKG_VERSION=0.2.6 with tag v0.2.7 was ACCEPTED"
fi

# --- negative: missing PKG_RELEASE must fail ---
NORELEASE="$(mktemp -d -p "$TMP" norelease-XXXXXX)"
mkdir -p "$NORELEASE/openwrt"
printf 'PKG_NAME:=goshisen\nPKG_VERSION:=0.2.7\n' > "$NORELEASE/openwrt/Makefile"
git -C "$NORELEASE" init -q -b main
git -C "$NORELEASE" config user.email test@example.com
git -C "$NORELEASE" config user.name "Gate Test"
git -C "$NORELEASE" config commit.gpgsign false
git -C "$NORELEASE" add openwrt/Makefile
git -C "$NORELEASE" commit -q -m makefile
if run_gate "$NORELEASE" --tag v0.2.7 >/dev/null 2>&1; then
  fail "missing PKG_RELEASE was ACCEPTED"
fi

# --- negative: dirty working tree must fail ---
DIRTY="$(new_repo 0.2.7 1)"
echo "stray" > "$DIRTY/note.txt"
if run_gate "$DIRTY" --tag v0.2.7 >/dev/null 2>&1; then
  fail "dirty working tree was ACCEPTED"
fi
rm "$DIRTY/note.txt"

# --- negative: non-semver tag must fail ---
BADTAG="$(new_repo 0.2.7 1)"
if run_gate "$BADTAG" --tag v0.2.7.0 >/dev/null 2>&1; then
  fail "malformed tag v0.2.7.0 was ACCEPTED"
fi

# --- positive: --commit pointing at the tag candidate passes even with a
# --- dirty working tree (e.g. a post-tag re-run from a different dir) ---
POS2="$(new_repo 0.2.7 1)"
COMMIT_SHA="$(git -C "$POS2" rev-parse -q HEAD)"
echo "stray" > "$POS2/note.txt"
run_gate "$POS2" --tag v0.2.7 --commit "$COMMIT_SHA" >/dev/null \
  || fail "--commit at the tag candidate was REJECTED"

# --- negative: --commit that does not contain the Makefile must fail ---
NOCOMMIT="$(mktemp -d -p "$TMP" nocommit-XXXXXX)"
git -C "$NOCOMMIT" init -q -b main
git -C "$NOCOMMIT" config user.email test@example.com
git -C "$NOCOMMIT" config user.name "Gate Test"
git -C "$NOCOMMIT" config commit.gpgsign false
echo "no makefile" > "$NOCOMMIT/readme.md"
git -C "$NOCOMMIT" add readme.md
git -C "$NOCOMMIT" commit -q -m readme
if run_gate "$NOCOMMIT" --tag v0.2.7 >/dev/null 2>&1; then
  fail "repo without openwrt/Makefile was ACCEPTED"
fi

echo "RELEASE-PRETAG-CHECK-GATE-GUARD-PASS"
