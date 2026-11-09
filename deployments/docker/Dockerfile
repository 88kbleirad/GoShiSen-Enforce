# GoShiSen-Enforce daemon image.
#
# Design:
#   - multi-stage: Go builder -> nftables static builder -> runtime
#   - minimal runtime: distroless base (glibc only, no shell, no
#     package manager) with the Go binary + the nft binary. The nft
#     binary is statically linked against libmnl/libnftnl (only
#     glibc remains dynamic, which distroless base provides).
#   - capabilities: CAP_NET_ADMIN + CAP_NET_RAW are granted explicitly;
#     the container NEVER runs with --privileged
#   - health: the daemon exposes /healthz (liveness) and /readyz
#     (readiness) on the API port. The image ships no curl/wget, so
#     use a k8s httpGet probe (or a host-side probe against the mapped
#     port).
#   - shutdown: goshisen handles SIGTERM/SIGINT -> graceful queue stop.
#     The container stop signal is TERM; run the container with
#     `--stop-timeout` >= the graceful drain window (default 10s).
#   - recovery: restart under a supervisor or k8s restartPolicy. The
#     durable policy store (/var/lib/goshisen) survives restarts; on
#     restart the daemon reinstalls its nftables table (atomic
#     transaction) and re-binds NFQUEUE.
#
# The image is NOT self-sufficient for a firewall alone: the daemon
# applies nftables rules in ITS OWN network namespace and binds
# NFQUEUE there. For a lab deployment, attach the container to the lab
# netns (or run with the netns pre-configured). See scripts/lab.sh.

# ---- go build stage ----
FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/88kbleirad/GoShiSen-Enforce/internal/cli.Version=${VERSION} -X github.com/88kbleirad/GoShiSen-Enforce/internal/app.Version=${VERSION}" \
    -o /out/goshisen ./cmd/goshisen

# ---- nftables static build stage ----
FROM golang:1.25 AS nft-build
# Static nft (glibc) so the runtime needs no shared libs for nft.
# The nftables repo builds libnftables + the nft CLI. It needs
# libnftnl >= 1.3.2 (Debian bookworm ships 1.2.9), so build
# libnftnl and libmnl from source first; use built-in mini-gmp to
# drop the libgmp dependency and --without-cli to drop readline.
RUN apt-get update && apt-get install -y --no-install-recommends \
        autoconf automake bison flex gcc libtool make pkg-config \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /src
RUN git clone --depth 1 --branch libmnl-1.0.5 https://git.netfilter.org/libmnl \
    && cd libmnl \
    && ./autogen.sh \
    && ./configure --enable-static --disable-shared --prefix=/usr \
    && make -j"$(nproc)" \
    && make install
RUN git clone --depth 1 --branch libnftnl-1.3.2 https://git.netfilter.org/libnftnl \
    && cd libnftnl \
    && ./autogen.sh \
    && ./configure --enable-static --disable-shared --prefix=/usr \
    && make -j"$(nproc)" \
    && make install
RUN git clone --depth 1 --branch v1.1.7 https://git.netfilter.org/nftables \
    && cd nftables \
    && ./autogen.sh \
    && LDFLAGS="-static" ./configure --enable-static --disable-shared \
        --with-mini-gmp --without-cli --prefix=/usr \
        --disable-python --disable-doc --disable-man-doc \
    && make -j"$(nproc)" \
    && make install \
    && ! ldd /usr/sbin/nft | grep -E "mnl|nftnl|readline|jansson|gmp|json" \
    && /usr/sbin/nft --version

# ---- runtime stage ----
# distroless "base" (not "static") ships glibc, which the nft binary
# needs; it has no shell, no package manager, no extra utilities. The
# Go daemon itself is fully static. base-debian13 matches the
# golang:1.25 builder's glibc (2.41), so the statically-built nft's
# GLIBC_2.38+ requirements are satisfied.
FROM gcr.io/distroless/base-debian13
COPY --from=build /out/goshisen /usr/local/bin/goshisen
COPY --from=nft-build /usr/sbin/nft /usr/sbin/nft
COPY policies /policies
COPY configs /configs

# API and metrics ports (localhost bind inside the container; expose for
# host-side health checks when required).
EXPOSE 8080 9090

# Capabilities: the daemon needs NET_ADMIN (nftables + NFQUEUE) and
# NET_RAW. Everything else should be dropped. The container NEVER runs
# with --privileged. Recommended runtime flags:
#   --cap-drop=ALL --cap-add=NET_ADMIN --cap-add=NET_RAW
LABEL org.opencontainers.image.capabilities="NET_ADMIN,NET_RAW"

# The daemon requires a config file and policy file; pass them on the
# command line. The defaults here only set envs; `--config` is up to
# the operator (the sample config references `interface: eth0`, which
# will not exist in this container — supply a lab-appropriate config).
ENV GOSHISEN_POLICY_DIR=/var/lib/goshisen \
    GOSHISEN_API_PORT=8080 \
    GOSHISEN_METRICS_PORT=9090

VOLUME ["/var/lib/goshisen"]

# daemon init; TERM is the default container stop signal. The daemon
# exits 0 on SIGTERM/SIGINT after draining the queue.
STOPSIGNAL TERM

# The daemon is PID 1; it installs the hook and binds NFQUEUE itself.
ENTRYPOINT ["/usr/local/bin/goshisen"]
CMD ["--help"]
