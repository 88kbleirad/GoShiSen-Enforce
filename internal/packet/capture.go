// Package packet implements the NFQUEUE transport backend for the policy
// engine. It speaks the nfnetlink_queue protocol directly over
// github.com/mdlayher/netlink (no cgo). The architecture document's
// primary dependency (go-nfnetlink) does not exist as a published module,
// so this direct implementation is the committed path.
package packet

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

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
	nfqaPayload        = 10
	nfqaCfgCmd         = 1
	nfqaCfgParams      = 2
	nfqaCfgQueueMaxlen = 3
	nfqaCT             = 11
	nfqaCTInfo         = 12

	nfqnlCfgCmdBind   = 1
	nfqnlCfgCmdUnbind = 2
	nfqnlCfgCmdPfBind = 3
	// nfqnlCfgCmdPfUnbind = 4 (unused)

	nfqnlCopyPacket = 2

	// nfqnl_attr_config indices.
	nfqaCfgMask  = 4
	nfqaCfgFlags = 5

	// nfqnl_attr_config flags.
	nfqaCfgFConnntrack = 1 << 1

	// nfqaCfgFFailOpen is NFQA_CFG_F_FAIL_OPEN: when set, the kernel
	// ACCEPTs packets it cannot deliver to userspace (queue full or
	// unicast failure). The daemon must NEVER set it — doing so would
	// break fail-closed behavior. It is defined here only so tests can
	// assert it stays clear.
	nfqaCfgFFailOpen = 1 << 0

	// nfnetlink_conntrack attributes (subset).
	ctaProtoInfo         = 4
	ctaProtoInfoTCP      = 2
	ctaProtoInfoTCPState = 1

	// ip_conntrack_info bits (nf_conntrack_common.h). The enum is
	// 0-based: ESTABLISHED=0, RELATED=1, NEW=2, IS_REPLY=3.
	ipCTEstablished = 0
	ipCTRelated     = 1
	ipCTNew         = 2
	ipCTIsReply     = 3

	nfAccept = 1
	nfDrop   = 0

	afInet  = 2
	afInet6 = 10
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
// The struct nlattr header (nla_len/nla_type) is read by the kernel in
// native byte order on the raw wire bytes; only the payload fields use
// explicit big-endian where the UAPI declares __be16/__be32.
func attr(typ uint16, payload []byte) []byte {
	l := 4 + len(payload)
	pad := (4 - l%4) % 4
	out := make([]byte, l+pad)
	// Native endian (host order) for the attribute header.
	binary.NativeEndian.PutUint16(out[0:], uint16(l+pad))
	binary.NativeEndian.PutUint16(out[2:], typ)
	copy(out[4:], payload)
	return out
}

// parseAttrs walks an attribute stream. Attribute headers are in native
// byte order (see attr); payload fields use the endianness declared by
// their UAPI types.
func parseAttrs(b []byte, fn func(typ uint16, payload []byte) bool) {
	for len(b) >= 4 {
		l := int(binary.NativeEndian.Uint16(b[0:]))
		if l < 4 || l > len(b) {
			return
		}
		typ := binary.NativeEndian.Uint16(b[2:])
		if !fn(typ, b[4:l]) {
			return
		}
		al := (l + 3) &^ 3
		// A malformed/truncated trailing attribute may have an aligned
		// length beyond the buffer; bail out instead of panicking.
		if al > len(b) {
			return
		}
		b = b[al:]
	}
}

// packetAttrsFetcher fetches one attribute from a stream. It returns the
// payload and true when found, false otherwise. Parsing is inlined in
// the hot path instead of walking the stream with a closure.
type packetAttrsFetcher func(typ uint16) ([]byte, bool)

// newPacketAttrsFetcher returns a fetcher over the given attribute
// stream. It parses the stream once per fetch; a fresh fetcher should be
// created for each message.
func newPacketAttrsFetcher(b []byte) packetAttrsFetcher {
	return func(typ uint16) ([]byte, bool) {
		for len(b) >= 4 {
			l := int(binary.NativeEndian.Uint16(b[0:]))
			if l < 4 || l > len(b) {
				return nil, false
			}
			if t := binary.NativeEndian.Uint16(b[2:]); t == typ {
				return b[4:l], true
			}
			al := (l + 3) &^ 3
			if al > len(b) {
				return nil, false
			}
			b = b[al:]
		}
		return nil, false
	}
}

// verdictBufPool reuses the per-verdict netlink message buffer. The
// kernel copies the contents on send, so the buffer can be reused for
// every verdict without a data race.
var verdictBufPool = sync.Pool{
	New: func() any { return make([]byte, 0, 20) },
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

func (b *Backend) noteError(kind string) {
	if b.observeErr != nil {
		b.observeErr(kind)
	}
}

// SetErrorObserver installs a callback invoked for backend errors
// (parse, panic, send) so the daemon can expose them as metrics.
func (b *Backend) SetErrorObserver(observe func(kind string)) {
	b.observeErr = observe
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
	conn6  *netlink.Conn
	queue  uint16
	maxlen int

	decide func(policy.Packet) policy.Verdict

	// observeErr, when set, is called for backend errors (parse,
	// panic, send) with a stable kind string so the daemon can expose
	// them as metrics.
	observeErr func(kind string)

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
	c6, err := netlink.Dial(unix.NETLINK_NETFILTER, nil)
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("netlink dial (v6): %w", err)
	}
	b := &Backend{conn: c, conn6: c6, queue: uint16(opts.Queue), maxlen: opts.Maxlen}
	if err := b.configure(); err != nil {
		c.Close()
		c6.Close()
		return nil, err
	}
	return b, nil
}

// ctFlagsTolerable reports whether a failure to configure conntrack
// attribute delivery should be tolerated. Kernels that build
// nfnetlink_queue without the conntrack glue reject
// NFQA_CFG_F_CONNTRACK with EOPNOTSUPP; the queue remains fully
// functional and packets arrive without NFQA_CT, which the engine
// classifies INVALID → fail-closed DROP. Any other error is fatal.
func ctFlagsTolerable(err error) bool {
	return errors.Is(err, unix.EOPNOTSUPP)
}

// configure binds two queue instances: IPv4 on the configured queue
// (AF_INET) and IPv6 on queue+1 (AF_INET6), matching the two queue
// rules the hook installs. Each family gets its own socket so the
// corresponding conntrack attributes are delivered for it. The
// conntrack-flags message is optional: see ctFlagsTolerable.
func (b *Backend) configure() error {
	if err := b.configureFamily(afInet, b.queue, b.conn); err != nil {
		return err
	}
	return b.configureFamily(afInet6, b.queue+1, b.conn6)
}

func (b *Backend) configureFamily(family uint8, queue uint16, c *netlink.Conn) error {
	msgs := b.configMessages(family, queue)
	// Split off the final conntrack-flags message.
	ctFlags := msgs[len(msgs)-1]
	msgs = msgs[:len(msgs)-1]
	for _, m := range msgs {
		if err := b.send(c, m); err != nil {
			return fmt.Errorf("nfqueue config: %w", err)
		}
	}
	if err := b.send(c, ctFlags); err != nil {
		if ctFlagsTolerable(err) {
			return nil
		}
		return fmt.Errorf("nfqueue config: %w", err)
	}
	return nil
}

// configMessages builds the exact NFQUEUE configuration sequence the
// kernel expects, in order:
//
//  1. BIND the queue (NFQNL_CFG_CMD_BIND).
//  2. Set copy mode to NFQNL_COPY_PACKET (copy whole packets).
//  3. Set the queue maximum length.
//  4. Request conntrack attributes (NFQA_CFG_F_CONNTRACK).
//
// The obsolete PF_BIND/PF_UNBIND commands are deliberately NOT sent:
// they are no-ops on modern kernels, and the old implementation sent a
// byte that the kernel decodes as UNBIND (2) — destroying the queue
// instance right after BIND. See Phase 8.4 report for the byte-level
// evidence.
func (b *Backend) configMessages(family uint8, queue uint16) []netlink.Message {
	gen := nfgenmsg{family: family, version: 0, resID: queue}
	msgs := make([]netlink.Message, 0, 4)

	// 1. BIND (command=1). pf is __be16 (AF_INET = 2, AF_INET6 = 10).
	msgs = append(msgs, configMsg(gen,
		attr(nfqaCfgCmd, []byte{nfqnlCfgCmdBind, 0, 0, family})))

	// 2. Copy mode: copy_range=0xffff, copy_mode=NFQNL_COPY_PACKET.
	params := make([]byte, 8)
	binary.BigEndian.PutUint32(params[0:], 0xffff)
	params[4] = nfqnlCopyPacket
	msgs = append(msgs, configMsg(gen, attr(nfqaCfgParams, params)))

	// 3. Maxlen.
	maxlen := make([]byte, 4)
	binary.BigEndian.PutUint32(maxlen, uint32(b.maxlen))
	msgs = append(msgs, configMsg(gen, attr(nfqaCfgQueueMaxlen, maxlen)))

	// 4. Conntrack config flags (never FAIL_OPEN).
	flags, mask := ctConfigFlags()
	flagsAttr := make([]byte, 4)
	binary.BigEndian.PutUint32(flagsAttr, flags)
	maskAttr := make([]byte, 4)
	binary.BigEndian.PutUint32(maskAttr, mask)
	msgs = append(msgs, configMsg(gen, attr(nfqaCfgMask, maskAttr), attr(nfqaCfgFlags, flagsAttr)))

	return msgs
}

// ctConfigFlags returns the config flags/mask the backend requests from
// the kernel. It exists so the fail-closed invariant (never requesting
// NFQA_CFG_F_FAIL_OPEN) is directly testable.
func ctConfigFlags() (flags, mask uint32) {
	return nfqaCfgFConnntrack, nfqaCfgFConnntrack
}

func configMsg(gen nfgenmsg, attrs ...[]byte) netlink.Message {
	data := gen.marshal()
	for _, a := range attrs {
		data = append(data, a...)
	}
	return netlink.Message{
		Header: netlink.Header{
			Type: queueMsg(nfqnlMsgConfig),
			// NLM_F_REQUEST | NLM_F_ACK: the kernel only sends a
			// reply when ACK is requested (errors are always
			// reported). b.send waits for that ACK.
			Flags: netlink.Request | netlink.Acknowledge,
		},
		Data: data,
	}
}

func (b *Backend) send(c *netlink.Conn, m netlink.Message) error {
	if _, err := c.Send(m); err != nil {
		return err
	}
	// Wait for the kernel acknowledgment. A netlink ACK is an
	// NLMSG_ERROR message (type Error) with error=0; an error response
	// has error!=0 and is surfaced by conn.Receive as an error (see
	// mdlayher checkMessage). So the first message we successfully
	// receive after a request is the ACK.
	for {
		msgs, err := c.Receive()
		if err != nil {
			return err
		}
		for _, m := range msgs {
			if m.Header.Type == netlink.Error {
				return nil
			}
		}
	}
}

// SetDecider installs the verdict function.
func (b *Backend) SetDecider(decide func(policy.Packet) policy.Verdict) {
	b.decide = decide
}

// Run receives packets on the IPv4 (queue N) and IPv6 (queue N+1)
// queue instances until ctx is cancelled. Every received packet
// receives exactly one verdict. On any anomaly (parse failure, engine
// panic) the packet is DROPped — always fail closed.
func (b *Backend) Run(ctx context.Context) error {
	errCh := make(chan error, 2)
	b.wg.Add(2)
	go func() { defer b.wg.Done(); errCh <- b.runSocket(ctx, b.conn, afInet) }()
	go func() { defer b.wg.Done(); errCh <- b.runSocket(ctx, b.conn6, afInet6) }()
	var firstErr error
	for range 2 {
		if err := <-errCh; err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (b *Backend) runSocket(ctx context.Context, c *netlink.Conn, family uint8) error {
	c.SetDeadline(time.Time{}) // clear any lingering deadline
	for {
		if ctx.Err() != nil {
			return nil
		}
		c.SetDeadline(time.Now().Add(500 * time.Millisecond))
		msgs, err := c.Receive()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue // deadline fired; check ctx again
			}
			b.Stats.SendErrors.Add(1)
			b.noteError("send")
			continue
		}
		for i := range msgs {
			m := msgs[i]
			if m.Header.Type != queueMsg(nfqnlMsgPacket) {
				continue
			}
			b.handlePacket(family, m)
		}
	}
}

// handlePacket processes one queued packet. It never returns without
// sending a verdict. Kernel NFQUEUE packet messages are framed as
// nlmsghdr + nfgenmsg(4) + attributes; the nfgenmsg must be skipped
// before attribute parsing.
func (b *Backend) handlePacket(family uint8, m netlink.Message) {
	b.Stats.PacketsSeen.Add(1)
	pktID, verdict := b.evaluate(packetAttrs(m.Data))
	if err := b.sendVerdict(family, pktID, verdict); err != nil {
		b.Stats.SendErrors.Add(1)
	}
	b.Stats.VerdictsSent.Add(1)
	if verdict == nfAccept {
		b.Stats.Accepted.Add(1)
	} else {
		b.Stats.Dropped.Add(1)
	}
}

// packetAttrs returns the attribute stream of an NFQUEUE message. The
// kernel frames packet messages as nlmsghdr + nfgenmsg(4) + attributes
// (mdlayher strips the nlmsghdr into Header, leaving nfgenmsg first in
// Data), so the leading 4-byte nfgenmsg must be skipped before
// attribute parsing.
func packetAttrs(data []byte) []byte {
	if len(data) >= 4 {
		return data[4:]
	}
	return data
}

// nfqueueAttrs is the decoded attribute set of one NFQUEUE packet
// message. It is stack-allocated per packet in evaluate; the slice
// fields alias the message buffer (no copying).
type nfqueueAttrs struct {
	pktID         uint32
	payload       []byte
	ctInfo        byte
	ctInfoPresent bool
	ctAttr        []byte
}

// parseNFQueueAttrs walks the attribute stream once and aggregates the
// attributes the decision path needs: packet id, packet payload,
// conntrack info bits and conntrack tuple blob. Unknown attributes are
// skipped. The result aliases b.
func parseNFQueueAttrs(b []byte) nfqueueAttrs {
	var a nfqueueAttrs
	for len(b) >= 4 {
		l := int(binary.NativeEndian.Uint16(b[0:]))
		if l < 4 || l > len(b) {
			return a
		}
		typ := binary.NativeEndian.Uint16(b[2:])
		p := b[4:l]
		switch typ {
		case nfqaPacketHdr:
			if len(p) >= 4 {
				a.pktID = binary.BigEndian.Uint32(p[0:])
			}
		case nfqaPayload:
			a.payload = p
		case nfqaCTInfo:
			// NFQA_CT_INFO is a __be32 (see ctnetlink_glue_build:
			// nla_put_be32(..., htonl(ctinfo))). Reading only the first
			// byte would always yield 0 for valid ctinfo values.
			if len(p) >= 4 {
				a.ctInfo = byte(binary.BigEndian.Uint32(p[0:4]))
				a.ctInfoPresent = true
			}
		case nfqaCT:
			a.ctAttr = p
		}
		al := (l + 3) &^ 3
		if al > len(b) {
			return a
		}
		b = b[al:]
	}
	return a
}

// evaluate parses the payload and decides. It recovers from any panic and
// returns DROP so a verdict is always produced.
func (b *Backend) evaluate(payload []byte) (uint32, int) {
	a := parseNFQueueAttrs(payload)
	if a.payload == nil {
		b.Stats.ParseErrors.Add(1)
		b.noteError("parse")
		return a.pktID, nfDrop
	}
	if b.decide == nil {
		return a.pktID, nfDrop
	}
	defer func() {
		if r := recover(); r != nil {
			b.Stats.Panics.Add(1)
			b.noteError("panic")
		}
	}()
	res := Parse(a.payload)
	if !res.OK {
		b.Stats.ParseErrors.Add(1)
		b.noteError("parse")
		return a.pktID, nfDrop
	}
	p := res.Packet
	applyCTInfo(&p, a.ctInfo, a.ctInfoPresent, a.ctAttr)
	v := b.decide(p)
	switch v.Action {
	case policy.ActionAccept:
		return a.pktID, nfAccept
	default:
		return a.pktID, nfDrop
	}
}

// applyCTInfo maps the kernel's conntrack attributes onto the engine
// packet's state/direction fields. When conntrack attrs are absent, the
// packet is INVALID (untracked) unless it is a NEW original-direction
// packet, which we leave stateless so stateless rules can still accept it.
// applyCTInfo maps the kernel's conntrack attributes onto the engine
// packet's state/direction fields.
//
// ctInfo is enum ip_conntrack_info (nf_conntrack_common.h): the values
// are additive — 0..2 are original direction (ESTABLISHED, RELATED,
// NEW) and IP_CT_IS_REPLY(3) is a threshold; reply packets have
// ctInfo >= 3 with base = ctInfo - IP_CT_IS_REPLY. present reports
// whether NFQA_CT_INFO was attached at all (a packet with no
// conntrack entry has none, and is INVALID/untracked).
func applyCTInfo(p *policy.Packet, ctInfo byte, present bool, ctAttr []byte) {
	if !present {
		p.State = policy.StateInvalid
		p.TCPState = -1
		return
	}
	info := int(ctInfo)
	reply := info >= ipCTIsReply
	base := info
	if reply {
		base = info - ipCTIsReply
		p.Direction = policy.DirReply
	} else {
		p.Direction = policy.DirOriginal
	}
	switch base {
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

// sendVerdict writes a verdict for a packet id on the socket of the
// family the packet arrived on (IPv4 queue N, IPv6 queue N+1).
func (b *Backend) sendVerdict(family uint8, pktID uint32, verdict int) error {
	c := b.conn
	queue := b.queue
	if family == afInet6 {
		c = b.conn6
		queue = b.queue + 1
	}
	// Reuse a pooled buffer for the verdict message. The kernel copies
	// the data on send, so reuse is race-free. Layout: nfgenmsg(4) +
	// verdict header attr (len/type(4) + 8-byte payload, aligned).
	buf := verdictBufPool.Get().([]byte)[:0]
	buf = append(buf, family, 0)
	buf = binary.BigEndian.AppendUint16(buf, queue)
	buf = binary.NativeEndian.AppendUint16(buf, 12) // nla_len: 4+8
	buf = binary.NativeEndian.AppendUint16(buf, nfqaVerdictHdr)
	buf = binary.BigEndian.AppendUint32(buf, uint32(verdict))
	buf = binary.BigEndian.AppendUint32(buf, pktID)
	m := netlink.Message{
		Header: netlink.Header{
			Type:  queueMsg(nfqnlMsgVerdict),
			Flags: netlink.Request,
		},
		Data: buf,
	}
	_, err := c.Send(m)
	verdictBufPool.Put(buf[:0])
	return err
}

// Close stops the backend and releases both sockets (IPv4 and IPv6).
func (b *Backend) Close() error {
	err4 := b.conn.Close()
	err6 := b.conn6.Close()
	if err4 != nil {
		return err4
	}
	return err6
}
