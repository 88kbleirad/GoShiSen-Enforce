package packet

import (
	"encoding/binary"
	"net/netip"
	"strconv"
	"strings"
	"testing"

	"github.com/88kbleirad/GoShiSen-Enforce/internal/policy"
)

// buildIPv4 assembles a minimal IPv4 packet.
func buildIPv4(totalLen, proto int, src, dst string, fragOffset, flags, ihl int, l4 []byte) []byte {
	if totalLen == 0 {
		totalLen = ihl*4 + len(l4)
	}
	b := make([]byte, ihl*4+len(l4))
	b[0] = byte(0x40 | ihl) // version 4, IHL
	binary.BigEndian.PutUint16(b[2:], uint16(totalLen))
	binary.BigEndian.PutUint16(b[6:], uint16(fragOffset|flags))
	b[9] = byte(proto)
	sip := parseIP4(src)
	dip := parseIP4(dst)
	copy(b[12:16], sip)
	copy(b[16:20], dip)
	copy(b[ihl*4:], l4)
	return b
}

func parseIP4(s string) []byte {
	var out [4]byte
	parts := strings.Split(s, ".")
	for i, p := range parts {
		n, _ := strconv.Atoi(p)
		out[i] = byte(n)
	}
	return out[:]
}

func tcp4(sport, dport uint16, flags byte) []byte {
	b := make([]byte, 20)
	binary.BigEndian.PutUint16(b[0:], sport)
	binary.BigEndian.PutUint16(b[2:], dport)
	b[13] = flags
	return b
}

func udp4(sport, dport uint16) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint16(b[0:], sport)
	binary.BigEndian.PutUint16(b[2:], dport)
	binary.BigEndian.PutUint16(b[4:], uint16(8+len(b)))
	return b
}

func TestParseTCP(t *testing.T) {
	pkt := buildIPv4(40, 6, "10.0.0.1", "10.0.0.2", 0, 0, 5, tcp4(12345, 443, 0x02))
	r := Parse(pkt)
	if !r.OK {
		t.Fatalf("Parse = %+v, want OK", r)
	}
	if r.Packet.Protocol != policy.ProtoTCP {
		t.Errorf("protocol = %v", r.Packet.Protocol)
	}
	if r.Packet.Src.String() != "10.0.0.1" || r.Packet.Dst.String() != "10.0.0.2" {
		t.Errorf("addrs = %s -> %s", r.Packet.Src, r.Packet.Dst)
	}
	if r.Packet.SrcPort != 12345 || r.Packet.DstPort != 443 {
		t.Errorf("ports = %d/%d", r.Packet.SrcPort, r.Packet.DstPort)
	}
}

func TestParseUDP(t *testing.T) {
	pkt := buildIPv4(28, 17, "10.0.0.1", "10.0.0.2", 0, 0, 5, udp4(5353, 53))
	r := Parse(pkt)
	if !r.OK {
		t.Fatalf("Parse = %+v, want OK", r)
	}
	if r.Packet.Protocol != policy.ProtoUDP {
		t.Errorf("protocol = %v", r.Packet.Protocol)
	}
	if r.Packet.SrcPort != 5353 || r.Packet.DstPort != 53 {
		t.Errorf("ports = %d/%d", r.Packet.SrcPort, r.Packet.DstPort)
	}
}

func TestParseWithOptions(t *testing.T) {
	pkt := buildIPv4(0, 6, "10.0.0.1", "10.0.0.2", 0, 0, 6, tcp4(1, 2, 0))
	r := Parse(pkt)
	if !r.OK {
		t.Fatalf("Parse(ihl=6) = %+v", r)
	}
}

func TestParseRejects(t *testing.T) {
	tcp := tcp4(1, 2, 0)
	udp := udp4(1, 2)
	cases := map[string]struct {
		pkt  []byte
		want string // substring of Err, or "" for drop-without-error
	}{
		"empty":             {nil, "empty"},
		"too short":         {make([]byte, 10), "not IPv4 or IPv6"},
		"not ipv4/6":        {append([]byte{0x70}, make([]byte, 30)...), "not IPv4 or IPv6"},
		"ihl zero":          {append([]byte{0x40}, make([]byte, 30)...), "IHL"},
		"ihl less than 5":   {append([]byte{0x41}, make([]byte, 30)...), "IHL"},
		"ip header short":   {buildIPv4(40, 6, "10.0.0.1", "10.0.0.2", 0, 0, 7, tcp)[:20], "shorter"},
		"total len < ihl":   {buildIPv4(10, 6, "10.0.0.1", "10.0.0.2", 0, 0, 5, tcp), "total length"},
		"truncated packets": {buildIPv4(400, 6, "10.0.0.1", "10.0.0.2", 0, 0, 5, tcp), ""},
		"truncated tcp":     {buildIPv4(0, 6, "10.0.0.1", "10.0.0.2", 0, 0, 5, tcp[:10]), "truncated TCP"},
		"truncated udp":     {buildIPv4(0, 17, "10.0.0.1", "10.0.0.2", 0, 0, 5, udp[:4]), "truncated UDP"},
		"fragment first":    {buildIPv4(40, 6, "10.0.0.1", "10.0.0.2", 100, 0x2000, 5, tcp), "fragment"},
		"fragment offset":   {buildIPv4(40, 6, "10.0.0.1", "10.0.0.2", 200, 0, 5, tcp), "fragment"},
		"icmp unsupported":  {buildIPv4(0, 1, "10.0.0.1", "10.0.0.2", 0, 0, 5, make([]byte, 8)), "protocol 1"},
		"gre unsupported":   {buildIPv4(0, 47, "10.0.0.1", "10.0.0.2", 0, 0, 5, make([]byte, 4)), "protocol 47"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := Parse(tc.pkt)
			if r.OK {
				t.Fatalf("Parse accepted invalid packet: %+v", r)
			}
			if tc.want != "" && !strings.Contains(r.Err, tc.want) && !strings.Contains(r.Unsupported, tc.want) {
				t.Errorf("Err = %q, Unsupported = %q, want %q", r.Err, r.Unsupported, tc.want)
			}
			if tc.want == "" && !r.Truncated {
				t.Errorf("expected Truncated=true for %s", name)
			}
		})
	}
}

func TestParseUnsupportedProtocol(t *testing.T) {
	r := Parse(buildIPv4(28, 1, "10.0.0.1", "10.0.0.2", 0, 0, 5, make([]byte, 8)))
	if r.Unsupported == "" {
		t.Error("Unsupported empty for ICMP")
	}
	if r.OK {
		t.Error("OK true for ICMP")
	}
}

func TestParseNeverPanicsOnRandomInput(t *testing.T) {
	seeds := [][]byte{
		nil,
		{},
		{0x45},
		make([]byte, 100),
		append([]byte{0x45}, make([]byte, 99)...),
	}
	for i := 0; i < 1000; i++ {
		b := make([]byte, i%80)
		for j := range b {
			b[j] = byte(i * 31 % 251)
		}
		seeds = append(seeds, b)
	}
	for i, pkt := range seeds {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Parse panicked on seed %d: %v", i, r)
				}
			}()
			Parse(pkt)
		}()
	}
}

// FuzzParse feeds arbitrary bytes and asserts the parser never panics and
// never claims OK for clearly truncated input.
func FuzzParse(f *testing.F) {
	f.Add([]byte{0x45, 0x00, 0x00, 0x28, 0, 0, 0, 0, 64, 6, 0, 0, 10, 0, 0, 1, 10, 0, 0, 2})
	f.Add(udp4(1, 2))
	f.Add([]byte{})
	f.Add([]byte{0x45})
	f.Fuzz(func(t *testing.T, b []byte) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("panic: %v (input %x)", r, b)
			}
		}()
		r := Parse(b)
		if r.OK && len(b) < 20 {
			t.Errorf("OK for %d-byte input", len(b))
		}
		_ = r
	})
}

// FuzzParseAttrs fuzzes the netlink attribute parser. parseAttrs
// previously panicked on truncated trailing attributes (slice bounds
// out of range); this guards against any future panic.
func FuzzParseAttrs(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0, 8, 0, 1, 1, 2, 3, 4})
	f.Add([]byte{72, 0, 0, 0, 1, 2, 3, 4})
	f.Add([]byte{0, 8, 0, 1, 1, 2, 3, 4, 72, 0, 0, 0})
	f.Fuzz(func(t *testing.T, b []byte) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("panic: %v (input %x)", r, b)
			}
		}()
		parseAttrs(b, func(typ uint16, p []byte) bool { return true })
	})
}

// buildIPv6 assembles a minimal IPv6 packet (no extension headers).
func buildIPv6(payloadLen, nextHdr int, src, dst string, l4 []byte) []byte {
	b := make([]byte, 40+len(l4))
	b[0] = 0x60 // version 6
	binary.BigEndian.PutUint16(b[4:], uint16(payloadLen))
	b[6] = byte(nextHdr)
	b[7] = 64
	sp := netip.MustParseAddr(src).As16()
	dp := netip.MustParseAddr(dst).As16()
	copy(b[8:24], sp[:])
	copy(b[24:40], dp[:])
	copy(b[40:], l4)
	return b
}

func TestParseIPv6TCP(t *testing.T) {
	pkt := buildIPv6(20, 6, "fd70:1::100", "fd80::2", tcp4(12345, 8088, 0x02))
	r := Parse(pkt)
	if !r.OK {
		t.Fatalf("Parse = %+v, want OK", r)
	}
	if r.Packet.Protocol != policy.ProtoTCP {
		t.Errorf("protocol = %v, want tcp", r.Packet.Protocol)
	}
	if !r.Packet.Src.Is6() || r.Packet.Src.String() != "fd70:1::100" {
		t.Errorf("src = %v, want fd70:1::100", r.Packet.Src)
	}
	if !r.Packet.Dst.Is6() || r.Packet.Dst.String() != "fd80::2" {
		t.Errorf("dst = %v, want fd80::2", r.Packet.Dst)
	}
	if r.Packet.SrcPort != 12345 || r.Packet.DstPort != 8088 {
		t.Errorf("ports = %d:%d, want 12345:8088", r.Packet.SrcPort, r.Packet.DstPort)
	}
}

func TestParseIPv6UDP(t *testing.T) {
	pkt := buildIPv6(8, 17, "fd70:1::100", "fd80::2", udp4(53, 5353))
	r := Parse(pkt)
	if !r.OK {
		t.Fatalf("Parse = %+v, want OK", r)
	}
	if r.Packet.Protocol != policy.ProtoUDP {
		t.Errorf("protocol = %v, want udp", r.Packet.Protocol)
	}
}

func TestParseIPv6ExtensionHeaders(t *testing.T) {
	// TCP with a hop-by-hop extension header (8 bytes) in front.
	hbhl := []byte{6, 0, 0, 0, 0, 0, 0, 0} // next=TCP, len=0
	tcp := tcp4(12345, 8088, 0x02)
	pkt := make([]byte, 40+8+len(tcp))
	pkt[0] = 0x60
	binary.BigEndian.PutUint16(pkt[4:], uint16(8+len(tcp)))
	pkt[6] = 0 // hop-by-hop
	pkt[7] = 64
	sp := netip.MustParseAddr("fd70:1::100").As16()
	dp := netip.MustParseAddr("fd80::2").As16()
	copy(pkt[8:24], sp[:])
	copy(pkt[24:40], dp[:])
	copy(pkt[40:48], hbhl)
	copy(pkt[48:], tcp)
	r := Parse(pkt)
	if !r.OK {
		t.Fatalf("Parse = %+v, want OK", r)
	}
	if r.Packet.DstPort != 8088 {
		t.Errorf("dst_port = %d, want 8088", r.Packet.DstPort)
	}
}

func TestParseIPv6FragmentNonInitial(t *testing.T) {
	// Fragment header with offset != 0.
	frag := []byte{6, 0, 0x00, 0x08, 0, 0, 0, 0} // next=TCP, offset=1
	pkt := make([]byte, 40+8)
	pkt[0] = 0x60
	binary.BigEndian.PutUint16(pkt[4:], 8)
	pkt[6] = 44 // fragment
	sp := netip.MustParseAddr("fd70:1::100").As16()
	dp := netip.MustParseAddr("fd80::2").As16()
	copy(pkt[8:24], sp[:])
	copy(pkt[24:40], dp[:])
	copy(pkt[40:], frag)
	r := Parse(pkt)
	if !r.Fragment {
		t.Fatalf("Parse = %+v, want Fragment", r)
	}
}

func TestParseIPv6Truncated(t *testing.T) {
	pkt := buildIPv6(20, 6, "fd70:1::100", "fd80::2", tcp4(1, 2, 0))
	// Truncate below the TCP header length.
	r := Parse(pkt[:40+10])
	if r.OK || !r.Truncated {
		t.Fatalf("Parse = %+v, want Truncated", r)
	}
}
