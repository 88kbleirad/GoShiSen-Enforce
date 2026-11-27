# GoShiSen-Enforce OpenWrt package.
#
# Build with the OpenWrt SDK:
#
#   make package/goshisen/compile V=s \
#     OPENWRT_SDK=$PWD/openwrt-sdk-.../staging_dir/toolchain-.../bin
#
# Or install this directory as a feed (feeds.conf) in the SDK and use
# the standard buildroot flow.

include $(TOPDIR)/rules.mk

PKG_NAME:=goshisen
PKG_VERSION:=0.2.9
# Package version is independent of the CLI release, but for this release
# it is aligned with the CLI tag (v0.2.9). The daemon source is pinned by
# PKG_SOURCE_VERSION below (the v0.2.9 tag commit; set by the post-tag pin
# PR after the tag exists — a tag cannot contain its own hash).
# PKG_RELEASE=1 restarts the package release counter for the new
# PKG_VERSION (0.2.9-r1 > 0.2.8-r1 under apk ordering).
PKG_RELEASE:=1
# NOTE: PKG_SOURCE_VERSION / PKG_MIRROR_HASH are deliberately NOT bumped in
# this pre-tag PR. They are the POST-tag pin PR's job (pin = the v0.2.9 tag
# commit + derived mirror hash), so that the tagged commit's Makefile
# already carries PKG_VERSION:=0.2.9 — what scripts/release-pretag-check.sh
# (F-01) demands before the tag is created.

PKG_SOURCE_PROTO:=git
PKG_SOURCE_URL:=https://github.com/88kbleirad/GoShiSen-Enforce.git
# Daemon source SHA = the v0.2.9 tag commit (8e2ae79, tag object aca4823),
# set by the post-tag pin PR after the tag exists.
# This is the ONLY daemon source pin; the packaging recipe lives in this
# checkout (Makefile + files/) and is versioned independently.
PKG_SOURCE_VERSION:=8e2ae79f6800188d621d292d2311be75f1f2d00a
# sha256 of the source tarball the SDK produces from a git clone of the
# pinned commit (PKG_SOURCE_PROTO=git); lets the SDK verify the download
# (mirror hash). Derived with the committed recipe in the digest-pinned SDK
# container (scripts/build-apk-release.sh, zero-hash two-pass): pass 1
# reports `got <hash>`, pass 2 exits 0 with sha256sum of
# dl/goshisen-0.2.9.tar.zst (516,776 B) equal to the pin.
PKG_MIRROR_HASH:=590c129f01fcc0fe0fc814b960b140dcc0545ff8758e383ee13cf9acb2e16a64
PKG_SOURCE_SUBDIR:=$(PKG_NAME)-$(PKG_VERSION)

# Go binary architecture for the SDK target (amd64 for x86/64, arm64 for
# armsr/armv8). The SDK's own toolchain is not used for the Go build;
# this only selects GOARCH.
PKG_GOARCH ?= amd64

# Go builds are handled outside OpenWrt's gcc toolchain.
PKG_BUILD_DEPENDS:=golang/host
PKG_USE_MIPS16:=0
PKG_BUILD_PARALLEL:=1

STRIP:=true
include $(INCLUDE_DIR)/package.mk

define Package/goshisen
	SECTION:=net
	CATEGORY:=Network
	TITLE:=GoShiSen-Enforce — fail-closed NFQUEUE firewall daemon
	URL:=https://github.com/88kbleirad/GoShiSen-Enforce
	DEPENDS:=+kmod-nfnetlink-queue +kmod-nft-queue +kmod-nf-conntrack +kmod-nf-conntrack-netlink +nftables
	PROVIDES:=goshisen
endef

define Package/goshisen/description
	GoShiSen-Enforce is a fail-closed, stateful packet filtering daemon
	that hooks the router's forward path AFTER firewall4 using NFQUEUE.
	It never replaces firewall4; it only adds an extra enforcement
	layer. The service is disabled by default.
endef

define Build/Compile
	cd $(PKG_BUILD_DIR) && \
	CGO_ENABLED=0 GOOS=linux GOARCH=$(PKG_GOARCH) \
		go build -trimpath -buildvcs=false \
		-ldflags "-s -w \
			-X github.com/88kbleirad/GoShiSen-Enforce/internal/cli.Version=$(PKG_VERSION) \
			-X github.com/88kbleirad/GoShiSen-Enforce/internal/app.Version=$(PKG_VERSION)" \
		-o $(PKG_BUILD_DIR)/goshisen ./cmd/goshisen
endef

define Package/goshisen/install
	$(INSTALL_DIR) $(1)/usr/sbin
	$(INSTALL_BIN) $(PKG_BUILD_DIR)/goshisen $(1)/usr/sbin/goshisen
	$(INSTALL_DIR) $(1)/etc/init.d
	$(INSTALL_BIN) ./files/etc/init.d/goshisen $(1)/etc/init.d/goshisen
	$(INSTALL_BIN) ./files/etc/init.d/goshisen-boot $(1)/etc/init.d/goshisen-boot
	$(INSTALL_DIR) $(1)/etc/config
	$(INSTALL_DATA) ./files/etc/config/goshisen $(1)/etc/config/goshisen
	$(INSTALL_DIR) $(1)/etc/goshisen
	$(INSTALL_DATA) ./files/etc/goshisen/policy.yaml $(1)/etc/goshisen/policy.yaml
	$(INSTALL_DIR) $(1)/lib/goshisen
	$(INSTALL_DATA) ./files/lib/goshisen-lifecycle.sh $(1)/lib/goshisen/goshisen-lifecycle.sh
endef

# The daemon service stays disabled at install. The pre-firewall boot
# guard is linked (so it runs early) but is a no-op unless the service
# is enabled (UCI goshisen.main.enabled=1), so a disabled install never
# blocks forwarding.
define Package/goshisen/postinst
	# On upgrade (PKG_UPGRADE=1) apk runs this body after
	# `default_postinst`. That loop calls `start` on each init script,
	# but procd's `start` is a no-op for an already-running instance —
	# so without this the OLD daemon binary keeps running the deleted
	# inode until a manual restart (issue #57). Restart here instead:
	# rc.common `restart` = `stop` + `start`. During the gap the
	# kernel NFQUEUE rule persists with no listener and kernel-drops
	# (fail-closed); the daemon's start reinstalls the hook atomically
	# (delete+add, one transaction) before/at bind. Never stop without
	# immediately starting; a failed restart is loud but must not
	# abort the apk transaction (the old process keeps enforcing).
	if [ -z "$${IPKG_INSTROOT}" ]; then
		/etc/init.d/goshisen enabled || true
		[ -x /etc/init.d/goshisen-boot ] && /etc/init.d/goshisen-boot enable || true
		if [ "$${PKG_UPGRADE:-0}" = "1" ] && /etc/init.d/goshisen enabled 2>/dev/null; then
			/etc/init.d/goshisen restart || \
				echo "goshisen: upgrade restart failed (manual /etc/init.d/goshisen restart required)" >&2
		fi
	fi
endef

define Package/goshisen/postinst-notify-listener-defaults
# (proposal, NOT merged — F-02 listener-defaults upgrade notice)
# A router upgrading from <= v0.2.6 with the packaged `option api_port
# '0'` / `metrics_port '0'` had BOTH loopback listeners listening
# (those releases ignored a file/flag 0). v0.2.7 kills them. One-time
# notice on the first post-upgrade start:
#   - `apk` postinst on PKG_UPGRADE=1 writes /var/run/goshisen/.upgraded-to-027;
#   - the init script's start() logs a daemon.warn once (marker file)
#     when it renders both ports 0: listeners are off; to re-enable
#     set `uci set goshisen.main.api_port='8080'` and
#     `metrics_port='9090'` (+ restart).
endef

define Package/goshisen/prerm
	# On uninstall OR downgrade the app-owned nftables table must be
	# removed and the package-owned rc.d symlinks cleaned unconditionally
	# (the init scripts are package files removed by apk, but the rc.d
	# links are runtime state created by `enable` and would otherwise
	# dangle when a downgrade to a release that does not ship the script
	# happens). The cleanup never follows symlinks, never does recursive
	# deletion, and only touches paths owned by this package.
	if [ -z "$${IPKG_INSTROOT}" ]; then
		if [ -x /usr/sbin/goshisen ]; then
			/usr/sbin/goshisen hook cleanup || true
		fi
		/etc/init.d/goshisen stop || true
		/etc/init.d/goshisen disable || true
		if [ -x /etc/init.d/goshisen-boot ]; then
			/etc/init.d/goshisen-boot stop || true
			/etc/init.d/goshisen-boot disable || true
		fi
		# Unconditional: remove package-owned rc.d links even when the
		# target init script is already gone (downgrade case).
		if [ -r /lib/goshisen/goshisen-lifecycle.sh ]; then
			. /lib/goshisen/goshisen-lifecycle.sh
			goshisen_remove_links || {
				echo "goshisen: prerm could not clean rc.d links" >&2
				exit 1
			}
		fi
	fi
endef

$(eval $(call BuildPackage,goshisen))
