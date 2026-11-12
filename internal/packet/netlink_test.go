package packet

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"

	"github.com/88kbleirad/GoShiSen-Enforce/internal/policy"
)

// These tests pin the exact wire encoding of our NFQUEUE netlink
// messages against the Linux UAPI layouts (verified against
// include/uapi/linux/netfilter/nfnetlink_queue.h and
// include/uapi/linux/netlink.h):
//
//	struct nlmsghdr { __u32 nlmsg_len; __u16 nlmsg_type; __u16
//	                   nlmsg_flags; __u32 nlmsg_seq; __u32 nlmsg_pid; }
//	struct nfgenmsg { __u8 nfgen_family; __u8 version; __be16 res_id; }
//	struct nfqnl_msg_config_cmd { __u8 command; __u8 _pad;
//	                              __be16 pf; }
//	struct nfqnl_msg_config_params { __be32 copy_range; __u8 copy_mode; }
//	struct nfqnl_msg_verdict_hdr { __be32 verdict; __be32 id; }
//
// Netfilter netlink message type = (subsys_id << 8) | msg_type.

func TestNFQUEUEMessageTypeEncoding(t *testing.T) {
	if got := queueMsg(nfqnlMsgPacket); got != netlink.HeaderType(3<<8|0) {
		t.Errorf("queueMsg(packet) = %#x, want %#x", got, netlink.HeaderType(3<<8))
	}
	if got := queueMsg(nfqnlMsgVerdict); got != netlink.HeaderType(3<<8|1) {
		t.Errorf("queueMsg(verdict) = %#x, want %#x", got, netlink.HeaderType(3<<8|1))
	}
	if got := queueMsg(nfqnlMsgConfig); got != netlink.HeaderType(3<<8|2) {
		t.Errorf("queueMsg(config) = %#x, want %#x", got, netlink.HeaderType(3<<8|2))
	}
}

func TestNFGenmsgMarshal(t *testing.T) {
	gen := nfgenmsg{family: afInet, version: 0, resID: 42}
	b := gen.marshal()
	want := []byte{2, 0, 0, 42} // family=AF_INET, version=0, res_id=42 BE
	if string(b) != string(want) {
		t.Errorf("marshal = % x, want % x", b, want)
	}
	if len(b) != 4 {
		t.Errorf("marshal length = %d, want 4", len(b))
	}
}

func TestAttrEncoding(t *testing.T) {
	// attr(type, payload): 2 bytes len + 2 bytes type in NATIVE byte
	// order (the kernel reads struct nlattr on the raw wire bytes), then
	// payload, padded to 4-byte alignment. On little-endian hosts the
	// header bytes are therefore reversed relative to big-endian.
	if !isLittleEndian() {
		t.Skip("test assumes little-endian host")
	}
	a := attr(1, []byte{0xaa, 0xbb})
	want := []byte{8, 0, 1, 0, 0xaa, 0xbb, 0, 0}
	if string(a) != string(want) {
		t.Errorf("attr(1,{aa bb}) = % x, want % x", a, want)
	}
	// Type field: native order. cmd type = NFQA_CFG_CMD(1).
	cmd := attr(nfqaCfgCmd, []byte{nfqnlCfgCmdBind, 0, 0, afInet})
	if len(cmd) != 8 {
		t.Errorf("cmd attr length = %d, want 8", len(cmd))
	}
	if binary.NativeEndian.Uint16(cmd[2:]) != 1 {
		t.Errorf("NFQA_CFG_CMD type = %d, want 1", binary.NativeEndian.Uint16(cmd[2:]))
	}
	if cmd[4] != nfqnlCfgCmdBind {
		t.Errorf("cmd byte = %d, want BIND(1)", cmd[4])
	}
	if cmd[6] != 0 || cmd[7] != afInet {
		t.Errorf("pf bytes = %d,%d, want 0,AF_INET(2) (BE16)", cmd[6], cmd[7])
	}
}

func TestConfigMessageGolden(t *testing.T) {
	// Full config message for BIND: nlmsghdr(16) + nfgenmsg(4) + attr.
	if !isLittleEndian() {
		t.Skip("test assumes little-endian host")
	}
	gen := nfgenmsg{family: afInet, version: 0, resID: 0}
	m := configMsg(gen, attr(nfqaCfgCmd, []byte{nfqnlCfgCmdBind, 0, 0, afInet}))

	if m.Header.Type != queueMsg(nfqnlMsgConfig) {
		t.Errorf("header type = %#x, want config", m.Header.Type)
	}
	if m.Header.Flags != netlink.Request|netlink.Acknowledge {
		t.Errorf("header flags = %#x, want REQUEST|ACK", m.Header.Flags)
	}
	wantData := []byte{
		2, 0, 0, 0, // nfgenmsg: family=AF_INET, version=0, res_id=0 (BE)
		8, 0, 1, 0, // attr (native): len=8, type=NFQA_CFG_CMD(1)
		1, 0, 0, 2, // nfqnl_msg_config_cmd: command=BIND(1), pad, pf=AF_INET (BE16)
	}
	if string(m.Data) != string(wantData) {
		t.Errorf("config data = % x\nwant % x", m.Data, wantData)
	}
}

func TestConfigParamsGolden(t *testing.T) {
	// Verify the copy-mode param attribute: copy_range=0xffff,
	// copy_mode=NFQNL_COPY_PACKET(2), type=NFQA_CFG_PARAMS(2).
	if !isLittleEndian() {
		t.Skip("test assumes little-endian host")
	}
	params := make([]byte, 8)
	binary.BigEndian.PutUint32(params[0:], 0xffff)
	params[4] = nfqnlCopyPacket
	a := attr(nfqaCfgParams, params)
	if binary.NativeEndian.Uint16(a[2:]) != 2 {
		t.Errorf("NFQA_CFG_PARAMS type = %d, want 2", binary.NativeEndian.Uint16(a[2:]))
	}
	if binary.BigEndian.Uint32(a[4:]) != 0xffff {
		t.Errorf("copy_range = %#x, want 0xffff", binary.BigEndian.Uint32(a[4:]))
	}
	if a[8] != nfqnlCopyPacket {
		t.Errorf("copy_mode = %d, want NFQNL_COPY_PACKET(2)", a[8])
	}
}

func TestVerdictMessageGolden(t *testing.T) {
	// Verdict message for queue 0, packet id 0xDEADBEEF, verdict DROP(0).
	if !isLittleEndian() {
		t.Skip("test assumes little-endian host")
	}
	gen := nfgenmsg{family: afInet, version: 0, resID: 0}
	hdr := make([]byte, 8)
	binary.BigEndian.PutUint32(hdr[0:], uint32(nfDrop))
	binary.BigEndian.PutUint32(hdr[4:], 0xDEADBEEF)
	data := gen.marshal()
	data = append(data, attr(nfqaVerdictHdr, hdr)...)

	// nfgenmsg + NFQA_VERDICT_HDR(2): verdict DROP(0), id 0xDEADBEEF.
	wantPrefix := []byte{2, 0, 0, 0, 12, 0, 2, 0}
	if string(data[:8]) != string(wantPrefix) {
		t.Errorf("verdict prefix = % x, want % x", data[:8], wantPrefix)
	}
	if binary.BigEndian.Uint32(data[8:]) != 0 {
		t.Errorf("verdict = %#x, want DROP(0)", binary.BigEndian.Uint32(data[8:]))
	}
	if binary.BigEndian.Uint32(data[12:]) != 0xDEADBEEF {
		t.Errorf("id = %#x, want 0xDEADBEEF", binary.BigEndian.Uint32(data[12:]))
	}
}

func TestCTAttrsGolden(t *testing.T) {
	// Nested conntrack attrs: CTA_PROTOINFO(4) -> CTA_PROTOINFO_TCP(2)
	// -> CTA_PROTOINFO_TCP_STATE(1) = 5 (ESTABLISHED). Attribute
	// headers are native byte order; the state payload is a u8.
	if !isLittleEndian() {
		t.Skip("test assumes little-endian host")
	}
	inner := synthCTAttr(5)
	if len(inner) != 16 {
		t.Errorf("synthCTAttr length = %d, want 16", len(inner))
	}
	// Outer attr: len=16, type=CTA_PROTOINFO(4); nested inner attr.
	if binary.NativeEndian.Uint16(inner[0:]) != 16 {
		t.Errorf("outer len = %d, want 16", binary.NativeEndian.Uint16(inner[0:]))
	}
	if binary.NativeEndian.Uint16(inner[2:]) != ctaProtoInfo {
		t.Errorf("outer type = %d, want CTA_PROTOINFO(4)", binary.NativeEndian.Uint16(inner[2:]))
	}
	if binary.NativeEndian.Uint16(inner[6:]) != ctaProtoInfoTCP {
		t.Errorf("middle type = %d, want CTA_PROTOINFO_TCP(2)", binary.NativeEndian.Uint16(inner[6:]))
	}
	if binary.NativeEndian.Uint16(inner[10:]) != ctaProtoInfoTCPState {
		t.Errorf("inner type = %d, want CTA_PROTOINFO_TCP_STATE(1)", binary.NativeEndian.Uint16(inner[10:]))
	}
	// Layout: outer attr header(4) -> tcp attr header(4) -> state attr
	// header(4); state payload at offset 12.
	if inner[12] != 5 {
		t.Errorf("tcp state byte = %d, want 5 (at offset 12)", inner[12])
	}
}

func TestNFQNLAttrNumbersMatchUAPI(t *testing.T) {
	// Pin the exact UAPI enum values (nfnetlink_queue.h). The packet
	// and config enums share type space values; check each separately.
	packetAttrs := map[string]int{
		"NFQA_PACKET_HDR":  nfqaPacketHdr,
		"NFQA_VERDICT_HDR": nfqaVerdictHdr,
		"NFQA_PAYLOAD":     nfqaPayload,
		"NFQA_CT":          nfqaCT,
		"NFQA_CT_INFO":     nfqaCTInfo,
	}
	wantPacket := map[string]int{
		"NFQA_PACKET_HDR":  1,
		"NFQA_VERDICT_HDR": 2,
		"NFQA_PAYLOAD":     10,
		"NFQA_CT":          11,
		"NFQA_CT_INFO":     12,
	}
	for name, got := range packetAttrs {
		if got != wantPacket[name] {
			t.Errorf("%s = %d, want UAPI %d", name, got, wantPacket[name])
		}
	}
	cfgAttrs := map[string]int{
		"NFQA_CFG_CMD":          nfqaCfgCmd,
		"NFQA_CFG_PARAMS":       nfqaCfgParams,
		"NFQA_CFG_QUEUE_MAXLEN": nfqaCfgQueueMaxlen,
	}
	wantCfg := map[string]int{
		"NFQA_CFG_CMD":          1,
		"NFQA_CFG_PARAMS":       2,
		"NFQA_CFG_QUEUE_MAXLEN": 3,
	}
	for name, got := range cfgAttrs {
		if got != wantCfg[name] {
			t.Errorf("%s = %d, want UAPI %d", name, got, wantCfg[name])
		}
	}
}

func TestNFQNLConfigFlagsGolden(t *testing.T) {
	// Only NFQA_CFG_F_CONNTRACK (1<<1) must ever be requested;
	// NFQA_CFG_F_FAIL_OPEN (1<<0) must never be set.
	flags, mask := ctConfigFlags()
	if flags != nfqaCfgFConnntrack {
		t.Errorf("flags = %#x, want CONNTRACK only %#x", flags, nfqaCfgFConnntrack)
	}
	if mask != nfqaCfgFConnntrack {
		t.Errorf("mask = %#x, want %#x", mask, nfqaCfgFConnntrack)
	}
	if flags&nfqaCfgFFailOpen != 0 {
		t.Fatal("FAIL_OPEN requested")
	}
}

func isLittleEndian() bool {
	return binary.NativeEndian.Uint16([]byte{1, 0}) == 1
}

// TestConfigSequenceGolden validates the exact outgoing configuration
// sequence against hard-coded Linux UAPI values (not derived from the
// implementation constants, so a duplicated wrong constant cannot make
// TestConfigSequenceGolden pins the four-message configuration sequence.
func TestConfigSequenceGolden(t *testing.T) {
	if !isLittleEndian() {
		t.Skip("test assumes little-endian host")
	}
	b := &Backend{queue: 0, maxlen: 1024}
	msgs := b.configMessages(afInet, b.queue)

	if len(msgs) != 4 {
		t.Fatalf("configMessages() = %d messages, want 4", len(msgs))
	}

	// All messages must be NFQNL_MSG_CONFIG (subsys 3 << 8 | type 2).
	for i, m := range msgs {
		if m.Header.Type != netlink.HeaderType(3<<8|2) {
			t.Errorf("msg %d: type = %#x, want 0x0302", i, m.Header.Type)
		}
		if m.Header.Flags != netlink.Request|netlink.Acknowledge {
			t.Errorf("msg %d: flags = %#x, want REQUEST|ACK", i, m.Header.Flags)
		}
		// nfgenmsg: family=AF_INET(2), version=0, res_id=0.
		gen := m.Data[:4]
		if gen[0] != 2 || gen[1] != 0 || gen[2] != 0 || gen[3] != 0 {
			t.Errorf("msg %d: nfgenmsg = % x, want 02 00 00 00", i, gen)
		}
	}

	// Message 1: BIND. Command attr type=1, payload command=1 (BIND),
	// pf=AF_INET as __be16 (00 02).
	m1 := msgs[0].Data[4:]
	if m1[0] != 8 || m1[1] != 0 || m1[2] != 1 || m1[3] != 0 {
		t.Errorf("BIND attr header = % x, want 08 00 01 00 (native)", m1[:4])
	}
	if m1[4] != 1 {
		t.Errorf("BIND command = %d, want NFQNL_CFG_CMD_BIND(1)", m1[4])
	}
	if m1[6] != 0 || m1[7] != 2 {
		t.Errorf("BIND pf = % x, want 00 02 (BE16 AF_INET)", m1[6:8])
	}

	// Message 2: PARAMS. Attr type=2, copy_range=0xffff (BE), copy_mode=2.
	m2 := msgs[1].Data[4:]
	if m2[2] != 2 || m2[3] != 0 {
		t.Errorf("PARAMS attr type = %d, want NFQA_CFG_PARAMS(2)", m2[2])
	}
	if m2[4] != 0 || m2[5] != 0 || m2[6] != 0xff || m2[7] != 0xff {
		t.Errorf("PARAMS copy_range = % x, want 00 00 ff ff (BE)", m2[4:8])
	}
	if m2[8] != 2 {
		t.Errorf("PARAMS copy_mode = %d, want NFQNL_COPY_PACKET(2)", m2[8])
	}

	// Message 3: MAXLEN. Attr type=3, payload = maxlen BE.
	m3 := msgs[2].Data[4:]
	if m3[2] != 3 || m3[3] != 0 {
		t.Errorf("MAXLEN attr type = %d, want NFQA_CFG_QUEUE_MAXLEN(3)", m3[2])
	}
	wantMaxlen := []byte{0, 0, 4, 0} // 1024 BE
	if string(m3[4:8]) != string(wantMaxlen) {
		t.Errorf("MAXLEN = % x, want % x (1024 BE)", m3[4:8], wantMaxlen)
	}

	// Message 4: MASK + FLAGS. Two attrs: type=4 (mask), type=5
	// (flags), both with payload = CONNTRACK (1<<1). FAIL_OPEN never set.
	m4 := msgs[3].Data[4:]
	maskAttr := m4[0:8]
	flagsAttr := m4[8:16]
	if maskAttr[2] != 4 || maskAttr[3] != 0 {
		t.Errorf("MASK attr type = %d, want NFQA_CFG_MASK(4)", maskAttr[2])
	}
	if flagsAttr[2] != 5 || flagsAttr[3] != 0 {
		t.Errorf("FLAGS attr type = %d, want NFQA_CFG_FLAGS(5)", flagsAttr[2])
	}
	maskVal := binary.BigEndian.Uint32(maskAttr[4:8])
	flagsVal := binary.BigEndian.Uint32(flagsAttr[4:8])
	if maskVal != 2 || flagsVal != 2 {
		t.Errorf("mask/flags = %d/%d, want 2/2 (CONNTRACK 1<<1)", maskVal, flagsVal)
	}
	if flagsVal&1 != 0 {
		t.Error("FAIL_OPEN (1<<0) requested in flags")
	}
}

// TestConfigCommandValuesUAPI pins the command mapping against the Linux
// UAPI so a duplicated wrong constant cannot pass unnoticed.
func TestConfigCommandValuesUAPI(t *testing.T) {
	if nfqnlCfgCmdBind != 1 {
		t.Errorf("nfqnlCfgCmdBind = %d, want NFQNL_CFG_CMD_BIND(1)", nfqnlCfgCmdBind)
	}
	if nfqnlCfgCmdUnbind != 2 {
		t.Errorf("nfqnlCfgCmdUnbind = %d, want NFQNL_CFG_CMD_UNBIND(2)", nfqnlCfgCmdUnbind)
	}
	if nfqnlCfgCmdPfBind != 3 {
		t.Errorf("nfqnlCfgCmdPfBind = %d, want NFQNL_CFG_CMD_PF_BIND(3)", nfqnlCfgCmdPfBind)
	}
}

// TestConfigSequenceNoUnbind asserts the initialization never sends an
// UNBIND or PF_BIND command (the original defect sent command byte 2,
// which the kernel decodes as UNBIND, destroying the queue).
func TestConfigSequenceNoUnbind(t *testing.T) {
	b := &Backend{queue: 0, maxlen: 1024}
	for i, m := range b.configMessages(afInet, b.queue) {
		data := m.Data[4:]
		for off := 0; off+8 <= len(data); {
			if data[off+2] == 1 && data[off+3] == 0 {
				cmd := data[off+4]
				if cmd == nfqnlCfgCmdUnbind {
					t.Errorf("msg %d: UNBIND(2) command present", i)
				}
				if cmd == nfqnlCfgCmdPfBind {
					t.Errorf("msg %d: PF_BIND(3) command present", i)
				}
			}
			off += 4
		}
	}
}

// TestCTInfoUAPIGolden pins the NFQA_CT_INFO wire format and the
// ip_conntrack_info enum against hard-coded values from the Linux UAPI
// headers (include/uapi/linux/netfilter/nf_conntrack_common.h and
// nfnetlink_queue.h), NOT from the implementation's constants.
//
//	enum ip_conntrack_info {
//	  IP_CT_ESTABLISHED,            // 0
//	  IP_CT_RELATED,                // 1
//	  IP_CT_NEW,                    // 2
//	  IP_CT_IS_REPLY,               // 3
//	  IP_CT_ESTABLISHED_REPLY = 3,  // 0+3
//	  IP_CT_RELATED_REPLY      = 4, // 1+3
//	  IP_CT_NEW_REPLY          = 5, // 2+3
//	};
func TestCTInfoUAPIGolden(t *testing.T) {
	const (
		uapiEstablished    = 0
		uapiRelated        = 1
		uapiNew            = 2
		uapiIsReply        = 3
		uapiEstablishedRpl = 3
		uapiRelatedRpl     = 4
		uapiNewRpl         = 5
	)
	// The implementation's constants must equal the UAPI values.
	if ipCTEstablished != uapiEstablished || ipCTRelated != uapiRelated ||
		ipCTNew != uapiNew || ipCTIsReply != uapiIsReply {
		t.Errorf("ip_conntrack_info constants do not match UAPI: est=%d rel=%d new=%d reply=%d (want %d/%d/%d/%d)",
			ipCTEstablished, ipCTRelated, ipCTNew, ipCTIsReply,
			uapiEstablished, uapiRelated, uapiNew, uapiIsReply)
	}
	// NFQA_CT_INFO wire format: 4-byte big-endian value. A 1-byte read
	// yields 0 for every valid ctinfo. Encode each UAPI value as the
	// kernel does (nla_put_be32(..., htonl(ctinfo))) and verify the
	// backend decodes the matching state/direction.
	cases := []struct {
		ctinfo    uint32
		wantState policy.State
		wantDir   policy.Direction
	}{
		{uapiEstablished, policy.StateEstablished, policy.DirOriginal},
		{uapiRelated, policy.StateRelated, policy.DirOriginal},
		{uapiNew, policy.StateNew, policy.DirOriginal},
		{uapiEstablishedRpl, policy.StateEstablished, policy.DirReply},
		{uapiRelatedRpl, policy.StateRelated, policy.DirReply},
		{uapiNewRpl, policy.StateNew, policy.DirReply},
	}
	tcpOK := buildIPv4(0, 6, "10.0.0.1", "10.0.0.2", 0, 0, 5, tcp4(12345, 80, 0x02))
	for _, tc := range cases {
		wire := make([]byte, 4)
		binary.BigEndian.PutUint32(wire, tc.ctinfo)
		b := &Backend{}
		var got policy.Packet
		b.decide = func(p policy.Packet) policy.Verdict {
			got = p
			return policy.Verdict{Action: policy.ActionAccept}
		}
		hdr := attr(nfqaPacketHdr, []byte{0, 0, 0, 1})
		payload := append(append(hdr, attr(nfqaPayload, tcpOK)...), attr(nfqaCTInfo, wire)...)
		if _, v := b.evaluate(payload); v != nfAccept {
			t.Fatalf("ctinfo=%d: verdict != accept", tc.ctinfo)
		}
		if got.State != tc.wantState || got.Direction != tc.wantDir {
			t.Errorf("ctinfo=%d: got state=%v dir=%v, want state=%v dir=%v",
				tc.ctinfo, got.State, got.Direction, tc.wantState, tc.wantDir)
		}
	}
}

// TestCTFlagsTolerable pins the fallback contract: only EOPNOTSUPP
// (kernel built without the conntrack delivery glue) is tolerated when
// configuring NFQA_CFG_F_CONNTRACK; every other error is fatal.
func TestCTFlagsTolerable(t *testing.T) {
	if !ctFlagsTolerable(unix.EOPNOTSUPP) {
		t.Error("EOPNOTSUPP must be tolerated")
	}
	for _, err := range []error{
		unix.EPERM,
		unix.EINVAL,
		unix.ENOBUFS,
		fmt.Errorf("netlink receive: %w", unix.EIO),
	} {
		if ctFlagsTolerable(err) {
			t.Errorf("%v must not be tolerated", err)
		}
	}
	if ctFlagsTolerable(nil) {
		t.Error("nil error must not be tolerated")
	}
}

// TestConfigSequenceEndsWithCTGFlags guards the configure() split: the
// conntrack-flags message must be the last config message, because
// configure() treats precisely that message as optional.
func TestConfigSequenceEndsWithCTGFlags(t *testing.T) {
	b := &Backend{queue: 0, maxlen: 1024}
	msgs := b.configMessages(afInet, b.queue)
	if len(msgs) != 4 {
		t.Fatalf("configMessages() = %d messages, want 4", len(msgs))
	}
	last := msgs[3].Data[4:]
	// Two attrs: mask (type 4) then flags (type 5).
	if last[2] != 4 {
		t.Errorf("last message first attr type = %d, want NFQA_CFG_MASK(4)", last[2])
	}
	if last[8+2] != 5 {
		t.Errorf("last message second attr type = %d, want NFQA_CFG_FLAGS(5)", last[8+2])
	}
}

// TestConfigSequenceIPv6Family pins that the IPv6 config sequence binds
// with AF_INET6 (10) so the inet hook's IPv6 queue instance is
// configured, not the v4 one.
func TestConfigSequenceIPv6Family(t *testing.T) {
	b := &Backend{queue: 100, maxlen: 1024}
	msgs := b.configMessages(afInet6, b.queue)
	if len(msgs) != 4 {
		t.Fatalf("configMessages(v6) = %d messages, want 4", len(msgs))
	}
	// First message: BIND with pf=AF_INET6.
	data := msgs[0].Data[4:]
	// attr header (4) + nfgenmsg already consumed: cmd byte at offset 4.
	if data[4] != nfqnlCfgCmdBind {
		t.Errorf("first v6 command = %d, want BIND(1)", data[4])
	}
	if data[7] != 10 {
		t.Errorf("v6 pf = %d, want AF_INET6(10)", data[7])
	}
}
