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
PKG_RELEASE:=1

PKG_SOURCE_PROTO:=git
PKG_SOURCE_URL:=https://github.com/88kbleirad/GoShiSen-Enforce.git
PKG_SOURCE_VERSION:=b53ce5813eb8e5951cea97c8830da2df0e1dff43
# sha256 of the GitHub-generated tarball of the pinned commit; lets the
# SDK verify the download (mirror hash) when cloning.
PKG_MIRROR_HASH:=cccc5ce10c003271737e0b5f70cc578de01f69e1c3ba935a3ba396f78d0f8b10
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
	$(INSTALL_DIR) $(1)/etc/config
	$(INSTALL_DATA) ./files/etc/config/goshisen $(1)/etc/config/goshisen
	$(INSTALL_DIR) $(1)/etc/goshisen
	$(INSTALL_DATA) ./files/etc/goshisen/policy.yaml $(1)/etc/goshisen/policy.yaml
endef

# Do not auto-enable at boot during installation.
define Package/goshisen/postinst
	[ -z "$${IPKG_INSTROOT}" ] && /etc/init.d/goshisen enabled || true
endef

define Package/goshisen/prerm
	# On uninstall the app-owned nftables table must be removed: the
	# daemon's queue rule would otherwise stay installed with no
	# consumer and keep blocking forwarded traffic (the kernel drops
	# packets queued to a queue without a listener).
	if [ -z "$${IPKG_INSTROOT}" ]; then
		if [ -x /usr/sbin/goshisen ]; then
			/usr/sbin/goshisen hook cleanup || true
		fi
		/etc/init.d/goshisen stop || true
	fi
endef

$(eval $(call BuildPackage,goshisen))
