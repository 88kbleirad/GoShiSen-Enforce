// Package packet parses raw IPv4/TCP/UDP packet bytes into the policy
// engine's Packet view. It is pure: it never allocates beyond the result,
// never panics on any input, and performs no I/O. Fragments and
// unsupported protocols follow the architecture document (defrag is the
// kernel's job; whatever reaches the engine defensively falls back to the
// protocol's "any" or default-DROP semantics).
package packet

import (
	"encoding/binary"
	"net/netip"

	"github.com/88kbleirad/GoShiSen-Enforce/internal/policy"
)

const (
	ipv4VersionIHL = 0 // byte 0: 4 bits version, 4 bits IHL
	ipv4TotalLen   = 2
	ipv4Fragment   = 6
	ipv4TTL        = 8
	ipv4Protocol   = 9
	ipv4SrcAddr    = 12
	ipv4DstAddr    = 16
	ipv4IHLMin     = 5 // 5 * 4 = 20 bytes, no options

	ipv4FlagMoreFragments  = 0x2000
	ipv4FragmentOffsetMask = 0x1fff

	tcpSrcPort = 0
	tcpDstPort = 2
	udpSrcPort = 0
	udpDstPort = 2

	protoTCP = 6
	protoUDP = 17

	// IPv6 fixed header (RFC 8200): 40 bytes. Version/TC/FlowLabel in
	// the first 4 bytes; payload length at offset 4; next header at
	// offset 6; hop limit at 7; addresses at 8 and 24.
	ipv6Version    = 0
	ipv6PayloadLen = 4
	ipv6NextHdr    = 6
	ipv6SrcAddr    = 8
	ipv6DstAddr    = 24
	ipv6HdrLen     = 40

	// IPv6 extension headers (next header values).
	ipv6ExtHopByHop = 0
	ipv6ExtRouting  = 43
	ipv6ExtFragment = 44
	ipv6ExtDestOpts = 60
	ipv6ExtNoNext   = 59
)

// Result is the outcome of parsing one packet.
type Result struct {
	// Packet is the engine view, valid when OK is true.
	Packet policy.Packet
	// OK is true when the packet was parsed as IPv4 TCP/UDP.
	OK bool
	// Unsupported is non-empty when the packet is a valid IPv4 packet
	// whose protocol is not TCP/UDP (e.g. ICMP, GRE). The architecture
	// drops those at the kernel hook; this is the defensive mirror.
	Unsupported string
	// Fragment is true when the packet carries IPv4 fragmentation flags.
	Fragment bool
	// Truncated is true when the buffer ended before the header the
	// packet claims to have.
	Truncated bool
	// Err is a human-readable reason when OK is false, for debug logs.
	Err string
}

// Parse inspects buf as an IPv4 or IPv6 packet and returns the engine's
// view. It never panics. On any malformed input it returns
// Result{OK:false, Err: <reason>}.
func Parse(buf []byte) Result {
	if len(buf) < 1 {
		return Result{Truncated: true, Err: "empty packet"}
	}
	switch buf[ipv4VersionIHL] >> 4 {
	case 4:
		return parse4(buf)
	case 6:
		return parse6(buf)
	default:
		return Result{Err: "not IPv4 or IPv6"}
	}
}

// parse4 inspects buf as an IPv4 packet.
func parse4(buf []byte) Result {
	if len(buf) < 20 {
		return Result{Truncated: true, Err: "packet shorter than IPv4 header"}
	}
	if buf[ipv4VersionIHL]>>4 != 4 {
		return Result{Err: "not IPv4"}
	}
	ihl := int(buf[ipv4VersionIHL] & 0x0f)
	if ihl < ipv4IHLMin {
		return Result{Err: "IPv4 IHL < 5"}
	}
	ipLen := int(ihl * 4)
	if len(buf) < ipLen {
		return Result{Truncated: true, Err: "packet shorter than IPv4 header with options"}
	}
	totalLen := int(binary.BigEndian.Uint16(buf[ipv4TotalLen:]))
	if totalLen < ipLen {
		return Result{Err: "IPv4 total length smaller than header"}
	}
	if totalLen > len(buf) {
		return Result{Truncated: true, Err: "IPv4 total length exceeds captured length"}
	}

	proto := buf[ipv4Protocol]
	src, ok1 := addr4(buf[ipv4SrcAddr:])
	dst, ok2 := addr4(buf[ipv4DstAddr:])
	if !ok1 || !ok2 {
		return Result{Err: "invalid IPv4 address bytes"}
	}

	fragField := binary.BigEndian.Uint16(buf[ipv4Fragment:])
	offset := fragField & ipv4FragmentOffsetMask
	more := fragField&ipv4FlagMoreFragments != 0
	if offset != 0 || more {
		// Non-initial fragments carry no L4 header. The kernel
		// reassembles before conntrack/forward, so this is a defensive
		// path: report it and let the caller drop.
		return Result{
			Fragment: true,
			Err:      "IPv4 fragment (reassembly is the kernel's job)",
		}
	}

	l4 := buf[ipLen:totalLen]
	switch proto {
	case protoTCP:
		if len(l4) < 20 {
			return Result{Truncated: true, Err: "truncated TCP header"}
		}
		return Result{
			OK: true,
			Packet: policy.Packet{
				Protocol: policy.ProtoTCP,
				Src:      src,
				Dst:      dst,
				SrcPort:  binary.BigEndian.Uint16(l4[tcpSrcPort:]),
				DstPort:  binary.BigEndian.Uint16(l4[tcpDstPort:]),
			},
		}
	case protoUDP:
		if len(l4) < 8 {
			return Result{Truncated: true, Err: "truncated UDP header"}
		}
		return Result{
			OK: true,
			Packet: policy.Packet{
				Protocol: policy.ProtoUDP,
				Src:      src,
				Dst:      dst,
				SrcPort:  binary.BigEndian.Uint16(l4[udpSrcPort:]),
				DstPort:  binary.BigEndian.Uint16(l4[udpDstPort:]),
			},
		}
	default:
		return Result{Unsupported: "protocol " + itoa(uint32(proto))}
	}
}

// parse6 inspects buf as an IPv6 packet. It walks extension headers
// (hop-by-hop, routing, fragment, destination options) to find the L4
// header. Non-initial fragments (fragment offset != 0) and fragment
// extension headers carrying a non-zero offset are reported as Fragment
// and dropped by the caller; the kernel normally reassembles before
// the forward hook, so this is a defensive path.
func parse6(buf []byte) Result {
	if len(buf) < ipv6HdrLen {
		return Result{Truncated: true, Err: "packet shorter than IPv6 header"}
	}
	payloadLen := int(binary.BigEndian.Uint16(buf[ipv6PayloadLen:]))
	if payloadLen > len(buf)-ipv6HdrLen {
		return Result{Truncated: true, Err: "IPv6 payload length exceeds captured length"}
	}
	src, ok1 := addr16(buf[ipv6SrcAddr:])
	dst, ok2 := addr16(buf[ipv6DstAddr:])
	if !ok1 || !ok2 {
		return Result{Err: "invalid IPv6 address bytes"}
	}

	off := ipv6HdrLen
	next := buf[ipv6NextHdr]
	end := ipv6HdrLen + payloadLen
	for {
		switch next {
		case ipv6ExtHopByHop, ipv6ExtRouting, ipv6ExtDestOpts:
			// Extension header: first byte is next header, second is
			// length in 8-octet units excluding the first 8 octets.
			if off+8 > len(buf) {
				return Result{Truncated: true, Err: "truncated IPv6 extension header"}
			}
			next = buf[off]
			off += 8 + int(buf[off+1])*8
		case ipv6ExtFragment:
			if off+8 > len(buf) {
				return Result{Truncated: true, Err: "truncated IPv6 fragment header"}
			}
			fragOff := int(binary.BigEndian.Uint16(buf[off+2:])) >> 3
			if fragOff != 0 {
				return Result{Fragment: true, Err: "IPv6 fragment (reassembly is the kernel's job)"}
			}
			next = buf[off]
			off += 8
		default:
			goto l4
		}
		if off > end {
			return Result{Truncated: true, Err: "IPv6 extension header runs past payload"}
		}
	}

l4:
	if off > end {
		return Result{Truncated: true, Err: "IPv6 L4 header runs past payload"}
	}
	l4 := buf[off:end]
	switch next {
	case protoTCP:
		if len(l4) < 20 {
			return Result{Truncated: true, Err: "truncated TCP header"}
		}
		return Result{
			OK: true,
			Packet: policy.Packet{
				Protocol: policy.ProtoTCP,
				Src:      src,
				Dst:      dst,
				SrcPort:  binary.BigEndian.Uint16(l4[tcpSrcPort:]),
				DstPort:  binary.BigEndian.Uint16(l4[tcpDstPort:]),
			},
		}
	case protoUDP:
		if len(l4) < 8 {
			return Result{Truncated: true, Err: "truncated UDP header"}
		}
		return Result{
			OK: true,
			Packet: policy.Packet{
				Protocol: policy.ProtoUDP,
				Src:      src,
				Dst:      dst,
				SrcPort:  binary.BigEndian.Uint16(l4[udpSrcPort:]),
				DstPort:  binary.BigEndian.Uint16(l4[udpDstPort:]),
			},
		}
	case ipv6ExtNoNext:
		return Result{Unsupported: "no next header"}
	default:
		return Result{Unsupported: "protocol " + itoa(uint32(next))}
	}
}

// addr16 reads a 16-byte IPv6 address without allocation.
func addr16(b []byte) (netip.Addr, bool) {
	if len(b) < 16 {
		return netip.Addr{}, false
	}
	var a16 [16]byte
	copy(a16[:], b[:16])
	return netip.AddrFrom16(a16), true
}

// addr4 reads a 4-byte IPv4 address without allocation.
func addr4(b []byte) (netip.Addr, bool) {
	if len(b) < 4 {
		return netip.Addr{}, false
	}
	var a4 [4]byte
	copy(a4[:], b[:4])
	return netip.AddrFrom4(a4), true
}

// itoa formats a small integer without fmt (parser hot path).
func itoa(v uint32) string {
	if v == 0 {
		return "0"
	}
	var buf [10]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
