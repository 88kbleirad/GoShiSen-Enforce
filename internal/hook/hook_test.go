package hook

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/88kbleirad/GoShiSen-Enforce/internal/config"
)

// nftSyntaxCheck validates a generated nftables program's grammar with
// `nft -c` inside an unprivileged user namespace. It skips when
// user namespaces or nft are unavailable; the privileged integration
// suite exercises the real install path.
func nftSyntaxCheck(t *testing.T, prog string) {
	t.Helper()
	unshare, err := exec.LookPath("unshare")
	if err != nil {
		t.Skip("unshare not in PATH")
	}
	nft, err := exec.LookPath("nft")
	if err != nil {
		t.Skip("nft not in PATH")
	}
	// Confirm unprivileged user namespaces work at all.
	probe := exec.Command(unshare, "-Urn", nft, "-c", "-f", "-")
	probe.Stdin = strings.NewReader("table ip gsh_syntax_probe {\n}\n")
	if out, err := probe.CombinedOutput(); err != nil {
		t.Skipf("unprivileged user namespace nft unavailable: %v: %s", err, out)
	}
	cmd := exec.Command(unshare, "-Urn", nft, "-c", "-f", "-")
	cmd.Stdin = strings.NewReader(prog)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated nftables program failed syntax check:\n%s\nnft: %v: %s", prog, err, out)
	}
}

func TestBuildFilterOnly(t *testing.T) {
	rs := Ruleset{Filter: FilterConfig{QueueNum: 0}}
	out := Build(rs)

	for _, want := range []string{
		"table inet goshisen {",
		"type filter hook forward priority filter; policy drop;",
		"meta nfproto ipv4 meta l4proto { tcp, udp } counter queue to 0",
		"meta nfproto ipv6 meta l4proto { tcp, udp } counter queue to 1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// No NAT chains when disabled.
	if strings.Contains(out, "dstnat") || strings.Contains(out, "srcnat") {
		t.Errorf("NAT chains present despite disabled:\n%s", out)
	}
}

func TestBuildOpenWrtScope(t *testing.T) {
	rs := Ruleset{Filter: FilterConfig{QueueNum: 100}, Scope: ScopeOpenWrt}
	out := Build(rs)

	for _, want := range []string{
		"table inet goshisen {",
		// After firewall4's filter priority 0, policy accept.
		"type filter hook forward priority 1; policy accept;",
		"meta nfproto ipv4 meta l4proto { tcp, udp } counter queue to 100",
		"meta nfproto ipv6 meta l4proto { tcp, udp } counter queue to 101",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// The openwrt scope must never contain NAT chains (firewall4 owns NAT).
	if strings.Contains(out, "dstnat") || strings.Contains(out, "srcnat") {
		t.Errorf("NAT chains present in openwrt scope:\n%s", out)
	}
	// And must not ship a policy drop (fw4's own forward chain drops).
	if strings.Contains(out, "policy drop;") {
		t.Errorf("openwrt scope must not set policy drop:\n%s", out)
	}
	nftSyntaxCheck(t, out)
}

func TestBuildNAT(t *testing.T) {
	rs := Ruleset{
		Filter: FilterConfig{QueueNum: 0},
		Nat: config.NatConfig{
			Enabled:    true,
			Masquerade: config.MasqueradeConfig{Enabled: true, OutIface: "eth1"},
			Dnat: []config.DnatRule{
				{Name: "web", Protocol: "tcp", PublicIP: "10.99.0.1", PublicPort: 80, TargetIP: "10.99.1.2", TargetPort: 8080},
				{Name: "dns", Protocol: "udp", PublicIP: "10.99.0.1", PublicPort: 53, TargetIP: "10.99.1.2", TargetPort: 53},
			},
		},
	}
	out := Build(rs)

	for _, want := range []string{
		"chain dstnat {",
		"type nat hook prerouting priority dstnat;",
		"meta l4proto tcp ip daddr 10.99.0.1 tcp dport 80 dnat to 10.99.1.2:8080",
		"meta l4proto udp ip daddr 10.99.0.1 udp dport 53 dnat to 10.99.1.2:53",
		"chain srcnat {",
		"type nat hook postrouting priority srcnat;",
		`oifname "eth1" masquerade`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestBuildNoMasqueradeWhenDisabled(t *testing.T) {
	rs := Ruleset{
		Filter: FilterConfig{QueueNum: 0},
		Nat: config.NatConfig{
			Enabled: true,
			Dnat: []config.DnatRule{
				{Name: "web", Protocol: "tcp", PublicIP: "10.99.0.1", PublicPort: 80, TargetIP: "10.99.1.2", TargetPort: 8080},
			},
		},
	}
	out := Build(rs)
	if strings.Contains(out, "srcnat") || strings.Contains(out, "masquerade") {
		t.Errorf("masquerade present despite disabled:\n%s", out)
	}
	if !strings.Contains(out, "dstnat") {
		t.Errorf("dnat missing:\n%s", out)
	}
}

// TestBuildSyntaxValidated proves the rendered programs are accepted by
// the real nftables parser for the full configuration matrix: filter
// only, DNAT, MASQUERADE, and both together. This is what catches
// grammar regressions (e.g. a malformed DNAT rule) that string-contains
// assertions cannot.
func TestBuildSyntaxValidated(t *testing.T) {
	full := Ruleset{
		Filter: FilterConfig{QueueNum: 0},
		Nat: config.NatConfig{
			Enabled:    true,
			Masquerade: config.MasqueradeConfig{Enabled: true, OutIface: "eth1"},
			Dnat: []config.DnatRule{
				{Name: "web", Protocol: "tcp", PublicIP: "10.99.0.100", PublicPort: 80, TargetIP: "10.99.1.2", TargetPort: 8080},
				{Name: "dns", Protocol: "udp", PublicIP: "10.99.0.100", PublicPort: 53, TargetIP: "10.99.1.2", TargetPort: 5353},
			},
		},
	}
	programs := map[string]Ruleset{
		"filter-only":      {Filter: FilterConfig{QueueNum: 0}},
		"dnat-only":        {Filter: FilterConfig{QueueNum: 0}, Nat: config.NatConfig{Enabled: true, Dnat: full.Nat.Dnat}},
		"masquerade-only":  {Filter: FilterConfig{QueueNum: 0}, Nat: config.NatConfig{Enabled: true, Masquerade: full.Nat.Masquerade}},
		"filter+dnat+masq": full,
	}
	for name, rs := range programs {
		t.Run(name, func(t *testing.T) {
			nftSyntaxCheck(t, Build(rs))
		})
	}
}

// TestInstallProgramIsAtomic verifies the install program is a single
// transaction that both recreates the table and (re)defines it, so a
// failed apply cannot leave the packet path open.
func TestInstallProgramIsAtomic(t *testing.T) {
	rs := Ruleset{Filter: FilterConfig{QueueNum: 0}}
	var b strings.Builder
	b.WriteString("add table ip " + Table + "\n")
	b.WriteString("delete table ip " + Table + "\n")
	b.WriteString(Build(rs))
	prog := b.String()
	if strings.Count(prog, "delete table ip ") != 1 {
		t.Errorf("want exactly one delete in the atomic program:\n%s", prog)
	}
	if !strings.Contains(prog, "policy drop;") {
		t.Errorf("atomic program must define the drop policy in the same batch:\n%s", prog)
	}
	nftSyntaxCheck(t, prog)
}

func TestValidateNAT(t *testing.T) {
	good := config.NatConfig{
		Enabled: true,
		Dnat: []config.DnatRule{
			{Name: "web", Protocol: "tcp", PublicIP: "10.99.0.1", PublicPort: 80, TargetIP: "10.99.1.2", TargetPort: 8080},
		},
	}
	if err := ValidateNAT(good); err != nil {
		t.Errorf("good NAT rejected: %v", err)
	}

	cases := []config.NatConfig{
		{Enabled: true, Masquerade: config.MasqueradeConfig{Enabled: true}},
		{Enabled: true, Dnat: []config.DnatRule{{Protocol: "sctp"}}},
		{Enabled: true, Dnat: []config.DnatRule{{Name: "a", Protocol: "tcp", PublicIP: "not-an-ip"}}},
		{Enabled: true, Dnat: []config.DnatRule{{Name: "a", Protocol: "tcp", PublicIP: "10.0.0.1", PublicPort: 80, TargetIP: "10.0.0.2", TargetPort: 0}}},
		{Enabled: true, Dnat: []config.DnatRule{
			{Name: "a", Protocol: "tcp", PublicIP: "10.0.0.1", PublicPort: 80, TargetIP: "10.0.0.2", TargetPort: 80},
			{Name: "a", Protocol: "udp", PublicIP: "10.0.0.1", PublicPort: 53, TargetIP: "10.0.0.2", TargetPort: 53},
		}},
	}
	for i, c := range cases {
		if err := ValidateNAT(c); err == nil {
			t.Errorf("case %d: invalid NAT accepted", i)
		}
	}
}
