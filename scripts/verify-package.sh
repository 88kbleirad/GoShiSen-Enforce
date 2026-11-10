#!/usr/bin/env bash
# Validate built release archives.
#
# Reads dist/goshisen-linux-*.tar.gz (built by scripts/package.sh) and
# verifies:
#   - archive contains goshisen + LICENSE + INSTALL.md
#   - executable architecture matches its name
#   - executable is statically linked
#   - CLI version output equals VERSION
#   - checksums.txt matches the archives
#   - SBOM exists and is parseable SPDX JSON
set -euo pipefail

DIST=dist
VERSION=${VERSION:-dev}
ARCH=${ARCH:-amd64}

for arch in amd64 arm64; do
    [ -f "$DIST/goshisen-linux-$arch.tar.gz" ] || {
        [ "$arch" = "arm64" ] && [ "${ARCH:-}" = "amd64" ] && continue
        echo "ERROR: missing $DIST/goshisen-linux-$arch.tar.gz" >&2
        exit 1
    }
done

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

for arch in amd64 arm64; do
    [ -f "$DIST/goshisen-linux-$arch.tar.gz" ] || continue
    tar xzf "$DIST/goshisen-linux-$arch.tar.gz" -C "$TMP"

    # 1. Contents.
    for f in goshisen LICENSE INSTALL.md; do
        [ -f "$TMP/goshisen-linux-$arch/$f" ] || {
            echo "ERROR: archive missing $f" >&2; exit 1
        }
    done

    # 2. Architecture and linkage.
    file "$TMP/goshisen-linux-$arch/goshisen" | grep -q "statically linked" || {
        echo "ERROR: not statically linked" >&2; exit 1
    }
    case "$arch" in
        amd64) file "$TMP/goshisen-linux-$arch/goshisen" | grep -q "x86-64" || { echo "ERROR: wrong arch" >&2; exit 1; } ;;
        arm64) file "$TMP/goshisen-linux-$arch/goshisen" | grep -q "aarch64" || { echo "ERROR: wrong arch" >&2; exit 1; } ;;
    esac

    # 3. Version. Execute directly when the arch matches the host; for
    # cross-built binaries verify the stamped string via `strings` (the
    # version is baked in by ldflags; execution there requires qemu and
    # is out of scope for host-side verification).
    expected="goshisen $VERSION"
    hostarch=$(uname -m)
    case "$hostarch" in
        x86_64) hostarch=amd64 ;;
        aarch64|arm64) hostarch=arm64 ;;
    esac
    if [ "$arch" = "$hostarch" ]; then
        actual=$("$TMP/goshisen-linux-$arch/goshisen" --version)
        [ "$actual" = "$expected" ] || {
            echo "ERROR: version $actual != expected $expected" >&2; exit 1
        }
        echo "OK $arch: $actual"
    else
        # `grep -q` exits on first match, SIGPIPE-ing `strings`; under
        # pipefail that would fail the pipeline. Capture instead.
        found=$(strings "$TMP/goshisen-linux-$arch/goshisen" | grep -F -- "$VERSION" || true)
        [ -n "$found" ] || {
            echo "ERROR: version $VERSION not found in $arch binary" >&2; exit 1
        }
        echo "OK $arch (version stamp present; execution requires a $arch host)"
    fi
done

# 4. Checksums.
(cd "$DIST" && grep -E 'goshisen-linux-(amd64|arm64)\.tar\.gz' checksums.txt | sha256sum -c -)

# 5. SBOM.
[ -s "$DIST/sbom.spdx.json" ] || { echo "ERROR: missing SBOM" >&2; exit 1; }
jq -e '.spdxVersion == "SPDX-2.3" and ((.packages | length) > 0)' "$DIST/sbom.spdx.json" >/dev/null || {
    echo "ERROR: SBOM invalid" >&2; exit 1
}
echo "OK checksums + SBOM"
