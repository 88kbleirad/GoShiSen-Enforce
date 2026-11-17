#!/usr/bin/env bash
# validate-release-tag.sh — fail-closed release tag name gate (S-003).
#
# Git ref names may contain shell metacharacters (';', '$()', backticks,
# quotes, pipes). The release workflows interpolate the tag name into
# shell commands (make VERSION=..., --tag ...), so a specially-named
# pushed tag could otherwise execute commands in a job that holds the
# release signing key. This script rejects every name that is not
# exactly v<major>.<minor>.<patch> BEFORE the name is used anywhere.
#
# The regex here is the single source of truth; the release workflow's
# pre-checkout validation step (release.yml) uses the identical pattern.
#
# Usage: bash scripts/validate-release-tag.sh <tag>
#   exit 0: tag matches ^v[0-9]+\.[0-9]+\.[0-9]+$
#   exit 1: anything else (empty, missing, metacharacters, prefix/suffix)
set -euo pipefail

tag="${1:-}"

if [[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "OK: release tag '$tag' matches ^v[0-9]+\.[0-9]+\.[0-9]+$"
  exit 0
fi

echo "FAIL: release tag '$tag' does not match ^v[0-9]+\.[0-9]+\.[0-9]+$; rejected before use" >&2
exit 1
