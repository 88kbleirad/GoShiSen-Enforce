package packet

import (
	"encoding/binary"
	"testing"

	"github.com/88kbleirad/GoShiSen-Enforce/internal/policy"
)

// TestEvaluateVerdicts exercises the backend's packet-decision logic
// without a kernel socket: it feeds synthetic NFQUEUE messages through
// evaluate and checks that every packet yields a verdict.
func TestEvaluateVerdicts(t *testing.T) {
	tcpOK := buildIPv4(0, 6, "10.0.0.1", "10.0.0.2", 0, 0, 5, tcp4(12345, 80, 0x02))
	udpOK := buildIPv4(0, 17, "10.0.0.1", "10.0.0.2", 0, 0, 5, udp4(12345, 53))
	garbage := []byte{0x45, 0x00}

	synth := func(id uint32, payload []byte) []byte {
		var data []byte
		hdr := make([]byte, 8)
		binary.BigEndian.PutUint32(hdr[0:], id)
		data = append(data, attr(nfqaPacketHdr, hdr)...)
		data = append(data, attr(nfqaPayload, payload)...)
		return data
	}

	cases := []struct {
		name    string
		payload []byte
		decide  func(policy.Packet) policy.Verdict
		want    int // nfAccept or nfDrop
	}{
		{
			name:    "accept tcp",
			payload: synth(1, tcpOK),
			decide:  func(policy.Packet) policy.Verdict { return policy.Verdict{Action: policy.ActionAccept} },
			want:    nfAccept,
		},
		{
			name:    "drop tcp",
			payload: synth(2, tcpOK),
			decide:  func(policy.Packet) policy.Verdict { return policy.Verdict{Action: policy.ActionDrop} },
			want:    nfDrop,
		},
		{
			name:    "udp",
			payload: synth(3, udpOK),
			decide:  func(policy.Packet) policy.Verdict { return policy.Verdict{Action: policy.ActionAccept} },
			want:    nfAccept,
		},
		{
			name:    "parse error drops",
			payload: synth(4, garbage),
			decide:  func(policy.Packet) policy.Verdict { return policy.Verdict{Action: policy.ActionAccept} },
			want:    nfDrop,
		},
		{
			name:    "no decider drops",
			payload: synth(5, tcpOK),
			want:    nfDrop,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := &Backend{}
			b.decide = tc.decide
			id, v := b.evaluate(tc.payload)
			if v != tc.want {
				t.Errorf("verdict = %d, want %d", v, tc.want)
			}
			if id == 0 {
				t.Error("packet id not parsed")
			}
			if tc.name == "parse error drops" && b.Stats.ParseErrors.Load() != 1 {
				t.Error("ParseErrors not incremented")
			}
		})
	}
}

// TestEvaluatePanicSafety verifies the panic recovery: a misbehaving
// decider must never leave a packet without a DROP verdict.
func TestEvaluatePanicSafety(t *testing.T) {
	pkt := buildIPv4(0, 6, "10.0.0.1", "10.0.0.2", 0, 0, 5, tcp4(1, 2, 0))
	payload := append(attr(nfqaPacketHdr, []byte{0, 0, 0, 9}), attr(nfqaPayload, pkt)...)
	b := &Backend{}
	b.decide = func(policy.Packet) policy.Verdict { panic("boom") }
	_, v := b.evaluate(payload)
	if v != nfDrop {
		t.Errorf("verdict = %d, want drop on panic", v)
	}
	if b.Stats.Panics.Load() != 1 {
		t.Error("Panics not incremented")
	}
}

// synthCTAttr builds a fake NFQA_CT payload containing an original tuple
// with a TCP protoinfo state.
func synthCTAttr(tcpState int) []byte {
	state := attr(ctaProtoInfoTCPState, []byte{byte(tcpState)})
	tcpInfo := attr(ctaProtoInfoTCP, state)
	return attr(ctaProtoInfo, tcpInfo)
}

func TestApplyCTInfo(t *testing.T) {
	tcp := tcp4(1, 2, 0)
	base := buildIPv4(0, 6, "10.0.0.1", "10.0.0.2", 0, 0, 5, tcp)
	ctAttr := synthCTAttr(5) // TCP_CONNTRACK_ESTABLISHED

	cases := []struct {
		name         string
		ctInfo       byte
		ctAttr       []byte
		wantState    policy.State
		wantDir      policy.Direction
		wantTCPState int
	}{
		{"no ct attrs", 0, nil, policy.StateInvalid, policy.DirOriginal, -1},
		{"new original", ipCTNew, ctAttr, policy.StateNew, policy.DirOriginal, 5},
		{"established original", ipCTEstablished, ctAttr, policy.StateEstablished, policy.DirOriginal, 5},
		{"established reply", ipCTEstablished | ipCTIsReply, ctAttr, policy.StateEstablished, policy.DirReply, 5},
		{"related reply", ipCTRelated | ipCTIsReply, ctAttr, policy.StateRelated, policy.DirReply, 5},
		{"no tcp protoinfo", ipCTEstablished, nil, policy.StateEstablished, policy.DirOriginal, -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := policy.Packet{}
			applyCTInfo(&p, tc.ctInfo, tc.ctAttr)
			if p.State != tc.wantState {
				t.Errorf("state = %v, want %v", p.State, tc.wantState)
			}
			if p.Direction != tc.wantDir {
				t.Errorf("direction = %v, want %v", p.Direction, tc.wantDir)
			}
			if p.TCPState != tc.wantTCPState {
				t.Errorf("tcp state = %d, want %d", p.TCPState, tc.wantTCPState)
			}
		})
	}

	// End-to-end: a packet with CT_INFO=ESTABLISHED|REPLY must reach the
	// decider with state/direction set.
	b := &Backend{}
	var got policy.Packet
	b.decide = func(p policy.Packet) policy.Verdict {
		got = p
		return policy.Verdict{Action: policy.ActionAccept}
	}
	hdr := attr(nfqaPacketHdr, []byte{0, 0, 0, 7})
	payload := append(append(hdr, attr(nfqaPayload, base)...), attr(nfqaCTInfo, []byte{ipCTEstablished | ipCTIsReply})...)
	id, v := b.evaluate(payload)
	if v != nfAccept {
		t.Fatalf("verdict = %d, want accept", v)
	}
	if id != 7 {
		t.Errorf("packet id = %d, want 7", id)
	}
	if got.State != policy.StateEstablished || got.Direction != policy.DirReply {
		t.Errorf("decider saw %+v, want established/reply", got)
	}
	_ = base
}

// TestFailClosedNeverRequestsFailOpen pins the fail-closed invariant:
// the backend must never set NFQA_CFG_F_FAIL_OPEN in its config flags.
// If it did, the kernel would ACCEPT packets the daemon never decided
// (queue full / unicast failure) — a bypass.
func TestFailClosedNeverRequestsFailOpen(t *testing.T) {
	flags, _ := ctConfigFlags()
	if flags&nfqaCfgFFailOpen != 0 {
		t.Fatalf("config flags request FAIL_OPEN (flags=%#x): queue overflow would bypass the firewall", flags)
	}
}

// TestHandlePacketKernelFraming verifies packetAttrs strips the 4-byte
// nfgenmsg prefix before attribute parsing, as the kernel frames packet
// messages (nlmsghdr + nfgenmsg + attributes). Without this, the parser
// reads the nfgenmsg as garbage attribute headers and drops every packet.
func TestHandlePacketKernelFraming(t *testing.T) {
	tcpOK := buildIPv4(0, 6, "10.0.0.1", "10.0.0.2", 0, 0, 5, tcp4(12345, 80, 0x02))
	// Kernel layout: nfgenmsg(4) + NFQA_PACKET_HDR + NFQA_PAYLOAD.
	hdr := make([]byte, 8)
	binary.BigEndian.PutUint32(hdr[0:], 42)
	var attrStream []byte
	attrStream = append(attrStream, attr(nfqaPacketHdr, hdr)...)
	attrStream = append(attrStream, attr(nfqaPayload, tcpOK)...)
	// Full kernel message Data (after nlmsghdr): nfgenmsg + attrs.
	kernelData := append([]byte{2, 0, 0, 0}, attrStream...)

	b := &Backend{}
	b.decide = func(policy.Packet) policy.Verdict { return policy.Verdict{Action: policy.ActionAccept} }
	id, v := b.evaluate(packetAttrs(kernelData))
	if v != nfAccept {
		t.Errorf("verdict = %d, want accept", v)
	}
	if id != 42 {
		t.Errorf("packet id = %d, want 42", id)
	}
	if b.Stats.ParseErrors.Load() != 0 {
		t.Errorf("ParseErrors = %d, want 0", b.Stats.ParseErrors.Load())
	}
}
