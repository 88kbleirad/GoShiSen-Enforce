package packet

import (
	"encoding/binary"
	"testing"

	"github.com/mdlayher/netlink"
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
	// attr(type, payload): 2 bytes len (incl. 4-byte header + alignment
	// padding), 2 bytes type (big-endian), payload, padded to 4-byte
	// alignment. The kernel's nla_len includes the header and padding.
	a := attr(1, []byte{0xaa, 0xbb})
	want := []byte{0, 8, 0, 1, 0xaa, 0xbb, 0, 0}
	if string(a) != string(want) {
		t.Errorf("attr(1,{aa bb}) = % x, want % x", a, want)
	}
	// Type encoding is big-endian and must match the UAPI enum values.
	// This is the core regression: these were previously shifted by +3
	// (nfqaCfgCmd=4 instead of 1, nfqaPayload=3 instead of 10).
	cmd := attr(nfqaCfgCmd, []byte{nfqnlCfgCmdBind, 0, afInet, 0})
	if len(cmd) != 8 {
		t.Errorf("cmd attr length = %d, want 8", len(cmd))
	}
	if binary.BigEndian.Uint16(cmd[2:]) != 1 {
		t.Errorf("NFQA_CFG_CMD type = %d, want 1", binary.BigEndian.Uint16(cmd[2:]))
	}
	if cmd[4] != nfqnlCfgCmdBind {
		t.Errorf("cmd byte = %d, want BIND(1)", cmd[4])
	}
	if cmd[6] != afInet {
		t.Errorf("pf byte = %d, want AF_INET(2)", cmd[6])
	}
}

func TestConfigMessageGolden(t *testing.T) {
	// Full config message for BIND: nlmsghdr(16) + nfgenmsg(4) + attr.
	gen := nfgenmsg{family: afInet, version: 0, resID: 0}
	m := configMsg(gen, attr(nfqaCfgCmd, []byte{nfqnlCfgCmdBind, 0, afInet, 0}))

	if m.Header.Type != queueMsg(nfqnlMsgConfig) {
		t.Errorf("header type = %#x, want config", m.Header.Type)
	}
	if m.Header.Flags != netlink.Request {
		t.Errorf("header flags = %#x, want REQUEST", m.Header.Flags)
	}
	wantData := []byte{
		2, 0, 0, 0, // nfgenmsg: family=AF_INET, version=0, res_id=0
		0, 8, 0, 1, // attr: len=8, type=NFQA_CFG_CMD(1)
		1, 0, 2, 0, // nfqnl_msg_config_cmd: command=BIND(1), pad, pf=AF_INET
	}
	if string(m.Data) != string(wantData) {
		t.Errorf("config data = % x\nwant % x", m.Data, wantData)
	}
}

func TestConfigParamsGolden(t *testing.T) {
	// Verify the copy-mode param attribute: copy_range=0xffff,
	// copy_mode=NFQNL_COPY_PACKET(2), type=NFQA_CFG_PARAMS(2).
	params := make([]byte, 8)
	binary.BigEndian.PutUint32(params[0:], 0xffff)
	params[4] = nfqnlCopyPacket
	a := attr(nfqaCfgParams, params)
	if binary.BigEndian.Uint16(a[2:]) != 2 {
		t.Errorf("NFQA_CFG_PARAMS type = %d, want 2", binary.BigEndian.Uint16(a[2:]))
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
	gen := nfgenmsg{family: afInet, version: 0, resID: 0}
	hdr := make([]byte, 8)
	binary.BigEndian.PutUint32(hdr[0:], uint32(nfDrop))
	binary.BigEndian.PutUint32(hdr[4:], 0xDEADBEEF)
	data := gen.marshal()
	data = append(data, attr(nfqaVerdictHdr, hdr)...)

	// nfgenmsg + NFQA_VERDICT_HDR(2): verdict DROP(0), id 0xDEADBEEF.
	wantPrefix := []byte{2, 0, 0, 0, 0, 12, 0, 2}
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
	// -> CTA_PROTOINFO_TCP_STATE(1) = 5 (ESTABLISHED).
	inner := synthCTAttr(5)
	if len(inner) != 16 {
		t.Errorf("synthCTAttr length = %d, want 16", len(inner))
	}
	// Outer attr: len=16, type=CTA_PROTOINFO(4); nested inner attr.
	if binary.BigEndian.Uint16(inner[0:]) != 16 {
		t.Errorf("outer len = %d, want 16", binary.BigEndian.Uint16(inner[0:]))
	}
	if binary.BigEndian.Uint16(inner[2:]) != ctaProtoInfo {
		t.Errorf("outer type = %d, want CTA_PROTOINFO(4)", binary.BigEndian.Uint16(inner[2:]))
	}
	if binary.BigEndian.Uint16(inner[6:]) != ctaProtoInfoTCP {
		t.Errorf("middle type = %d, want CTA_PROTOINFO_TCP(2)", binary.BigEndian.Uint16(inner[6:]))
	}
	if binary.BigEndian.Uint16(inner[10:]) != ctaProtoInfoTCPState {
		t.Errorf("inner type = %d, want CTA_PROTOINFO_TCP_STATE(1)", binary.BigEndian.Uint16(inner[10:]))
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
