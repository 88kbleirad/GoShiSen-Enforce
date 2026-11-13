package policy

import (
	"net/netip"
	"testing"
)

// TestTupleKeySemantics pins the accepted-set key canonicalization: the
// reply direction of a connection must map to the same key as the
// original direction, for both IPv4 and IPv6.
func TestTupleKeySemantics(t *testing.T) {
	pkt := func(src, dst string, sport, dport uint16, dir Direction) Packet {
		return Packet{
			Protocol:  ProtoTCP,
			Src:       netip.MustParseAddr(src),
			Dst:       netip.MustParseAddr(dst),
			SrcPort:   sport,
			DstPort:   dport,
			State:     StateEstablished,
			Direction: dir,
		}
	}
	cases := []struct {
		name  string
		orig  Packet
		reply Packet
	}{
		{"ipv4", pkt("10.0.0.1", "10.0.0.2", 12345, 80, DirOriginal),
			pkt("10.0.0.2", "10.0.0.1", 80, 12345, DirReply)},
		{"ipv6", pkt("fd00::1", "fd00::2", 12345, 443, DirOriginal),
			pkt("fd00::2", "fd00::1", 443, 12345, DirReply)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if newTupleKey(tc.orig) != newTupleKey(tc.reply) {
				t.Errorf("orig key %+v != reply key %+v", newTupleKey(tc.orig), newTupleKey(tc.reply))
			}
		})
	}

	// Regression: packaging must not alias 16-bit ports (the previous
	// ppm packing used 8 bits per port, so different port pairs
	// collided — e.g. (0,514) == (514,514) — and established replies
	// were dropped). Exercise ports across the full 16-bit range.
	high := pkt("10.0.0.1", "10.0.0.2", 0x0100, 0xFFFF, DirOriginal)
	i := pkt("10.0.0.1", "10.0.0.2", 0xFFFF, 0x0100, DirOriginal)
	hr := pkt("10.0.0.2", "10.0.0.1", 0xFFFF, 0x0100, DirReply)
	if newTupleKey(high) == newTupleKey(i) {
		t.Errorf("port aliasing: %+v == %+v", high, i)
	}
	// Protocol must not alias ports either: same 5-tuple with TCP and
	// UDP are different connections.
	udp := high
	udp.Protocol = ProtoUDP
	if newTupleKey(high) == newTupleKey(udp) {
		t.Errorf("protocol aliasing: tcp and udp keys equal: %+v", high)
	}
	if newTupleKey(high) != newTupleKey(hr) {
		t.Errorf("high-port reply not canonical: %+v != %+v", high, hr)
	}
	// Distinct connections must not collide.
	a := pkt("10.0.0.1", "10.0.0.2", 12345, 80, DirOriginal)
	b := pkt("10.0.0.1", "10.0.0.2", 12346, 80, DirOriginal)
	c := pkt("10.0.0.1", "10.0.0.3", 12345, 80, DirOriginal)
	d := pkt("10.0.0.1", "10.0.0.2", 12345, 81, DirOriginal)
	seen := map[tupleKey]bool{newTupleKey(a): true}
	for _, p := range []Packet{b, c, d} {
		if seen[newTupleKey(p)] {
			t.Errorf("key collision: %+v", p)
		}
		seen[newTupleKey(p)] = true
	}
}
