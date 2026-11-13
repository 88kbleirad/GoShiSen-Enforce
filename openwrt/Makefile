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
PKG_VERSION:=0.2.0
PKG_RELEASE:=4

PKG_SOURCE_PROTO:=git
PKG_SOURCE_URL:=https://github.com/88kbleirad/GoShiSen-Enforce.git
PKG_SOURCE_VERSION:=f5f211a173dab2934a42e3aa8220ab2d54fa12da
# sha256 of the source tarball the SDK produces from a git clone of the
# pinned commit (PKG_SOURCE_PROTO=git); lets the SDK verify the download
# (mirror hash). Compute with a clean clone: git clone <url>; git checkout
# <pin>; tar --zstd -cf <name>-<ver>.tar.zst <dir>; sha256sum.
PKG_MIRROR_HASH:=f922e983f58976fc7d101a46f8f2fad4086d1b2a38c14ae5e2bdc19770e09b2d
PKG_SOURCE_SUBDIR:=$(PKG_NAME)-$(PKG_VERSION)

# Go builds are handled outside OpenWrt's gcc toolchain.
PKG_BUILD_DEPENDS:=golang/host
PKG_USE_MIPS16:=0
PKG_BUILD_PARALLEL:=1

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
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
		go build -trimpath -buildvcs=false \
		-ldflags "-s -w -buildid= \
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
	if [ -z "$${IPKG_INSTROOT}" ]; then
		/etc/init.d/goshisen enabled || true
		[ -x /etc/init.d/goshisen-boot ] && /etc/init.d/goshisen-boot enable || true
	fi
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
