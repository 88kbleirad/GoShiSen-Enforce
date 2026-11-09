package policy

import (
	"fmt"
	"net/netip"
	"strings"
)

// Prefix is an IPv4 network matcher.
type Prefix struct {
	addr netip.Addr
	bits int
}

// ParsePrefix parses an IPv4 CIDR ("a.b.c.d/n") or "any". A bare address
// is treated as a host (/32). Malformed CIDRs are errors.
func ParsePrefix(s string) (Prefix, error) {
	s = trimLower(s)
	switch s {
	case "", "any":
		return AnyPrefix(), nil
	}
	if !strings.Contains(s, "/") {
		addr, err := netip.ParseAddr(s)
		if err != nil {
			return Prefix{}, fmt.Errorf("malformed CIDR %q (expected a.b.c.d/n or any)", s)
		}
		if !addr.Is4() {
			return Prefix{}, fmt.Errorf("CIDR %q is not IPv4", s)
		}
		return Prefix{addr: addr, bits: 32}, nil
	}
	addr, err := netip.ParsePrefix(s)
	if err != nil {
		return Prefix{}, fmt.Errorf("malformed CIDR %q (expected a.b.c.d/n or any)", s)
	}
	if !addr.Addr().Is4() {
		return Prefix{}, fmt.Errorf("CIDR %q is not IPv4", s)
	}
	return Prefix{addr: addr.Addr(), bits: addr.Bits()}, nil
}

// AnyPrefix matches every IPv4 address.
func AnyPrefix() Prefix {
	return Prefix{addr: netip.AddrFrom4([4]byte{}), bits: 0}
}

// Contains reports whether ip falls inside the prefix.
func (p Prefix) Contains(ip netip.Addr) bool {
	if !ip.Is4() {
		return false
	}
	pref := netip.PrefixFrom(p.addr, p.bits).Masked()
	return pref.Contains(ip)
}

func prefixFrom(p Prefix) netip.Prefix {
	return netip.PrefixFrom(p.addr, p.bits)
}

// String renders the prefix as its policy spelling ("any" for /0).
func (p Prefix) String() string {
	if p.bits == 0 {
		return "any"
	}
	return prefixFrom(p).String()
}

// CIDRBits exposes the prefix length (for tests).
func (p Prefix) CIDRBits() int { return p.bits }

func trimLower(s string) string {
	b := make([]byte, 0, len(s))
	for i := range s {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b = append(b, c)
	}
	return string(b)
}
