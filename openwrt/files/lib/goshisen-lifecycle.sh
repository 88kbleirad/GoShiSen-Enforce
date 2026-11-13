# GoShiSen-Enforce — OpenWrt package lifecycle helpers.
#
# Source from prerm/postinst snippets in the package Makefile. Provides
# deterministic, idempotent cleanup of the runtime-created rc.d symlinks
# owned by this package (the init scripts themselves are package files
# removed by apk; the rc.d links are runtime state and must be removed
# here).
#
# Safety rules:
#   - only touches /etc/rc.d/S12goshisen-boot, K10goshisen-boot,
#     S90goshisen, K10goshisen — nothing else;
#   - never follows symlinks (unlink only), never rm -rf;
#   - only removes links whose target is within ../init.d/goshisen*
#     (a foreign file/symlink with the same name is left alone and
#     reported as an error rather than removed);
#   - idempotent: absent links are success, repeated runs are clean;
#   - fails loudly (exit 1) when a link cannot be verified/removed.

# goshisen_rc_links prints the package-owned rc.d link paths.
goshisen_rc_links() {
	local root="${GOSHISEN_RC_ROOT:-/etc/rc.d}"
	echo "$root/S12goshisen-boot"
	echo "$root/K10goshisen-boot"
	echo "$root/S90goshisen"
	echo "$root/K10goshisen"
}

# goshisen_link_target_ok returns 0 when the path is a symlink whose
# target stays inside ../init.d/goshisen* (i.e. the package's own init
# scripts). A foreign file/symlink is not ours.
goshisen_link_target_ok() {
	local link=$1 target
	[ -L "$link" ] || return 1
	target=$(readlink "$link" 2>/dev/null) || return 1
	case "$target" in
		../init.d/goshisen*) return 0 ;;
		*) return 1 ;;
	esac
}

# goshisen_remove_links removes the package-owned rc.d symlinks.
# Returns 0 on success (including absent links); returns 1 if a link
# exists with a foreign target (nothing removed for that path) so the
# caller can fail the package operation without touching others' state.
goshisen_remove_links() {
	local link rc=0
	for link in $(goshisen_rc_links); do
		if [ -e "$link" ] || [ -L "$link" ]; then
			if goshisen_link_target_ok "$link"; then
				unlink "$link" 2>/dev/null || {
					echo "goshisen: cannot remove $link" >&2
					rc=1
				}
			else
				echo "goshisen: $link exists but is not a package-owned symlink; refusing" >&2
				rc=1
			fi
		fi
	done
	return "$rc"
}

# goshisen_postinst_links ensures the rc.d links exist for the given
# init script (used by postinst). It relies on rc.common's enable.
goshisen_enable_init() {
	local script=$1
	[ -x "$script" ] || return 0
	"$script" enable 2>/dev/null || true
}
