package policy

import (
	"net/netip"
	"strings"
	"testing"
)

func mustCompile(t *testing.T, yaml string) *Snapshot {
	t.Helper()
	s, err := Compile([]byte(yaml))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return s
}

// packet builds a test Packet.
func packet(proto Proto, src, dst string, sport, dport uint16) Packet {
	return Packet{
		Protocol: proto,
		Src:      mustAddr4(src),
		Dst:      mustAddr4(dst),
		SrcPort:  sport,
		DstPort:  dport,
	}
}

func mustAddr4(s string) (a netip.Addr) {
	a, err := netip.ParseAddr(s)
	if err != nil {
		panic(err)
	}
	return a
}

// ---------------------------------------------------------------------------
// Compile: valid inputs
// ---------------------------------------------------------------------------

func TestCompileMinimal(t *testing.T) {
	s := mustCompile(t, `
version: 1
rules:
  - id: allow-http
    action: ACCEPT
    protocol: tcp
    dst_port: 80
`)
	if s.Len() != 1 {
		t.Fatalf("rules = %d, want 1", s.Len())
	}
	r := s.Rules()[0]
	if r.ID != "allow-http" || r.Action != ActionAccept || r.Protocol != ProtoTCP {
		t.Errorf("rule = %+v", r)
	}
	if r.Src.String() != "any" || r.Dst.String() != "any" {
		t.Errorf("defaults not applied: src=%s dst=%s", r.Src, r.Dst)
	}
	if r.DstPort == nil || !r.DstPort.Contains(80) {
		t.Errorf("dst_port not parsed: %v", r.DstPort)
	}
}

func TestCompileDefaults(t *testing.T) {
	s := mustCompile(t, "version: 1\nrules: []\n")
	if s.DefaultAction() != ActionDrop {
		t.Errorf("default action = %v, want DROP", s.DefaultAction())
	}
	s = mustCompile(t, "version: 1\ndefault_action: ACCEPT\nrules: []\n")
	if s.DefaultAction() != ActionAccept {
		t.Errorf("default action = %v, want ACCEPT", s.DefaultAction())
	}
}

func TestCompileAnyFields(t *testing.T) {
	s := mustCompile(t, `
version: 1
rules:
  - id: any-all
    action: ACCEPT
    protocol: any
    src: any
    dst: any
`)
	r := s.Rules()[0]
	if r.Protocol != ProtoAny || r.Src.String() != "any" || r.Dst.String() != "any" {
		t.Errorf("any fields not normalized: %+v", r)
	}
}

func TestCompilePortRange(t *testing.T) {
	s := mustCompile(t, `
version: 1
rules:
  - id: svc
    action: ACCEPT
    protocol: udp
    src_port: 1024:2048
    dst_port: 53
`)
	r := s.Rules()[0]
	if r.SrcPort == nil || r.SrcPort.Lo != 1024 || r.SrcPort.Hi != 2048 {
		t.Errorf("src_port range = %v", r.SrcPort)
	}
	if r.DstPort == nil || r.DstPort.Lo != 53 || r.DstPort.Hi != 53 {
		t.Errorf("dst_port single = %v", r.DstPort)
	}
	if r.SrcPort.String() != "1024:2048" || r.DstPort.String() != "53" {
		t.Errorf("range String = %s/%s", r.SrcPort, r.DstPort)
	}
}

// ---------------------------------------------------------------------------
// Compile: invalid configuration
// ---------------------------------------------------------------------------

func TestCompileRejects(t *testing.T) {
	cases := map[string]string{
		"unknown field": `version: 1
rules:
  - id: r1
    action: ACCEPT
    bogus: x
`,
		"empty rule id": `version: 1
rules:
  - action: ACCEPT
`,
		"duplicate id": `version: 1
rules:
  - id: r1
    action: ACCEPT
  - id: r1
    action: DROP
`,
		"bad action": `version: 1
rules:
  - id: r1
    action: FROBNICATE
`,
		"bad protocol": `version: 1
rules:
  - id: r1
    action: ACCEPT
    protocol: sctp
`,
		"malformed cidr": `version: 1
rules:
  - id: r1
    action: ACCEPT
    src: 10.0.0.0/99
`,
		"bare address cidr": `version: 1
rules:
  - id: r1
    action: ACCEPT
    dst: not-an-ip
`,
		"ipv6 cidr": `version: 1
rules:
  - id: r1
    action: ACCEPT
    src: ::1/128
`,
		"wildcard octets": `version: 1
rules:
  - id: r1
    action: ACCEPT
    src: 10.0.*.*
`,
		"port with protocol any": `version: 1
rules:
  - id: r1
    action: ACCEPT
    protocol: any
    dst_port: 80
`,
		"port missing protocol": `version: 1
rules:
  - id: r1
    action: ACCEPT
    src_port: 80
`,
		"port zero": `version: 1
rules:
  - id: r1
    action: ACCEPT
    protocol: tcp
    dst_port: 0
`,
		"port above 65535": `version: 1
rules:
  - id: r1
    action: ACCEPT
    protocol: udp
    dst_port: 70000
`,
		"port not integer": `version: 1
rules:
  - id: r1
    action: ACCEPT
    protocol: tcp
    dst_port: http
`,
		"range lo gt hi": `version: 1
rules:
  - id: r1
    action: ACCEPT
    protocol: tcp
    src_port: 2000:1000
`,
		"range triple": `version: 1
rules:
  - id: r1
    action: ACCEPT
    protocol: tcp
    dst_port: 1:2:3
`,
		"invalid state value": `version: 1
rules:
  - id: r1
    action: ACCEPT
    state: [WATCHING]
`,
		"reserved interface field": `version: 1
rules:
  - id: r1
    action: ACCEPT
    in_iface: eth0
`,
		"bad default action": `version: 1
default_action: LOG
rules: []
`,
		"bad version": `version: 2
rules: []
`,
		"malformed yaml": `version: 1
rules: [unclosed
`,
		"top-level unknown field": `version: 1
snat: true
rules: []
`,
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Compile([]byte(tc)); err == nil {
				t.Fatal("Compile succeeded, want error")
			}
		})
	}
}

func TestCompileRejectsReportsAllErrors(t *testing.T) {
	_, err := Compile([]byte(`
version: 1
rules:
  - id: r1
    action: BAD
    protocol: sctp
    src: 10.0.0.0/40
  - id: r1
    action: ACCEPT
`))
	if err == nil {
		t.Fatal("Compile succeeded, want errors")
	}
	msg := err.Error()
	for _, want := range []string{"action", "protocol", "src", "duplicate"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not mention %q", msg, want)
		}
	}
}

func TestCompileReservedFieldMessage(t *testing.T) {
	_, err := Compile([]byte(`
version: 1
rules:
  - id: r1
    action: ACCEPT
    out_iface: eth1
`))
	if err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Errorf("err = %v, want reserved-field message", err)
	}
}

// ---------------------------------------------------------------------------
// Rule ordering, overlap, first-match, default DROP
// ---------------------------------------------------------------------------

const orderPolicy = `
version: 1
default_action: DROP
rules:
  - id: deny-web
    action: DROP
    protocol: tcp
    dst_port: 80
  - id: allow-web
    action: ACCEPT
    protocol: tcp
    dst_port: 80
  - id: allow-udp
    action: ACCEPT
    protocol: udp
    dst_port: 53
`

func TestFirstMatchWins(t *testing.T) {
	s := mustCompile(t, orderPolicy)
	for _, tc := range []struct {
		name string
		p    Packet
		want Action
		id   string
	}{
		{"first rule matches first", packet(ProtoTCP, "10.0.0.1", "10.0.0.2", 12345, 80), ActionDrop, "deny-web"},
		{"later overlapping rule unreachable", packet(ProtoTCP, "10.0.0.1", "10.0.0.2", 12345, 8080), ActionDrop, ""},
		{"distinct rule reachable", packet(ProtoUDP, "10.0.0.1", "10.0.0.2", 12345, 53), ActionAccept, "allow-udp"},
		{"no match default drop", packet(ProtoTCP, "10.0.0.1", "10.0.0.2", 1, 22), ActionDrop, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := s.Decide(tc.p)
			if v.Action != tc.want || v.RuleID != tc.id || v.Matched != (tc.id != "") {
				t.Errorf("Decide(%+v) = %+v, want action=%v id=%s matched=%v",
					tc.p, v, tc.want, tc.id, tc.id != "")
			}
		})
	}
}

func TestOverlappingRuleOrdering(t *testing.T) {
	// Identical matchers; both match every packet. Order must decide.
	s := mustCompile(t, `
version: 1
rules:
  - id: first-drop
    action: DROP
  - id: second-accept
    action: ACCEPT
`)
	v := s.Decide(packet(ProtoTCP, "1.2.3.4", "5.6.7.8", 1, 2))
	if v.Action != ActionDrop || v.RuleID != "first-drop" {
		t.Errorf("Decide = %+v, want drop first-drop", v)
	}
}

// ---------------------------------------------------------------------------
// CIDR boundaries
// ---------------------------------------------------------------------------

func TestCIDRMatches(t *testing.T) {
	// In-subnet, boundary, and out-of-subnet cases for /24, /32, /0.
	s := mustCompile(t, `
version: 1
rules:
  - id: sub-24
    action: ACCEPT
    src: 10.1.2.0/24
    dst: 10.9.9.0/24
  - id: host
    action: ACCEPT
    src: 192.168.0.55/32
`)
	cases := []struct {
		name string
		p    Packet
		want Action
	}{
		{"inside /24", packet(ProtoTCP, "10.1.2.1", "10.9.9.250", 1, 1), ActionAccept},
		{"boundary low", packet(ProtoTCP, "10.1.2.0", "10.9.9.0", 1, 1), ActionAccept},
		{"boundary high", packet(ProtoTCP, "10.1.2.255", "10.9.9.255", 1, 1), ActionAccept},
		{"just below", packet(ProtoTCP, "10.1.1.255", "10.9.9.1", 1, 1), ActionDrop},
		{"just above", packet(ProtoTCP, "10.1.3.0", "10.9.9.1", 1, 1), ActionDrop},
		{"dst outside", packet(ProtoTCP, "10.1.2.1", "10.8.0.1", 1, 1), ActionDrop},
		{"exact host", packet(ProtoTCP, "192.168.0.55", "10.0.0.1", 1, 1), ActionAccept},
		{"host neighbor", packet(ProtoTCP, "192.168.0.56", "10.0.0.1", 1, 1), ActionDrop},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := s.Decide(tc.p)
			if v.Action != tc.want {
				t.Errorf("Decide(%+v) = %v, want %v", tc.p, v.Action, tc.want)
			}
		})
	}
}

func TestParsePrefixValues(t *testing.T) {
	cases := []struct {
		in   string
		bits int
		str  string
	}{
		{"any", 0, "any"},
		{"", 0, "any"},
		{"0.0.0.0/0", 0, "any"},
		{"10.0.0.0/8", 8, "10.0.0.0/8"},
		{"1.2.3.4", 32, "1.2.3.4/32"},
		{"1.2.3.4/32", 32, "1.2.3.4/32"},
	}
	for _, tc := range cases {
		p, err := ParsePrefix(tc.in)
		if err != nil {
			t.Errorf("ParsePrefix(%q): %v", tc.in, err)
			continue
		}
		if p.CIDRBits() != tc.bits || p.String() != tc.str {
			t.Errorf("ParsePrefix(%q) = bits %d str %q, want %d %q", tc.in, p.CIDRBits(), p.String(), tc.bits, tc.str)
		}
	}
}

func TestParsePrefixRejects(t *testing.T) {
	for _, in := range []string{"10.0.0.0/33", "10.0.0.0/-1", "not-an-ip", "10.0.*.*", "::1/128", "10.0.0.0/24/24"} {
		if _, err := ParsePrefix(in); err == nil {
			t.Errorf("ParsePrefix(%q) succeeded, want error", in)
		}
	}
}

// ---------------------------------------------------------------------------
// Port boundaries
// ---------------------------------------------------------------------------

func TestPortBoundaries(t *testing.T) {
	s := mustCompile(t, `
version: 1
rules:
  - id: rng
    action: ACCEPT
    protocol: tcp
    src_port: 1024:2048
  - id: single
    action: ACCEPT
    protocol: udp
    dst_port: 53
`)
	cases := []struct {
		name string
		p    Packet
		want Action
	}{
		{"range low edge", packet(ProtoTCP, "1.1.1.1", "2.2.2.2", 1024, 1), ActionAccept},
		{"range high edge", packet(ProtoTCP, "1.1.1.1", "2.2.2.2", 2048, 1), ActionAccept},
		{"range inside", packet(ProtoTCP, "1.1.1.1", "2.2.2.2", 1500, 1), ActionAccept},
		{"range just below", packet(ProtoTCP, "1.1.1.1", "2.2.2.2", 1023, 1), ActionDrop},
		{"range just above", packet(ProtoTCP, "1.1.1.1", "2.2.2.2", 2049, 1), ActionDrop},
		{"wrong proto no match", packet(ProtoUDP, "1.1.1.1", "2.2.2.2", 1500, 1), ActionDrop},
		{"single exact", packet(ProtoUDP, "1.1.1.1", "2.2.2.2", 5000, 53), ActionAccept},
		{"single neighbor", packet(ProtoUDP, "1.1.1.1", "2.2.2.2", 5000, 54), ActionDrop},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := s.Decide(tc.p)
			if v.Action != tc.want {
				t.Errorf("Decide(%+v) = %v, want %v", tc.p, v.Action, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Default deny
// ---------------------------------------------------------------------------

func TestDefaultDeny(t *testing.T) {
	s := mustCompile(t, `
version: 1
rules:
  - id: allow-all
    action: ACCEPT
`)
	v := s.Decide(packet(ProtoTCP, "9.9.9.9", "8.8.8.8", 1, 2))
	if v.Action != ActionAccept {
		t.Errorf("Decide = %v, want ACCEPT", v.Action)
	}

	// Empty ruleset: everything drops.
	s2 := mustCompile(t, "version: 1\nrules: []\n")
	v = s2.Decide(packet(ProtoTCP, "9.9.9.9", "8.8.8.8", 1, 2))
	if v.Action != ActionDrop || v.Matched {
		t.Errorf("Decide = %+v, want DROP unmatched", v)
	}
}

// ---------------------------------------------------------------------------
// Stateful matching (Phase 4)
// ---------------------------------------------------------------------------

const statefulPolicy = `
version: 1
default_action: DROP
rules:
  - id: allow-new
    action: ACCEPT
    protocol: tcp
    src: 10.0.0.0/24
    dst_port: 8080
    state: [NEW]
  - id: allow-estab
    action: ACCEPT
    protocol: tcp
    state: [ESTABLISHED, RELATED]
`

func statePacket() Packet {
	return Packet{
		Protocol: ProtoTCP,
		Src:      mustAddr4("10.0.0.5"),
		Dst:      mustAddr4("10.0.0.2"),
		SrcPort:  12345,
		DstPort:  8080,
	}
}

func TestStatefulDecide(t *testing.T) {
	s := mustCompile(t, statefulPolicy)

	// NEW original accepted and recorded.
	p := statePacket()
	p.State = StateNew
	p.Direction = DirOriginal
	v := s.Decide(p)
	if v.Action != ActionAccept || !v.Accepted || v.RuleID != "allow-new" {
		t.Errorf("Decide(NEW) = %+v, want ACCEPT + accepted", v)
	}

	// ESTABLISHED reply, not yet accepted: the five-tuple alone must not
	// prove the connection.
	p2 := statePacket()
	p2.State = StateEstablished
	p2.Direction = DirReply
	v = s.Decide(p2)
	if v.Action != ActionDrop {
		t.Errorf("Decide(ESTABLISHED, unaccepted) = %+v, want DROP", v)
	}

	// Same packet, accepted flag set → passes.
	p3 := p2
	p3.Accepted = true
	v = s.Decide(p3)
	if v.Action != ActionAccept || v.RuleID != "allow-estab" {
		t.Errorf("Decide(ESTABLISHED, accepted) = %+v, want ACCEPT", v)
	}
}

func TestStatefulKingProcess(t *testing.T) {
	k := NewKing()
	s := mustCompile(t, statefulPolicy)
	k.Replace(s)

	// NEW accept records the connection.
	np := statePacket()
	np.State = StateNew
	np.Direction = DirOriginal
	v := k.Process(np)
	if v.Action != ActionAccept || !v.Accepted {
		t.Fatalf("Process(NEW) = %+v", v)
	}

	// Reply traffic now passes.
	ep := statePacket()
	ep.State = StateEstablished
	ep.Direction = DirReply
	v = k.Process(ep)
	if v.Action != ActionAccept || v.RuleID != "allow-estab" {
		t.Errorf("Process(ESTABLISHED reply) = %+v, want ACCEPT", v)
	}

	// An unsolicited inbound connection is never accepted.
	other := statePacket()
	other.Src, other.Dst = other.Dst, other.Src
	other.SrcPort, other.DstPort = other.DstPort, other.SrcPort
	other.State = StateNew
	other.Direction = DirOriginal
	if v := k.Process(other); v.Action != ActionDrop {
		t.Errorf("Process(unsolicited NEW) = %+v, want DROP", v)
	}
	if v := k.Process(other); v.Action != ActionDrop {
		t.Errorf("Process(unsolicited, 5-tuple matches established) = %+v, want DROP", v)
	}
}

func TestStateParsing(t *testing.T) {
	s := mustCompile(t, `
version: 1
default_action: ACCEPT
rules:
  - id: est
    action: ACCEPT
    state: [ESTABLISHED]
  - id: rel
    action: ACCEPT
    state: [RELATED]
  - id: inv
    action: DROP
    state: [INVALID]
`)
	p := statePacket()
	p.State = StateEstablished
	p.Accepted = true
	if v := s.Decide(p); v.RuleID != "est" {
		t.Errorf("established rule: got %+v", v)
	}
	p.State = StateRelated
	p.Accepted = true
	if v := s.Decide(p); v.RuleID != "rel" {
		t.Errorf("related rule: got %+v", v)
	}
	p.State = StateInvalid
	if v := s.Decide(p); v.RuleID != "inv" {
		t.Errorf("invalid rule: got %+v", v)
	}
	p.State = StateNew
	if v := s.Decide(p); v.Action != ActionAccept || v.Matched {
		t.Errorf("no-state rule should not match NEW: %+v", v)
	}
}

// TestPolicyReloadKeepsEstablished verifies that a policy reload does not
// sever existing accepted connections: the accepted set survives the
// snapshot swap.
func TestPolicyReloadKeepsEstablished(t *testing.T) {
	k := NewKing()
	k.Replace(mustCompile(t, statefulPolicy))

	np := statePacket()
	np.State = StateNew
	np.Direction = DirOriginal
	if v := k.Process(np); v.Action != ActionAccept {
		t.Fatalf("initial accept failed: %+v", v)
	}

	// Reload: NEW is now denied, but established reply traffic must
	// keep flowing.
	k.Replace(mustCompile(t, `
version: 1
default_action: DROP
rules:
  - id: allow-estab
    action: ACCEPT
    protocol: tcp
    state: [ESTABLISHED]
`))

	np2 := statePacket()
	np2.State = StateNew
	np2.Direction = DirOriginal
	if v := k.Process(np2); v.Action != ActionDrop {
		t.Errorf("NEW after reload = %+v, want DROP", v)
	}

	ep := statePacket()
	ep.State = StateEstablished
	ep.Direction = DirReply
	if v := k.Process(ep); v.Action != ActionAccept {
		t.Errorf("established after reload = %+v, want ACCEPT", v)
	}
}

func TestSnapshotImmutability(t *testing.T) {
	s := mustCompile(t, orderPolicy)
	before := s.Rules()

	// Mutating the returned slice must not affect the snapshot's view.
	// The snapshot owns its backing array; Rules() returns it for reads.
	rules := s.Rules()
	if len(rules) != 3 {
		t.Fatalf("rules = %d", len(rules))
	}
	rules[0].ID = "mutated"
	if got := s.Rules()[0].ID; got != "deny-web" {
		t.Errorf("snapshot mutated: id = %q", got)
	}
	_ = before
}

func TestKingAtomicReplace(t *testing.T) {
	k := NewKing()

	// Initial snapshot is fail-closed: empty, default DROP.
	v := k.Simulate(packet(ProtoTCP, "1.1.1.1", "2.2.2.2", 1, 2))
	if v.Action != ActionDrop || v.Matched {
		t.Errorf("initial simulate = %+v, want DROP unmatched", v)
	}

	allow := mustCompile(t, `
version: 1
rules:
  - id: allow
    action: ACCEPT
`)
	prev := k.Replace(allow)
	if prev == nil || prev.Len() != 0 {
		t.Errorf("Replace prev = %v, want empty snapshot", prev)
	}

	v = k.Simulate(packet(ProtoTCP, "1.1.1.1", "2.2.2.2", 1, 2))
	if v.Action != ActionAccept || v.RuleID != "allow" {
		t.Errorf("simulate after replace = %+v, want allow", v)
	}

	// The old snapshot is still usable and unchanged.
	if v := prev.Decide(packet(ProtoTCP, "1.1.1.1", "2.2.2.2", 1, 2)); v.Action != ActionDrop {
		t.Errorf("old snapshot = %+v, want DROP", v)
	}
}

// TestKingConcurrentReplace exercises the atomic swap under concurrent
// reads. It is a smoke test for data races (run with -race).
func TestKingConcurrentReplace(t *testing.T) {
	k := NewKing()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 1000 {
			_ = k.Simulate(packet(ProtoTCP, "1.1.1.1", "2.2.2.2", 1, 2))
		}
	}()
	for range 50 {
		s := mustCompile(t, `
version: 1
rules:
  - id: r
    action: ACCEPT
    protocol: tcp
    dst_port: 80
`)
		k.Replace(s)
	}
	<-done
}
