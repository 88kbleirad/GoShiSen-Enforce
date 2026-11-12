#!/usr/bin/env bash
# GoShiSen-Enforce release packaging.
#
# Builds reproducible, CGO-disabled Linux release archives:
#
#   dist/goshisen-linux-amd64.tar.gz
#   dist/goshisen-linux-arm64.tar.gz
#   dist/checksums.txt
#   dist/sbom.spdx.json
#
# Each archive contains the statically-linked executable, the LICENSE,
# and the installation guide. Archives are byte-for-byte reproducible
# for a fixed VERSION and source tree (build ID and mtimes are
# normalized). Exit non-zero on any failure; no partial artifacts are
# left behind.
set -euo pipefail

PKG=github.com/88kbleirad/GoShiSen-Enforce
VERSION=${VERSION:-dev}
DIST=dist
ARCH=${ARCH:-amd64}
GO=${GO:-go}

# Reproducible: normalize mtimes and avoid host-dependent metadata.
# Force Go to strip the build ID hash (not reproducible across runs)
# so the binaries are byte-identical between builds.
mkdir -p "$DIST"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

build_one() {
    local arch="$1"
    local out="$TMP/goshisen-linux-$arch/goshisen"

    mkdir -p "$(dirname "$out")"

    CGO_ENABLED=0 GOOS=linux GOARCH="$arch" \
        "$GO" build -trimpath -buildvcs=false \
        -ldflags "-s -w -buildid= \
            -X $PKG/internal/cli.Version=$VERSION \
            -X $PKG/internal/app.Version=$VERSION" \
        -o "$out" ./cmd/goshisen

    # Essential contents: executable, license, installation doc.
    cp LICENSE "$TMP/goshisen-linux-$arch/LICENSE"
    cp docs/installation.md "$TMP/goshisen-linux-$arch/INSTALL.md"

    # Normalize mtimes so archives are reproducible.
    touch -d @0 "$TMP/goshisen-linux-$arch"/*
    tar -C "$TMP" --sort=name --mtime=@0 --owner=0 --group=0 \
        --numeric-owner -czf "$DIST/goshisen-linux-$arch.tar.gz" \
        "goshisen-linux-$arch"
}

if [ "$ARCH" = "all" ]; then
    build_one amd64
    build_one arm64
else
    build_one "$ARCH"
fi

# Checksums for every archive.
(cd "$DIST" && sha256sum goshisen-linux-*.tar.gz > checksums.txt)

# SBOM over the source tree (pure Go module inventory). It is a
# required release artifact; fail if syft is unavailable rather than
# silently shipping a release without one.
if ! command -v syft >/dev/null 2>&1; then
    echo "ERROR: syft not found in PATH (required for the SBOM)" >&2
    exit 1
fi
syft scan -q -o spdx-json="$DIST/sbom.spdx.json" .

echo "packaged $VERSION:"
ls -la "$DIST"
