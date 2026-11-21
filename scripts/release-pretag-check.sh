#!/usr/bin/env bash
# GoShiSen-Enforce — release PRE-TAG pin/version agreement gate (F-01).
#
# Catches the defect that shipped the v0.2.6 tag: the tag's own
# openwrt/Makefile read PKG_VERSION:=0.2.5 while the tag was v0.2.6
# (the pin was fixed only by a POST-tag PR). A tag cannot contain its
# own hash, so this gate runs BEFORE the tag is created and proves the
# three claims that are checkable at that point:
#
#   1. PKG_VERSION (openwrt/Makefile) equals the scheduled tag version.
#   2. PKG_RELEASE is set (non-empty).
#   3. The Makefile being inspected is exactly the commit that will be
#      tagged (default: the working tree; with --commit, that commit).
#
# What this gate does NOT prove: PKG_SOURCE_VERSION == the tag commit
# (impossible by construction — the tag cannot contain its own hash).
# That post-tag property is covered by scripts/release-pin-gate.sh
# (ancestor/equal + empty source diff) once the apk exists.
#
# Usage:
#   scripts/release-pretag-check.sh --tag v0.2.7
#   scripts/release-pretag-check.sh --tag v0.2.7 --commit 8c53f1a
#
# Exit codes:
#   0  PASS
#   1  FAIL (one of the checks failed; the failing check is named)
#   2  usage
#   3  ENV (git unavailable)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# GATE_REPO overrides the repository the gate inspects (tests use a
# scratch git repo). Defaults to the checkout containing this script.
GATE_REPO="${GATE_REPO:-$ROOT}"
PKG_MAKEFILE="$GATE_REPO/openwrt/Makefile"

# Default target: the working tree, if the caller wants the exact
# commit, --commit overrides.
COMMIT=""
TAG=""

usage() {
  echo "usage: $0 --tag vX.Y.Z [--commit <sha>]" >&2
  exit 2
}

while [ $# -gt 0 ]; do
  case "$1" in
    --tag) TAG="$2"; shift 2 ;;
    --commit) COMMIT="$2"; shift 2 ;;
    *) usage ;;
  esac
done

[ -n "$TAG" ] || { echo "ERROR: --tag required" >&2; usage; }

command -v git >/dev/null 2>&1 || { echo "ENV: git required" >&2; exit 3; }

fail() { echo "FAIL: $*" >&2; exit 1; }
ok() { echo "OK: $*"; }

# ---------------------------------------------------------------------------
# Resolve the Makefile to inspect: the working tree (default) or the
# commit that will be tagged. The working-tree check is what CI runs on
# the pre-tag branch; --commit lets the maintainer check a specific
# commit (e.g. the tagged SHA) without checking it out.
# ---------------------------------------------------------------------------
TAG_VERSION="${TAG#v}"
[ "$TAG_VERSION" != "$TAG" ] || fail "tag must look like vX.Y.Z (got '$TAG')"

if [ -n "$COMMIT" ]; then
  REV="$COMMIT"
else
  REV="HEAD"
fi
PKG_MAKEFILE_REV="$(git -C "$GATE_REPO" rev-parse --verify --quiet "$REV:openwrt/Makefile")" || \
  fail "openwrt/Makefile not present at $REV (tag $TAG)"

# ---------------------------------------------------------------------------
# Extract package metadata from the Makefile.
# ---------------------------------------------------------------------------
pkg_var() { # pkg_var <name> <revision>
  git -C "$GATE_REPO" cat-file blob "$PKG_MAKEFILE_REV" \
    | sed -nE "s/^PKG_$1[[:space:]]*:=?[[:space:]]*(.*)/\1/p" \
    | head -n1
}

PKG_VERSION="$(pkg_var VERSION)"
PKG_RELEASE="$(pkg_var RELEASE)"

[ -n "$PKG_VERSION" ] || fail "PKG_VERSION empty in openwrt/Makefile at $REV"
[ -n "$PKG_RELEASE" ] || fail "PKG_RELEASE empty in openwrt/Makefile at $REV"

# ---------------------------------------------------------------------------
# 1. PKG_VERSION equals the scheduled tag version.
# ---------------------------------------------------------------------------
[ "$PKG_VERSION" = "$TAG_VERSION" ] || \
  fail "PKG_VERSION=$PKG_VERSION does not match tag version $TAG_VERSION (tag $TAG)"
ok "PKG_VERSION=$PKG_VERSION matches tag version $TAG_VERSION"

# ---------------------------------------------------------------------------
# 2. PKG_RELEASE is set.
# ---------------------------------------------------------------------------
ok "PKG_RELEASE=$PKG_RELEASE is set"

# ---------------------------------------------------------------------------
# 3. The Makefile is at the tag candidate commit: the working tree must
#    be exactly a commit (nothing uncommitted), or the commit given with
#    --commit.
# ---------------------------------------------------------------------------
if [ -n "$COMMIT" ]; then
  COMMIT_SHA="$(git -C "$GATE_REPO" rev-parse --verify --quiet "$COMMIT^{commit}")" || \
    fail "commit $COMMIT not found in this repository"
  ok "checked openwrt/Makefile at commit $COMMIT_SHA (tag $TAG)"
else
  DIRTY="$(git -C "$GATE_REPO" status --porcelain)"
  if [ -n "$DIRTY" ]; then
    echo "$DIRTY" >&2
    fail "working tree is not a commit ($(printf '%s\n' "$DIRTY" | wc -l | tr -d ' ') path(s) uncommitted); commit or stash before tagging $TAG"
  fi
  ok "working tree is the tag candidate commit ($(git -C "$GATE_REPO" rev-parse --short HEAD))"
fi

# Informational: a tag of the same name may already exist (pre-tag check
# re-run after the tag was created, or the post-tag pin PR state).
if EXISTING="$(git -C "$GATE_REPO" rev-parse --verify --quiet "refs/tags/$TAG^{commit}")"; then
  echo "NOTE: tag $TAG already exists at $(git -C "$GATE_REPO" rev-parse --short "$EXISTING")"
fi

echo "PASS: pre-tag pin/version agreement gate passed (tag $TAG)"
