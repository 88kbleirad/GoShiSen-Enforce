# Container Feasibility Evaluation

Status: **DEFERRED** — no container image is published in this phase.
A Dockerfile exists (`deployments/docker/Dockerfile`) and documents the
intended runtime shape, but a containerized firewall does not by itself
protect host traffic, and no container runtime was available in this
environment to validate a live deployment.

## Summary

| Question | Answer |
|---|---|
| Does a containerized daemon protect host traffic by default? | **No.** The daemon installs its nftables hook (and binds NFQUEUE) in the network namespace it runs in. A container on the default bridge network is isolated in its own netns; its hook sees only the container's traffic. |
| Can it be made to protect host traffic? | Only with `--network=host` (shares the host netns). Then the daemon's rules apply to the whole host — a high-risk operation that must be treated as running the firewall directly on the host, not as an isolation boundary. |
| Rootless feasible? | No. NFQUEUE binding and nftables require `CAP_NET_ADMIN` (and `CAP_NET_RAW`), which rootless containers cannot obtain. |
| Required capabilities | `CAP_NET_ADMIN`, `CAP_NET_RAW` (via `--cap-drop=ALL --cap-add=NET_ADMIN --cap-add=NET_RAW`). Never `--privileged`. |
| Kernel requirement | NFQUEUE + conntrack must be enabled in the **host** kernel even for a container deployment: the netlink subsystem is not namespaced for the netfilter queue; plus the container needs `/proc/net/netfilter/nfnetlink_queue` (namespace-scoped, provided by the host kernel). |
| Isolation boundaries | The container provides process/filesystem isolation, not network isolation for the host. The lab netns flow (`scripts/lab.sh`) remains the correct isolation model for validation. |

## Analysis

### NFQUEUE from containers
- The daemon uses `netlink.Dial(unix.NETLINK_NETFILTER)` — a socket,
  not a device. It works inside a container's netns provided the host
  kernel has NFQUEUE (`CONFIG_NETFILTER_NETLINK_QUEUE` or the
  `nfnetlink_queue` module) and the process holds `CAP_NET_ADMIN`.
- The queue it binds is a kernel-global resource keyed by queue number;
  the queue rules are installed via nftables **in the container's
  netns**. A queue rule in a container's netns does not capture host
  traffic.

### nftables and network namespaces
- The daemon installs its table in its own netns. With default bridge
  networking the container's netns receives only forwarded/NAT'd
  container traffic, and the hook's `forward` chain (priority filter)
  sees only traffic crossing that netns.
- `--network=host` puts the daemon in the host netns. The hook then
  applies to host traffic. This makes the container a thin packaging
  wrapper around a host firewall — the deployment risk is equivalent
  to installing the binary directly on the host.

### Host versus container network behavior
- Host mode: the daemon sees all host forwarded traffic; correct for a
  firewall, but no security boundary between the daemon and the host.
- Container mode: isolation is real, but the daemon protects only its
  own netns — not the host, and not other containers unless the
  network is shaped so the daemon's netns is on the forwarding path
  (a lab-router topology).

### Risks introduced by host networking
- A bug/misconfiguration in the container affects the whole host.
- The container must be run with bounded capabilities and read-only
  rootfs; otherwise a compromised daemon (or host-lib compromise)
  gains host-wide netfilter control.

## Recommendation

- Do not advertise a container image as a host firewall in this
  release. Document that the OCI image is for deployment **inside** a
  pre-configured network namespace (e.g. the lab router netns), not as
  a standalone host protection boundary.
- Keep the Dockerfile as an experimental artifact; the primary
  supported deployment remains the native binary + systemd unit (see
  `docs/installation.md`).
- If a container image is ever published, it must be built from the
  tagged source, runtime-validated in the lab netns with the same
  security gates as the native binary, and published only with explicit
  authorization.
