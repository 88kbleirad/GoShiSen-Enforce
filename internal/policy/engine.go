package policy

import (
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
)

// State is the connection state of a packet. Zero is "unknown/any" and
// is used by stateless callers (e.g. the simulate CLI). The kernel's
// ip_conntrack_info values are mapped onto these at the backend boundary.
type State uint8

const (
	// StateNone is the zero value: no state information (stateless).
	StateNone State = 0
	// StateNew is the first packet of a connection.
	StateNew State = 1
	// StateEstablished signals traffic both ways on a tracked connection.
	StateEstablished State = 2
	// StateRelated is a packet of an auxiliary connection (e.g. ICMP
	// errors, FTP-data). Only queued TCP/UDP packets can express it.
	StateRelated State = 3
	// StateInvalid is an untracked or invalid packet.
	StateInvalid State = 4
)

// String renders the state as its policy spelling.
func (s State) String() string {
	switch s {
	case StateNew:
		return "NEW"
	case StateEstablished:
		return "ESTABLISHED"
	case StateRelated:
		return "RELATED"
	case StateInvalid:
		return "INVALID"
	}
	return "any"
}

// ParseState converts a policy state string.
func ParseState(s string) (State, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "NEW":
		return StateNew, nil
	case "ESTABLISHED":
		return StateEstablished, nil
	case "RELATED":
		return StateRelated, nil
	case "INVALID":
		return StateInvalid, nil
	}
	return 0, fmt.Errorf("state %q must be new, established, related, or invalid", s)
}

// Direction tells whether the packet travels in the connection's
// original direction or the reply direction.
type Direction uint8

const (
	// DirOriginal is the direction of the packet that created the
	// connection.
	DirOriginal Direction = iota
	// DirReply is the opposite direction.
	DirReply
)

// String renders the direction.
func (d Direction) String() string {
	if d == DirReply {
		return "reply"
	}
	return "original"
}

// Rule is a single compiled, immutable policy rule.
type Rule struct {
	ID          string
	Description string
	Src         Prefix
	Dst         Prefix
	SrcPort     PortRange
	DstPort     PortRange
	Action      Action
	Protocol    Proto

	// StateMask is a bitmask of the connection states this rule
	// matches; 0 means all states (stateless rule). State values are
	// 1..4, so bit n of StateMask corresponds to State n. Parsed from
	// the YAML state list at compile time; matching is a single
	// bitwise test instead of a slice scan.
	StateMask uint16

	HasSrcPort bool
	HasDstPort bool
	Log        bool
}

// String renders the rule in a one-line, human-readable form.
func (r Rule) String() string {
	srcPort := "-"
	if r.HasSrcPort {
		srcPort = r.SrcPort.String()
	}
	dstPort := "-"
	if r.HasDstPort {
		dstPort = r.DstPort.String()
	}
	return fmt.Sprintf("%s %s proto=%s src=%s dst=%s srcport=%s dstport=%s",
		r.ID, r.Action, r.Protocol, r.Src, r.Dst, srcPort, dstPort)
}

// Snapshot is an immutable compiled policy: a rule list in evaluation
// order plus the default action. It is never mutated after construction.
// A policy reload compiles a fresh Snapshot and swaps it atomically; an
// evaluation always sees one complete Snapshot, never a half-applied one.
type Snapshot struct {
	defaultAction Action
	rules         []Rule
}

// NewSnapshot builds a Snapshot from a default action and an ordered rule
// list. The caller must not mutate the slice afterwards.
func NewSnapshot(defaultAction Action, rules []Rule) *Snapshot {
	return &Snapshot{defaultAction: defaultAction, rules: rules}
}

// DefaultAction returns the action taken when no rule matches.
func (s *Snapshot) DefaultAction() Action { return s.defaultAction }

// Rules returns a defensive copy of the rule list, preserving snapshot
// immutability against caller mutation.
func (s *Snapshot) Rules() []Rule {
	if s.rules == nil {
		return nil
	}
	out := make([]Rule, len(s.rules))
	copy(out, s.rules)
	return out
}

// Len returns the number of rules.
func (s *Snapshot) Len() int { return len(s.rules) }

// King is a concurrently readable, atomically replaceable snapshot holder.
// Evaluations hold a pinned Snapshot pointer for the duration of one
// packet decision; replacing the policy only swaps the pointer, so an
// in-flight evaluation never observes a partially compiled policy.
//
// King also tracks the set of connections accepted by policy (their
// 5-tuples). The set is populated by Process' NEW-accept verdicts and
// consulted by stateful ESTABLISHED/RELATED matches.
type King struct {
	current     atomic.Pointer[Snapshot]
	accepted    sync.RWMutex
	acceptedMap map[tupleKey]struct{}
}

// tupleKey is an allocation-free 5-tuple key: two 64-bit words carry the
// address pair (IPv4 in lo/hi as 32-bit halves; IPv6 as the full 128-bit
// pair), and one 32-bit word carries protocol + both ports. Unlike the
// previous fmt.Sprintf-based string key, building it neither allocates
// nor formats.
type tupleKey struct {
	lo  uint64
	hi  uint64
	ppm uint32 // protocol<<16 | srcPort<<8 | dstPort
}

// newTupleKey canonicalizes reply packets (src/dst swapped) so they map
// to the same connection as the original direction. Without this, the
// reply direction never matches the accepted-set and ESTABLISHED/RELATED
// replies are dropped.
func newTupleKey(p Packet) tupleKey {
	src, dst := p.Src, p.Dst
	sport, dport := p.SrcPort, p.DstPort
	if p.Direction == DirReply {
		src, dst = dst, src
		sport, dport = dport, sport
	}
	var k tupleKey
	if src.Is4() {
		a4 := src.As4()
		b4 := dst.As4()
		k.lo = uint64(a4[0])<<24 | uint64(a4[1])<<16 | uint64(a4[2])<<8 | uint64(a4[3])
		k.hi = uint64(b4[0])<<24 | uint64(b4[1])<<16 | uint64(b4[2])<<8 | uint64(b4[3])
	} else {
		a16 := src.As16()
		b16 := dst.As16()
		k.lo = uint64(a16[0])<<56 | uint64(a16[1])<<48 | uint64(a16[2])<<40 | uint64(a16[3])<<32 |
			uint64(a16[4])<<24 | uint64(a16[5])<<16 | uint64(a16[6])<<8 | uint64(a16[7])
		k.hi = uint64(b16[0])<<56 | uint64(b16[1])<<48 | uint64(b16[2])<<40 | uint64(b16[3])<<32 |
			uint64(b16[4])<<24 | uint64(b16[5])<<16 | uint64(b16[6])<<8 | uint64(b16[7])
	}
	k.ppm = uint32(p.Protocol)<<16 | uint32(sport)<<8 | uint32(dport)
	return k
}

// NewKing returns a King whose initial snapshot is an empty, all-DROP
// policy (fail-closed before the first reload).
func NewKing() *King {
	k := &King{}
	k.current.Store(NewSnapshot(ActionDrop, nil))
	k.acceptedMap = make(map[tupleKey]struct{})
	return k
}

// Snapshot returns the current policy snapshot.
func (k *King) Snapshot() *Snapshot { return k.current.Load() }

// Replace atomically installs a new snapshot, returning the previous one.
// Accepted connections are NOT cleared: existing traffic keeps flowing.
func (k *King) Replace(s *Snapshot) *Snapshot { return k.current.Swap(s) }

// MarkAccepted records a connection as accepted by policy.
func (k *King) MarkAccepted(p Packet) {
	key := newTupleKey(p)
	k.accepted.Lock()
	k.acceptedMap[key] = struct{}{}
	k.accepted.Unlock()
}

// IsAccepted reports whether the connection was accepted.
func (k *King) IsAccepted(p Packet) bool {
	key := newTupleKey(p)
	k.accepted.RLock()
	_, ok := k.acceptedMap[key]
	k.accepted.RUnlock()
	return ok
}

// Process is the stateful decision surface. It evaluates p against the
// current snapshot, records accepted NEW connections, and stashes the
// accepted flag on the packet for stateful matches. It never mutates
// the snapshot.
func (k *King) Process(p Packet) Verdict {
	if p.State != StateNone && p.State != StateInvalid && k.IsAccepted(p) {
		p.Accepted = true
	}
	v := k.Snapshot().Decide(p)
	if v.Accepted {
		k.MarkAccepted(p)
	}
	return v
}

// Packet is the engine's view of one packet. It is independent of the
// NFQUEUE backend so the engine can be evaluated in tests and simulations
// without root or a network.
type Packet struct {
	// Protocol is the L4 protocol: ProtoTCP, ProtoUDP, or ProtoAny when
	// the protocol is not one of the two (the engine then falls back to
	// default DROP for the protocol axis).
	Protocol Proto

	// Src and Dst are the packet's IP addresses.
	Src netip.Addr
	Dst netip.Addr

	// SrcPort and DstPort are the L4 ports. Ignored for non-TCP/UDP.
	SrcPort uint16
	DstPort uint16

	// State is the connection state. Zero equals "any"/unset (used by
	// stateless callers).
	State State

	// Direction is the connection-relative direction. Only meaningful
	// when State != StateNone.
	Direction Direction

	// TCPState is the kernel's TCP conntrack state, or -1 when not a
	// TCP connection (e.g. UDP).
	TCPState int

	// Accepted is true when this connection was accepted in its original
	// direction. It is set by the engine on NEW-accept and consulted by
	// the ESTABLISHED/RELATED shortcut.
	Accepted bool
}

// Verdict is a decision.
type Verdict struct {
	// Action is the decision.
	Action Action
	// RuleID is the matching rule's ID, or "" when no rule matched.
	RuleID string
	// Matched reports whether a rule decided this packet (vs default).
	Matched bool
	// Accepted is true when this packet is a NEW original-direction
	// packet the policy accepted; the caller should record the
	// connection as accepted.
	Accepted bool
}

// Decide evaluates a packet against the snapshot's rule list. The first
// matching rule wins; if none matches, the default action applies. If
// the packet is a NEW connection that is accepted, Decide returns a
// verdict carrying Accepted=true so the caller can record the connection
// as accepted. The packet and snapshot are never modified.
func (s *Snapshot) Decide(p Packet) Verdict {
	var v Verdict
	for _, r := range s.rules {
		if r.matches(p) {
			v.Action = r.Action
			v.RuleID = r.ID
			v.Matched = true
			if r.Action == ActionAccept && p.State == StateNew && p.Direction == DirOriginal {
				v.Accepted = true
			}
			return v
		}
	}
	v.Action = s.defaultAction
	return v
}

// matches implements the per-rule test. A rule matches when every
// matcher it defines matches the packet:
//   - protocol: the packet's protocol must equal the rule's (or the
//     rule is "any", which matches both TCP and UDP),
//   - src/dst: prefix membership (any matches all),
//   - src_port/dst_port: inclusive range membership (only meaningful for
//     tcp/udp rules; the compiler enforces that).
func (r Rule) matches(p Packet) bool {
	if r.Protocol != ProtoAny && p.Protocol != r.Protocol {
		return false
	}
	if !r.Src.Contains(p.Src) || !r.Dst.Contains(p.Dst) {
		return false
	}
	if r.HasSrcPort && !r.SrcPort.Contains(p.SrcPort) {
		return false
	}
	if r.HasDstPort && !r.DstPort.Contains(p.DstPort) {
		return false
	}
	if r.StateMask != 0 {
		if p.State == StateNone || r.StateMask&(1<<uint(p.State)) == 0 {
			return false
		}
		// ESTABLISHED and RELATED matches require the connection to have
		// been accepted in its original direction. The five-tuple alone
		// is never proof that the connection is real.
		if (p.State == StateEstablished || p.State == StateRelated) && !p.Accepted {
			return false
		}
	}
	return true
}

// Simulate evaluates a packet against the current snapshot and returns
// the verdict and the matching rule ID. It never modifies system state.
func (k *King) Simulate(p Packet) Verdict {
	return k.Process(p)
}
