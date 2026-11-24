# apk build recipe (committed inputs)

Everything here is a **build input** for the OpenWrt package, kept next to
the recipe it applies to so a release build is reviewable and re-runnable:

| File | What it is |
| --- | --- |
| `no-golang-host.patch` | The only change the release build makes to `openwrt/Makefile`: it comments out `PKG_BUILD_DEPENDS:=golang/host`, so the SDK does not build Go from source inside the container (~2 GB, and the first ENOSPC point on a small host). `Build/Compile` calls `go build` from `PATH`, which resolves to the pinned official go1.27.4 that `scripts/build-apk-release.sh` mounts at `/go-host`. Applied to a **scratch copy** only — the committed recipe keeps the dependency. |
| `sdk-x86_64.config` | The SDK config fragment used for the published v0.2.8 apk (x86/64, `CONFIG_ALL=n`, device-profile firmware blobs off, `CONFIG_PACKAGE_goshisen=m`). It was untracked before; it is the difference between "a build" and "the build". |

`scripts/build-apk-release.sh` uses both, records their sha256 (plus every
`openwrt/**` file) in `build-manifest-<label>.json`, and
`scripts/check-apk-build-manifest.sh` re-derives them from the commit — so a
manifest cannot claim a recipe the repository does not contain.

Provenance, honestly: the published v0.2.6–v0.2.8 apks were built with an
**uncommitted local copy** of the recipe that disabled the same dependency.
This directory is the same change, committed and hash-recorded; the v0.2.8
build evidence is in `docs/release/v0.2.8-build-evidence.md`, and the
reconstruction of that run (including the differences between its two build
scripts) is in `docs/release/v0.2.9-apk-build-recipe.md`.

Note the limits of this recipe, which the release notes must not overstate:
the build runs in the digest-pinned SDK container but uses a Go toolchain
**from outside** the SDK tree (mounted official go1.27.4), and the mirror
hash is toolchain-specific. "Byte-identical on repeated builds of the same
pin with the same toolchain and the same local recipe" is the claim this
recipe supports; "clean SDK build" is not.
