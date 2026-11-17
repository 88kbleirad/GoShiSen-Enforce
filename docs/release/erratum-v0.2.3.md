# Errata — v0.2.3 release description (DRAFT, not yet applied)

**Status:** draft for the maintainer to paste into the v0.2.3 GitHub
release description. Nothing here has been applied to the release; the
maintainer edits the release body directly.

---

## Errata — v0.2.3 release description

Two corrections to the v0.2.3 release notes:

### 1. Package source pin

The v0.2.3 apk (`goshisen-0.2.3-r1.apk`) was built from source commit
`c5de6e8ec52a55b84ed16bdb3c9e5cff654f4dc6` (the v0.2.2 tag commit), not
from the v0.2.3 tag commit itself. The runtime source is **byte-identical**
between the two commits (`git diff --stat c5de6e8 8b55953 -- cmd internal
pkg go.mod go.sum` is empty) — the release is packaging/signing-only —
but the version label (0.2.3) and the pinned source commit (v0.2.2) do
not name the same commit. Tracked in issue #49; the v0.2.4 release
pins to its own tag commit and adds a gate
(`scripts/release-pin-gate.sh`) proving source equivalence and label
agreement.

### 2. Stateful IPv6 "limitation" withdrawn

The v0.2.3 notes stated that stateful IPv6 ACCEPT relies on kernel
conntrack CT-attribute delivery and is INVALID → DROP (fail-closed) when
unavailable. Investigation (issue #5, PR #50) on OpenWrt 25.12.6 x86/64
(kernel 6.12.112) shows the earlier IPv6-stateful failure was a
**policy-rule-direction artifact**: a direction-specific established
rule dropped the client's original-direction established ACK/data.
`NFQA_CT_INFO` **is** delivered on IPv6 queue 101, and stateful IPv6
ACCEPT **completes end-to-end** (HTTP 200) with the canonical
bidirectional `ESTABLISHED/RELATED` rule pattern. This limitation
wording is **withdrawn**.
