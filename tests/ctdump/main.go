// Command ctdump is a temporary diagnostic: it binds NFQUEUE queue 0
// exactly like the daemon (BIND, copy mode, maxlen, CONNTRACK flag),
// then prints the netlink attribute types (and NFQA_CT_INFO value) of
// every queued packet, and verdicts it ACCEPT. Used to determine
// whether the kernel attaches NFQA_CT/NFQA_CT_INFO on this host.
package main

import (
	"encoding/binary"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

const (
	nlmsgHdrLen   = 16
	subsysQueue   = 3
	msgPacket     = 0
	msgVerdict    = 1
	msgConfig     = 2
	attrPacketID  = 1
	attrPayload   = 10
	attrCT        = 11
	attrCTInfo    = 12
	attrCfgCmd    = 1
	attrCfgParam  = 2
	attrCfgMaxlen = 3
	attrCfgMask   = 4
	attrCfgFlags  = 5
	cmdBind       = 1
	nfAccept      = 1
)

func attr(typ uint16, payload []byte) []byte {
	l := 4 + len(payload)
	pad := (4 - l%4) % 4
	out := make([]byte, l+pad)
	binary.NativeEndian.PutUint16(out[0:], uint16(l+pad))
	binary.NativeEndian.PutUint16(out[2:], typ)
	copy(out[4:], payload)
	return out
}

func msg(typ uint8, attrs ...[]byte) []byte {
	b := make([]byte, 0, 64)
	hdr := make([]byte, nlmsgHdrLen)
	binary.NativeEndian.PutUint16(hdr[4:], uint16(subsysQueue<<8|int(typ)))
	binary.NativeEndian.PutUint16(hdr[6:], unix.NLM_F_REQUEST|unix.NLM_F_ACK)
	b = append(b, hdr...)
	b = append(b, []byte{2, 0, 0, 0}...) // nfgenmsg: family=AF_INET
	for _, a := range attrs {
		b = append(b, a...)
	}
	binary.NativeEndian.PutUint32(b[0:], uint32(len(b)))
	return b
}

func send(fd int, m []byte) {
	if err := unix.Sendto(fd, m, 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		fmt.Println("sendto:", err)
	}
}

// cfg sends a config message and reads/prints the kernel ACK or error.
func cfg(fd int, label string, m []byte) {
	send(fd, m)
	buf := make([]byte, 4096)
	n, _, err := unix.Recvfrom(fd, buf, 0)
	if err != nil {
		fmt.Printf("CFG %s: recv err %v\n", label, err)
		return
	}
	if n >= 20 {
		typ := binary.NativeEndian.Uint16(buf[4:])
		code := int32(binary.NativeEndian.Uint32(buf[16:]))
		fmt.Printf("CFG %s: reply type=%d errno=%d\n", label, typ, code)
	} else {
		fmt.Printf("CFG %s: reply %d bytes\n", label, n)
	}
}

func main() {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW, unix.NETLINK_NETFILTER)
	if err != nil {
		fmt.Println("socket:", err)
		os.Exit(1)
	}
	defer unix.Close(fd)
	if err := unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		fmt.Println("bind:", err)
		os.Exit(1)
	}

	// Configure queue 0 exactly like the daemon.
	vh0 := make([]byte, 4)
	_ = vh0
	cfg(fd, "BIND", msg(msgConfig, attr(attrCfgCmd, []byte{cmdBind, 0, 0, 2})))
	params := make([]byte, 8)
	binary.BigEndian.PutUint32(params[0:], 0xffff)
	params[4] = 2 // COPY_PACKET
	cfg(fd, "PARAMS", msg(msgConfig, attr(attrCfgParam, params)))
	maxlen := make([]byte, 4)
	binary.BigEndian.PutUint32(maxlen, 1024)
	cfg(fd, "MAXLEN", msg(msgConfig, attr(attrCfgMaxlen, maxlen)))
	mask := make([]byte, 4)
	binary.BigEndian.PutUint32(mask, 2)
	flags := make([]byte, 4)
	binary.BigEndian.PutUint32(flags, 2)
	cfg(fd, "FLAGS(conntrack=2)", msg(msgConfig, attr(attrCfgMask, mask), attr(attrCfgFlags, flags)))

	tv := unix.Timeval{Sec: 8}
	_ = unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv)

	buf := make([]byte, 65536)
	for {
		n, _, err := unix.Recvfrom(fd, buf, 0)
		if err != nil {
			fmt.Println("recv done:", err)
			return
		}
		if n < nlmsgHdrLen {
			continue
		}
		typ := binary.NativeEndian.Uint16(buf[4:])
		if typ != uint16(subsysQueue<<8|msgPacket) {
			continue // ACK/other
		}
		// data = after nlmsghdr; skip nfgenmsg(4)
		data := buf[nlmsgHdrLen:n]
		if len(data) < 4 {
			continue
		}
		attrs := data[4:]
		var pktID uint32
		var sawCT, sawCTInfo bool
		var ctInfo byte
		types := []int{}
		for len(attrs) >= 4 {
			l := int(binary.NativeEndian.Uint16(attrs[0:]))
			at := int(binary.NativeEndian.Uint16(attrs[2:]))
			if l < 4 || l > len(attrs) {
				break
			}
			types = append(types, at)
			p := attrs[4:l]
			switch at {
			case attrPacketID:
				if len(p) >= 4 {
					pktID = binary.BigEndian.Uint32(p[0:])
				}
			case attrCT:
				sawCT = true
			case attrCTInfo:
				sawCTInfo = true
				if len(p) >= 4 {
					ctInfo = byte(binary.BigEndian.Uint32(p[0:4]))
					fmt.Printf("   CT_INFO raw bytes=% x value=%d\n", p[:4], binary.BigEndian.Uint32(p[:4]))
				}
			}
			al := (l + 3) &^ 3
			attrs = attrs[al:]
		}
		fmt.Printf("PKT id=%d types=%v CT=%v CT_INFO=%v ctinfo=%d\n", pktID, types, sawCT, sawCTInfo, ctInfo)
		// verdict: ACCEPT
		vh := make([]byte, 8)
		binary.BigEndian.PutUint32(vh[0:], nfAccept)
		binary.BigEndian.PutUint32(vh[4:], pktID)
		send(fd, msg(msgVerdict, attr(2, vh)))
	}
}
