package policy

import (
	"fmt"
	"net/netip"
	"testing"
)

// benchSnapshot builds a realistic policy: default DROP with typical lab
// rules (established accept, explicit allows, invalid drop). size rules
// controls the number of rules to test scaling.
func benchSnapshot(size int) *Snapshot {
	var rules []Rule
	// Classic stateful baseline first.
	rules = append(rules, Rule{
		ID: "drop-invalid", Action: ActionDrop, StateMask: maskOf(StateInvalid),
	})
	rules = append(rules, Rule{
		ID: "allow-established", Action: ActionAccept, StateMask: maskOf(StateEstablished, StateRelated),
	})
	// Per-service allows.
	for i := range size {
		port := uint16(1024 + i)
		rules = append(rules, Rule{
			ID: fmt.Sprintf("allow-%d", i), Action: ActionAccept,
			Protocol: ProtoTCP, DstPort: PortRange{Lo: port, Hi: port}, HasDstPort: true,
			StateMask: maskOf(StateNew),
		})
	}
	return NewSnapshot(ActionDrop, rules)
}

func maskOf(states ...State) uint16 {
	var m uint16
	for _, s := range states {
		m |= 1 << uint(s)
	}
	return m
}

func benchTCPPacket(src, dst string, sport, dport uint16, st State, dir Direction) Packet {
	return Packet{
		Protocol:  ProtoTCP,
		Src:       netip.MustParseAddr(src),
		Dst:       netip.MustParseAddr(dst),
		SrcPort:   sport,
		DstPort:   dport,
		State:     st,
		Direction: dir,
		TCPState:  -1,
	}
}

func BenchmarkDecideSmallPolicyNew(b *testing.B) {
	s := benchSnapshot(1)
	p := benchTCPPacket("10.0.0.1", "10.0.0.2", 12345, 1024, StateNew, DirOriginal)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		v := s.Decide(p)
		if !v.Matched || v.Action != ActionAccept {
			b.Fatal("unexpected verdict")
		}
	}
}

func BenchmarkDecideSmallPolicyEstablished(b *testing.B) {
	s := benchSnapshot(0)
	p := benchTCPPacket("10.0.0.1", "10.0.0.2", 12345, 80, StateEstablished, DirReply)
	p.Accepted = true
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		v := s.Decide(p)
		if !v.Matched || v.Action != ActionAccept {
			b.Fatal("unexpected verdict")
		}
	}
}

func BenchmarkDecideLargePolicy(b *testing.B) {
	s := benchSnapshot(200)
	// Packet matching the last rule: worst-case rule walk.
	p := benchTCPPacket("10.0.0.1", "10.0.0.2", 12345, 1223, StateNew, DirOriginal)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		v := s.Decide(p)
		if !v.Matched || v.Action != ActionAccept {
			b.Fatal("unexpected verdict")
		}
	}
}

func BenchmarkDecideNoMatchDefaultDrop(b *testing.B) {
	s := benchSnapshot(0)
	p := benchTCPPacket("10.0.0.1", "10.0.0.2", 12345, 9999, StateNew, DirOriginal)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		v := s.Decide(p)
		if v.Matched {
			b.Fatal("expected default")
		}
	}
}

func BenchmarkKingProcessNewAccept(b *testing.B) {
	k := NewKing()
	k.Replace(benchSnapshot(1))
	p := benchTCPPacket("10.0.0.1", "10.0.0.2", 12345, 1024, StateNew, DirOriginal)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		v := k.Process(p)
		if !v.Accepted {
			b.Fatal("expected accepted NEW")
		}
	}
}

func BenchmarkKingProcessEstablished(b *testing.B) {
	k := NewKing()
	k.Replace(benchSnapshot(1))
	orig := benchTCPPacket("10.0.0.1", "10.0.0.2", 12345, 1024, StateNew, DirOriginal)
	k.Process(orig)
	p := benchTCPPacket("10.0.0.2", "10.0.0.1", 1024, 12345, StateEstablished, DirReply)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		v := k.Process(p)
		if v.Action != ActionAccept {
			b.Fatal("expected accept")
		}
	}
}

func BenchmarkTupleKey(b *testing.B) {
	p := benchTCPPacket("10.0.0.1", "10.0.0.2", 12345, 80, StateEstablished, DirReply)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if newTupleKey(p) == (tupleKey{}) {
			b.Fatal("empty key")
		}
	}
}
