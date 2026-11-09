# Security Policy

## Project status

GoShiSen-Enforce is under active development (Phase 1 — foundation). The daemon currently installs no firewall hook and makes no system changes; it only reads configuration and performs read-only host checks. Treat it as **not yet suitable for production or for protecting real networks**.

## Reporting a vulnerability

Report suspected vulnerabilities privately to the maintainer, **not** via a public issue:

- Open a [private security advisory](https://github.com/88kbleirad/GoShiSen-Enforce/security/advisories/new) on GitHub, or
- contact the maintainer listed in `LICENSE` / the repository owner.

Include: affected version or commit, a description, reproduction steps, and the impact you believe it has. We aim to acknowledge reports within a few days.

## Scope notes for reviewers

- The daemon is intended to run as **root** in a Linux network namespace (later phases); privilege boundaries and NFQUEUE verdict handling are security-relevant.
- Fail-closed behavior is a core property: a stopped or crashed engine must result in dropped traffic, never bypassed traffic.
- Reports about the design (not only the code) are welcome against `docs/design/architecture.md`.

## Supported versions

None yet: no releases have been made. Once the first tagged release exists, this section will list supported versions.
