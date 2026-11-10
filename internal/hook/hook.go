// Package hook provisions the application-owned nftables ruleset: a
// forward filter chain (default drop, queue TCP/UDP) plus optional NAT
// chains (dstnat: DNAT; srcnat: MASQUERADE). The entire ruleset is
// applied as one transaction via a single `nft -f` program, so a failed
// apply changes nothing (automatic rollback). Only the table `ip
// goshisen` is ever touched; no other table is listed, flushed, or
// modified.
package hook

import (
	"fmt"
	"net/netip"
	"os/exec"
	"strings"

	"github.com/88kbleirad/GoShiSen-Enforce/internal/config"
)

// Table is the nftables table name the application owns.
const Table = "goshisen"

// FilterConfig is what the filter chain needs to know.
type FilterConfig struct {
	// QueueNum is the NFQUEUE number.
	QueueNum int
}

// Ruleset describes the full application ruleset.
type Ruleset struct {
	Filter FilterConfig
	Nat    config.NatConfig
}

// Build renders the complete nftables program for the ruleset. The
// program is one transaction: if any line fails, the kernel applies
// none of it.
func Build(rs Ruleset) string {
	var b strings.Builder
	fmt.Fprintf(&b, "table ip %s {\n", Table)
	fmt.Fprintf(&b, "    chain forward {\n")
	fmt.Fprintf(&b, "        type filter hook forward priority filter; policy drop;\n")
	// The conntrack expression activates the nf_conntrack hooks for this
	// table so forwarded packets get tracked; NFQUEUE only attaches
	// NFQA_CT/NFQA_CT_INFO for tracked packets (nf_ct_get returns NULL
	// otherwise), and the engine's state matcher treats untracked
	// packets as INVALID. This rule matches every packet (untracked
	// packets have state `untracked`) and has no verdict, so it only
	// forces tracking and does not change which packets are queued.
	fmt.Fprintf(&b, "        ct state { new, established, related, invalid, untracked } counter\n")
	fmt.Fprintf(&b, "        meta l4proto { tcp, udp } counter queue num %d\n", rs.Filter.QueueNum)
	fmt.Fprintf(&b, "    }\n")

	if rs.Nat.Enabled {
		fmt.Fprintf(&b, "    chain dstnat {\n")
		fmt.Fprintf(&b, "        type nat hook prerouting priority dstnat;\n")
		for _, d := range rs.Nat.Dnat {
			fmt.Fprintf(&b, "        meta l4proto %s ip daddr %s %s dport %d dnat to %s:%d\n",
				d.Protocol, d.PublicIP, d.Protocol, d.PublicPort,
				d.TargetIP, d.TargetPort)
		}
		fmt.Fprintf(&b, "    }\n")
		if rs.Nat.Masquerade.Enabled {
			fmt.Fprintf(&b, "    chain srcnat {\n")
			fmt.Fprintf(&b, "        type nat hook postrouting priority srcnat;\n")
			fmt.Fprintf(&b, "        oifname %q masquerade\n", rs.Nat.Masquerade.OutIface)
			fmt.Fprintf(&b, "    }\n")
		}
	}
	fmt.Fprintf(&b, "}\n")
	return b.String()
}

// Install replaces the application-owned ruleset atomically in the
// current network namespace. The program is a single transaction:
// re-create the table (idempotent; absent is normal) and define the
// whole ruleset. If any line fails, the kernel applies none of it —
// the previous ruleset (or its absence) is preserved untouched. Only
// the table `ip goshisen` is ever touched.
func Install(rs Ruleset) error {
	if _, err := exec.LookPath("nft"); err != nil {
		return fmt.Errorf("hook: nft binary not found in PATH: %w", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "add table ip %s\n", Table)
	fmt.Fprintf(&b, "delete table ip %s\n", Table)
	b.WriteString(Build(rs))
	cmd := exec.Command("nft", "-f", "/dev/stdin")
	cmd.Stdin = strings.NewReader(b.String())
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("hook: nft install failed (no changes applied): %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Verify checks that the table and queue rule are present, returning an
// error otherwise. Read-only.
func Verify() error {
	out, err := exec.Command("nft", "list", "table", "ip", Table).CombinedOutput()
	if err != nil {
		return fmt.Errorf("hook: nft list failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if !strings.Contains(string(out), "queue num") {
		return fmt.Errorf("hook: table %s exists but queue rule not found", Table)
	}
	return nil
}

// Cleanup removes the application-owned table entirely (best-effort).
// This is the only cleanup path; it never touches other tables.
func Cleanup() error {
	if _, err := exec.LookPath("nft"); err != nil {
		return fmt.Errorf("hook: nft binary not found in PATH: %w", err)
	}
	_ = exec.Command("nft", "delete", "table", "ip", Table).Run()
	return nil
}

// ValidateNAT performs IPv4 checks on NAT config (used by config package
// tests and daemon startup).
func ValidateNAT(n config.NatConfig) error {
	if !n.Enabled {
		return nil
	}
	if n.Masquerade.Enabled && n.Masquerade.OutIface == "" {
		return fmt.Errorf("masquerade.out_iface is required")
	}
	seen := map[string]bool{}
	for _, d := range n.Dnat {
		if d.Name == "" {
			return fmt.Errorf("dnat rule name is required")
		}
		if seen[d.Name] {
			return fmt.Errorf("duplicate dnat name %q", d.Name)
		}
		seen[d.Name] = true
		if d.Protocol != "tcp" && d.Protocol != "udp" {
			return fmt.Errorf("dnat %s: protocol must be tcp or udp", d.Name)
		}
		pub, err := netip.ParseAddr(d.PublicIP)
		if err != nil || !pub.Is4() {
			return fmt.Errorf("dnat %s: public IP %q is not IPv4", d.Name, d.PublicIP)
		}
		tgt, err := netip.ParseAddr(d.TargetIP)
		if err != nil || !tgt.Is4() {
			return fmt.Errorf("dnat %s: target IP %q is not IPv4", d.Name, d.TargetIP)
		}
		if d.PublicPort < 1 || d.PublicPort > 65535 || d.TargetPort < 1 || d.TargetPort > 65535 {
			return fmt.Errorf("dnat %s: ports out of range", d.Name)
		}
	}
	return nil
}
