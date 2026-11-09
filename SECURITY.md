# Security Policy

## Project status

GoShiSen-Enforce is a lab firewall under active development (Phase 7 — hardening/release). It implements policy enforcement over NFQUEUE with kernel-side conntrack state and nftables hooks, a localhost REST management API, and bounded-cardinality Prometheus metrics. It is **not yet officially released**; treat it as not-yet-suitable for protecting real production networks until the first tagged release. Tests requiring root (privileged integration suite) are gated and never falsely reported as passing.

## Reporting a vulnerability

Report suspected vulnerabilities privately to the maintainer, **not** via a public issue:

- Open a [private security advisory](https://github.com/88kbleirad/GoShiSen-Enforce/security/advisories/new) on GitHub, or
- contact the maintainer listed in `LICENSE` / the repository owner.

Include: affected version or commit, a description, reproduction steps, and the impact you believe it has. We aim to acknowledge reports within a few days.

## Scope notes for reviewers

- The daemon runs as **root** (or with `CAP_NET_ADMIN`/`CAP_NET_RAW`) inside a dedicated network namespace; it modifies only the app-owned nftables table `ip goshisen` in the current namespace and never touches the host firewall.
- Fail-closed behavior is a core property: a stopped, crashed, or overloaded engine (queue overflow) must result in dropped traffic, never bypassed traffic. The backend never sets `NFQA_CFG_F_FAIL_OPEN`.
- The API binds to loopback by default; administrative mutations require a bearer token that is never accepted via argv, never logged, and redacted in `config show`.
- Metric labels are bounded enums; no IPs, ports, or flow IDs appear as labels.
- Reports about the design (not only the code) are welcome against `docs/design/architecture.md`.

## Supported versions

None yet: no releases have been made. Once the first tagged release exists, this section will list supported versions.
