package packet

import (
	"testing"

	"github.com/88kbleirad/GoShiSen-Enforce/internal/policy"
)

func BenchmarkParseIPv4TCP(b *testing.B) {
	pkt := buildIPv4(0, 6, "10.0.0.1", "10.0.0.2", 0, 0, 5, tcp4(12345, 80, 0x02))
	b.ReportAllocs()
	b.SetBytes(int64(len(pkt)))
	b.ResetTimer()
	for b.Loop() {
		r := Parse(pkt)
		if !r.OK || r.Packet.Protocol != policy.ProtoTCP {
			b.Fatal("parse failed")
		}
	}
}

func BenchmarkParseIPv4UDP(b *testing.B) {
	pkt := buildIPv4(0, 17, "10.0.0.1", "10.0.0.2", 0, 0, 5, udp4(12345, 53))
	b.ReportAllocs()
	b.SetBytes(int64(len(pkt)))
	b.ResetTimer()
	for b.Loop() {
		r := Parse(pkt)
		if !r.OK || r.Packet.Protocol != policy.ProtoUDP {
			b.Fatal("parse failed")
		}
	}
}

func BenchmarkParseIPv6TCP(b *testing.B) {
	pkt := buildIPv6(20, 6, "fd00::1", "fd00::2", tcp4(12345, 443, 0x02))
	b.ReportAllocs()
	b.SetBytes(int64(len(pkt)))
	b.ResetTimer()
	for b.Loop() {
		r := Parse(pkt)
		if !r.OK || r.Packet.Protocol != policy.ProtoTCP {
			b.Fatal("parse failed")
		}
	}
}

func BenchmarkParseIPv4Truncated(b *testing.B) {
	pkt := []byte{0x45, 0x00}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		r := Parse(pkt)
		if r.OK {
			b.Fatal("expected failure")
		}
	}
}
