package policy

import (
	"net/netip"
	"testing"
)

const canonicalV6Policy = `
version: 1
default_action: DROP
rules:
  - id: allow-v6-new
    action: ACCEPT
    protocol: any
    src: fdcd:ea48:12a4::/64
    dst: fd00::/64
    state: [NEW]
  - id: allow-established
    action: ACCEPT
    protocol: any
    src: any
    dst: any
    state: [ESTABLISHED, RELATED]
`

// TestStatefulV6CanonicalPolicyFromCapturedPackets replays the exact
// captured v6 TCP packets (queue 101, OpenWrt 25.12.6 x86/64) through
// the engine with the canonical bidirectional-established policy.
func TestStatefulV6CanonicalPolicyFromCapturedPackets(t *testing.T) {
	snap, err := Compile([]byte(canonicalV6Policy))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	k := NewKing()
	k.Replace(snap)

	c := netip.MustParseAddr("fdcd:ea48:12a4::2")
	s := netip.MustParseAddr("fd00::2")

	// Captured: ctInfo=2 (NEW original).
	syn := Packet{Protocol: ProtoTCP, Src: c, Dst: s, SrcPort: 46160, DstPort: 8089,
		State: StateNew, Direction: DirOriginal, TCPState: -1}
	if v := k.Process(syn); v.Action != ActionAccept {
		t.Fatalf("SYN = %v, want ACCEPT", v.Action)
	}
	// Captured: ctInfo=3 (ESTABLISHED reply).
	synack := Packet{Protocol: ProtoTCP, Src: s, Dst: c, SrcPort: 8089, DstPort: 46160,
		State: StateEstablished, Direction: DirReply, TCPState: -1}
	if v := k.Process(synack); v.Action != ActionAccept {
		t.Fatalf("SYN-ACK = %v, want ACCEPT", v.Action)
	}
	// Captured: ctInfo=0 (ESTABLISHED original).
	ack := Packet{Protocol: ProtoTCP, Src: c, Dst: s, SrcPort: 46160, DstPort: 8089,
		State: StateEstablished, Direction: DirOriginal, TCPState: -1}
	if v := k.Process(ack); v.Action != ActionAccept {
		t.Fatalf("ACK = %v, want ACCEPT", v.Action)
	}
	// Captured: ctInfo=3 (ESTABLISHED reply).
	data := Packet{Protocol: ProtoTCP, Src: s, Dst: c, SrcPort: 8089, DstPort: 46160,
		State: StateEstablished, Direction: DirReply, TCPState: -1}
	if v := k.Process(data); v.Action != ActionAccept {
		t.Fatalf("data = %v, want ACCEPT", v.Action)
	}
}
