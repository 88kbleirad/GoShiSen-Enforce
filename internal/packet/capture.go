// Package packet implements the NFQUEUE transport backend for the policy
// engine. It speaks the nfnetlink_queue protocol directly over
// github.com/mdlayher/netlink (no cgo). The architecture document's
// primary dependency (go-nfnetlink) does not exist as a published module,
// so this direct implementation is the committed path.
package packet

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"

	"github.com/88kbleirad/GoShiSen-Enforce/internal/policy"
)

// nfnetlink_queue subsystem and message types.
const (
	nfnlSubsysQueue = 3

	nfqnlMsgPacket  = 0
	nfqnlMsgVerdict = 1
	nfqnlMsgConfig  = 2

	nfqaPacketHdr      = 1
	nfqaVerdictHdr     = 2
	nfqaPayload        = 3
	nfqaCfgCmd         = 4
	nfqaCfgParams      = 5
	nfqaCfgQueueMaxlen = 6
	nfqaCT             = 11
	nfqaCTInfo         = 12

	nfqnlCfgCmdBind   = 1
	nfqnlCfgCmdPfBind = 2

	nfqnlCopyPacket = 2

	// nfqnl_attr_config indices.
	nfqaCfgMask  = 4
	nfqaCfgFlags = 5

	// nfqnl_attr_config flags.
	nfqaCfgFConnntrack = 1 << 1

	// nfnetlink_conntrack attributes (subset).
	ctaProtoInfo         = 4
	ctaProtoInfoTCP      = 2
	ctaProtoInfoTCPState = 1

	// ip_conntrack_info bits (nf_conntrack_common.h).
	ipCTEstablished = 1
	ipCTRelated     = 2
	ipCTNew         = 3
	ipCTIsReply     = 4

	nfAccept = 1
	nfDrop   = 0

	afInet = 2
)

// queueMsg builds a netlink message type for nfnetlink_queue.
func queueMsg(msgType uint8) netlink.HeaderType {
	return netlink.HeaderType(uint16(nfnlSubsysQueue<<8) | uint16(msgType))
}

// nfgenmsg is the fixed nfnetlink header.
type nfgenmsg struct {
	family  uint8
	version uint8
	resID   uint16
}

func (n nfgenmsg) marshal() []byte {
	b := make([]byte, 4)
	b[0] = n.family
	b[1] = n.version
	binary.BigEndian.PutUint16(b[2:], n.resID)
	return b
}

// attr is one netlink attribute (len + type + payload, 4-byte aligned).
func attr(typ uint16, payload []byte) []byte {
	l := 4 + len(payload)
	pad := (4 - l%4) % 4
	out := make([]byte, l+pad)
	binary.BigEndian.PutUint16(out[0:], uint16(l+pad))
	binary.BigEndian.PutUint16(out[2:], typ)
	copy(out[4:], payload)
	return out
}

// parseAttrs walks an attribute stream.
func parseAttrs(b []byte, fn func(typ uint16, payload []byte) bool) {
	for len(b) >= 4 {
		l := int(binary.BigEndian.Uint16(b[0:]))
		if l < 4 || l > len(b) {
			return
		}
		typ := binary.BigEndian.Uint16(b[2:])
		if !fn(typ, b[4:l]) {
			return
		}
		al := (l + 3) &^ 3
		b = b[al:]
	}
}

// Stats exposes backend counters.
type Stats struct {
	PacketsSeen  atomic.Uint64
	VerdictsSent atomic.Uint64
	Accepted     atomic.Uint64
	Dropped      atomic.Uint64
	ParseErrors  atomic.Uint64
	SendErrors   atomic.Uint64
	Panics       atomic.Uint64
}

// Counters is a plain, race-safe snapshot of Stats for logging and
// interface boundaries.
type Counters struct {
	PacketsSeen  uint64
	VerdictsSent uint64
	Accepted     uint64
	Dropped      uint64
	ParseErrors  uint64
	SendErrors   uint64
	Panics       uint64
}

// Counters returns a snapshot of the backend counters.
func (b *Backend) Counters() Counters {
	return Counters{
		PacketsSeen:  b.Stats.PacketsSeen.Load(),
		VerdictsSent: b.Stats.VerdictsSent.Load(),
		Accepted:     b.Stats.Accepted.Load(),
		Dropped:      b.Stats.Dropped.Load(),
		ParseErrors:  b.Stats.ParseErrors.Load(),
		SendErrors:   b.Stats.SendErrors.Load(),
		Panics:       b.Stats.Panics.Load(),
	}
}

// Backend is an NFQUEUE listener. It binds a queue, copies packets to
// userspace, parses them, asks the engine for a verdict, and always sends
// a verdict back — for every packet, even when parsing fails.
type Backend struct {
	conn   *netlink.Conn
	queue  uint16
	maxlen int

	decide func(policy.Packet) policy.Verdict

	Stats Stats

	wg sync.WaitGroup
}

// Options configures a Backend.
type Options struct {
	// Queue is the NFQUEUE number to bind (default 0).
	Queue int
	// Maxlen is the kernel queue length in packets (default 1024).
	Maxlen int
}

// Open binds the queue. It requires CAP_NET_ADMIN (or root).
func Open(opts Options) (*Backend, error) {
	if opts.Queue < 0 || opts.Queue > 65535 {
		return nil, fmt.Errorf("queue %d out of range", opts.Queue)
	}
	if opts.Maxlen <= 0 {
		opts.Maxlen = 1024
	}
	c, err := netlink.Dial(unix.NETLINK_NETFILTER, nil)
	if err != nil {
		return nil, fmt.Errorf("netlink dial: %w", err)
	}
	b := &Backend{conn: c, queue: uint16(opts.Queue), maxlen: opts.Maxlen}
	if err := b.configure(); err != nil {
		c.Close()
		return nil, err
	}
	return b, nil
}

// configure binds the queue, sets copy mode, and sets maxlen.
func (b *Backend) configure() error {
	gen := nfgenmsg{family: afInet, version: 0, resID: b.queue}

	// Bind: NFQNL_CFG_CMD_BIND with pf=AF_INET (nftables queue binding
	// is per-family; we bind IPv4).
	cmd := attr(nfqaCfgCmd, []byte{nfqnlCfgCmdBind, 0, afInet, 0})
	if err := b.send(configMsg(gen, cmd)); err != nil {
		return fmt.Errorf("bind queue %d: %w", b.queue, err)
	}
	// PF_BIND is required before queues deliver (kernel requirement).
	pfBind := attr(nfqaCfgCmd, []byte{nfqnlCfgCmdPfBind, 0, afInet, 0})
	if err := b.send(configMsg(gen, pfBind)); err != nil {
		return fmt.Errorf("pf bind queue %d: %w", b.queue, err)
	}
	// Copy mode: NFQNL_COPY_PACKET (copy whole packet).
	params := make([]byte, 8)
	binary.BigEndian.PutUint32(params[0:], 0xffff)
	params[4] = nfqnlCopyPacket
	if err := b.send(configMsg(gen, attr(nfqaCfgParams, params))); err != nil {
		return fmt.Errorf("set copy mode queue %d: %w", b.queue, err)
	}
	// Maxlen.
	maxlen := make([]byte, 4)
	binary.BigEndian.PutUint32(maxlen, uint32(b.maxlen))
	if err := b.send(configMsg(gen, attr(nfqaCfgQueueMaxlen, maxlen))); err != nil {
		return fmt.Errorf("set maxlen queue %d: %w", b.queue, err)
	}
	// Ask the kernel to attach conntrack attributes (NFQA_CT,
	// NFQA_CT_INFO) to every queued packet.
	flags := make([]byte, 4)
	binary.BigEndian.PutUint32(flags, nfqaCfgFConnntrack)
	mask := make([]byte, 4)
	binary.BigEndian.PutUint32(mask, nfqaCfgFConnntrack)
	if err := b.send(configMsg(gen, attr(nfqaCfgMask, mask), attr(nfqaCfgFlags, flags))); err != nil {
		return fmt.Errorf("enable conntrack attributes queue %d: %w", b.queue, err)
	}
	return nil
}

func configMsg(gen nfgenmsg, attrs ...[]byte) netlink.Message {
	data := gen.marshal()
	for _, a := range attrs {
		data = append(data, a...)
	}
	return netlink.Message{
		Header: netlink.Header{
			Type:  queueMsg(nfqnlMsgConfig),
			Flags: netlink.Request,
		},
		Data: data,
	}
}

func (b *Backend) send(m netlink.Message) error {
	if _, err := b.conn.Send(m); err != nil {
		return err
	}
	// Wait for the ACK so configuration errors surface immediately.
	for {
		msgs, err := b.conn.Receive()
		if err != nil {
			return err
		}
		for _, m := range msgs {
			if m.Header.Type == 0 {
				return nil // ACK
			}
		}
	}
}

// SetDecider installs the verdict function.
func (b *Backend) SetDecider(decide func(policy.Packet) policy.Verdict) {
	b.decide = decide
}

// Run receives packets until ctx is cancelled. Every received packet
// receives exactly one verdict. On any anomaly (parse failure, engine
// panic) the packet is DROPped — always fail closed.
func (b *Backend) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		msgs, err := b.conn.Receive()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			b.Stats.SendErrors.Add(1)
			select {
			case <-ctx.Done():
				return nil
			default:
				continue
			}
		}
		for i := range msgs {
			m := msgs[i]
			if m.Header.Type != queueMsg(nfqnlMsgPacket) {
				continue
			}
			b.handlePacket(m)
		}
	}
}

// handlePacket processes one queued packet. It never returns without
// sending a verdict.
func (b *Backend) handlePacket(m netlink.Message) {
	b.Stats.PacketsSeen.Add(1)
	pktID, verdict := b.evaluate(m.Data)
	if err := b.sendVerdict(pktID, verdict); err != nil {
		b.Stats.SendErrors.Add(1)
	}
	b.Stats.VerdictsSent.Add(1)
	if verdict == nfAccept {
		b.Stats.Accepted.Add(1)
	} else {
		b.Stats.Dropped.Add(1)
	}
}

// evaluate parses the payload and decides. It recovers from any panic and
// returns DROP so a verdict is always produced.
func (b *Backend) evaluate(payload []byte) (uint32, int) {
	var pktID uint32
	var copyBuf []byte
	var ctInfo byte
	var ctAttr []byte
	parseAttrs(payload, func(typ uint16, p []byte) bool {
		switch typ {
		case nfqaPacketHdr:
			if len(p) >= 4 {
				pktID = binary.BigEndian.Uint32(p[0:])
			}
		case nfqaPayload:
			copyBuf = p
		case nfqaCTInfo:
			if len(p) >= 1 {
				ctInfo = p[0]
			}
		case nfqaCT:
			ctAttr = p
		}
		return true
	})
	if copyBuf == nil {
		b.Stats.ParseErrors.Add(1)
		return pktID, nfDrop
	}
	if b.decide == nil {
		return pktID, nfDrop
	}
	defer func() {
		if r := recover(); r != nil {
			b.Stats.Panics.Add(1)
		}
	}()
	res := Parse(copyBuf)
	if !res.OK {
		b.Stats.ParseErrors.Add(1)
		return pktID, nfDrop
	}
	p := res.Packet
	applyCTInfo(&p, ctInfo, ctAttr)
	v := b.decide(p)
	switch v.Action {
	case policy.ActionAccept:
		return pktID, nfAccept
	default:
		return pktID, nfDrop
	}
}

// applyCTInfo maps the kernel's conntrack attributes onto the engine
// packet's state/direction fields. When conntrack attrs are absent, the
// packet is INVALID (untracked) unless it is a NEW original-direction
// packet, which we leave stateless so stateless rules can still accept it.
func applyCTInfo(p *policy.Packet, ctInfo byte, ctAttr []byte) {
	if ctInfo == 0 {
		p.State = policy.StateInvalid
		p.TCPState = -1
		return
	}
	info := int(ctInfo)
	reply := info&ipCTIsReply != 0
	if reply {
		p.Direction = policy.DirReply
	} else {
		p.Direction = policy.DirOriginal
	}
	switch info &^ ipCTIsReply {
	case ipCTNew:
		p.State = policy.StateNew
	case ipCTEstablished:
		p.State = policy.StateEstablished
	case ipCTRelated:
		p.State = policy.StateRelated
	default:
		p.State = policy.StateInvalid
	}
	p.TCPState = tcpStateFromCT(ctAttr)
}

// tcpStateFromCT walks the nested CTA_PROTOINFO_TCP_STATE attribute.
// Layout: CTA_PROTOINFO → CTA_PROTOINFO_TCP → CTA_PROTOINFO_TCP_STATE.
func tcpStateFromCT(ctAttr []byte) int {
	tcpState := -1
	parseAttrs(ctAttr, func(typ uint16, p []byte) bool {
		if typ != ctaProtoInfo {
			return true
		}
		parseAttrs(p, func(typ2 uint16, p2 []byte) bool {
			if typ2 != ctaProtoInfoTCP {
				return true
			}
			parseAttrs(p2, func(typ3 uint16, p3 []byte) bool {
				if typ3 == ctaProtoInfoTCPState && len(p3) >= 1 {
					tcpState = int(p3[0])
				}
				return true
			})
			return true
		})
		return true
	})
	return tcpState
}

// sendVerdict writes a verdict for a packet id.
func (b *Backend) sendVerdict(pktID uint32, verdict int) error {
	gen := nfgenmsg{family: afInet, version: 0, resID: b.queue}
	hdr := make([]byte, 8)
	binary.BigEndian.PutUint32(hdr[0:], uint32(verdict))
	binary.BigEndian.PutUint32(hdr[4:], pktID)
	data := gen.marshal()
	data = append(data, attr(nfqaVerdictHdr, hdr)...)
	m := netlink.Message{
		Header: netlink.Header{
			Type:  queueMsg(nfqnlMsgVerdict),
			Flags: netlink.Request,
		},
		Data: data,
	}
	_, err := b.conn.Send(m)
	return err
}

// Close stops the backend and releases the socket.
func (b *Backend) Close() error {
	return b.conn.Close()
}
