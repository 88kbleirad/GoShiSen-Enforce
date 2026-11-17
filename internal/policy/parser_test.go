package policy

import "testing"

// TestCompileRejectsNonIntegerPorts locks the strictness of the port
// matcher: a port string must be a plain decimal integer. fmt.Sscanf's
// "%d" stops at the first non-digit, so "80evil", "12.5" and "1e3"
// would silently parse as 80, 12 and 1 — a typo that changes the rule
// instead of failing the policy.
func TestCompileRejectsNonIntegerPorts(t *testing.T) {
	cases := []struct {
		name string
		line string
	}{
		{"src_port trailing garbage", "src_port: 80evil"},
		{"src_port decimal fraction", "src_port: 12.5"},
		{"src_port exponent", "src_port: 1e3"},
		{"src_port range lo garbage", "src_port: 80x:90"},
		{"src_port range hi garbage", "src_port: 80:90x"},
		{"dst_port trailing garbage", "dst_port: 53junk"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc := "version: 1\nrules:\n- id: r1\n  action: accept\n  protocol: tcp\n  " + c.line + "\n"
			if _, err := Compile([]byte(doc)); err == nil {
				t.Fatalf("Compile accepted %q; want validation error", c.line)
			}
		})
	}

	// Leading zeros are a legitimate integer spelling and must still
	// parse (strconv.Atoi accepts them).
	doc := "version: 1\nrules:\n- id: r1\n  action: accept\n  protocol: tcp\n  src_port: 080\n"
	snap, err := Compile([]byte(doc))
	if err != nil {
		t.Fatalf("Compile rejected leading-zero port: %v", err)
	}
	r := snap.Rules()[0]
	if !r.HasSrcPort || r.SrcPort.Lo != 80 || r.SrcPort.Hi != 80 {
		t.Fatalf("leading-zero port parsed as %+v, want 80", r.SrcPort)
	}
}
