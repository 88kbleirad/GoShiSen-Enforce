// Command probe is a tiny network probe used by the integration test
// suite. It avoids depending on nc/socat being installed. It supports
// echo listeners and connect/send probes for TCP and UDP.
//
// Usage:
//
//	probe listen tcp 8080        # accept one connection, read, exit 0
//	probe listen udp 5353        # echo one datagram, exit 0
//	probe connect tcp HOST PORT  # dial; exit 0 on success
//	probe connect udp HOST PORT  # send+expect echo; exit 0 on reply
package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "probe: "+format+"\n", args...)
	os.Exit(1)
}

func listenTCP(port int) {
	l, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		die("listen tcp %d: %v", port, err)
	}
	defer l.Close()
	conn, err := l.Accept()
	if err != nil {
		die("accept: %v", err)
	}
	defer conn.Close()
	buf := make([]byte, 64)
	_, _ = conn.Read(buf)
}

// listenTCPEcho accepts connections, echoes every chunk back, and keeps
// each connection until the peer closes it (TCP handshake + response
// traffic exercise). It serves any number of connections until killed:
// tests exercise multiple exchanges (hold+reconnect, reload-while-
// established) against a single listener.
func listenTCPEcho(port int) {
	l, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		die("listen tcp-echo %d: %v", port, err)
	}
	defer l.Close()
	for {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		go func() {
			defer conn.Close()
			buf := make([]byte, 256)
			for {
				n, err := conn.Read(buf)
				if err != nil {
					return
				}
				if _, err := conn.Write(buf[:n]); err != nil {
					return
				}
			}
		}()
	}
}

// listenTCPEchoReport accepts connections, reports the remote address to
// the client, then echoes until close. Serves any number of connections
// until killed.
func listenTCPEchoReport(port int) {
	l, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		die("listen tcp-echo-report %d: %v", port, err)
	}
	defer l.Close()
	for {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		go func() {
			defer conn.Close()
			host, _, err := net.SplitHostPort(conn.RemoteAddr().String())
			if err != nil {
				host = conn.RemoteAddr().String()
			}
			if _, err := fmt.Fprintf(conn, "from %s\n", host); err != nil {
				return
			}
			buf := make([]byte, 256)
			for {
				n, err := conn.Read(buf)
				if err != nil {
					return
				}
				if _, err := conn.Write(buf[:n]); err != nil {
					return
				}
			}
		}()
	}
}

// connectTCPEchoReport connects, reads the "from" greeting, prints it,
// and exits 0.
func connectTCPEchoReport(host string, port int) {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 8*time.Second)
	if err != nil {
		die("connect tcp-echo-report %s:%d: %v", host, port, err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(20 * time.Second))
	buf := make([]byte, 64)
	n, err := conn.Read(buf)
	if err != nil {
		die("read report: %v", err)
	}
	line := strings.TrimSpace(string(buf[:n]))
	if !strings.HasPrefix(line, "from ") {
		die("unexpected greeting %q", line)
	}
	fmt.Println(line)
}

// talkHold connects to the target, exchanges one byte, then stays
// connected for hold seconds (keeps the conntrack entry alive).
func talkHold(host string, port, hold int) {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 8*time.Second)
	if err != nil {
		die("dial hold: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("h")); err != nil {
		die("write hold: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(20 * time.Second))
	buf := make([]byte, 8)
	if _, err := conn.Read(buf); err != nil {
		die("hold no reply: %v", err)
	}
	time.Sleep(time.Duration(hold) * time.Second)
	// Still alive after hold: keep a final write+read.
	if _, err := conn.Write([]byte("e")); err != nil {
		die("hold final write: %v", err)
	}
	_, _ = conn.Read(buf)
	fmt.Println("HELD")
}

func listenUDP(port int) {
	pc, err := net.ListenPacket("udp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		die("listen udp %d: %v", port, err)
	}
	defer pc.Close()
	buf := make([]byte, 64)
	for {
		n, addr, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}
		if _, err := pc.WriteTo(buf[:n], addr); err != nil {
			return
		}
	}
}

func connectTCP(host string, port int) {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 8*time.Second)
	if err != nil {
		die("connect tcp %s:%d: %v", host, port, err)
	}
	conn.Close()
	fmt.Println("CONNECTED")
}

func connectTCPEcho(host string, port int) {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 8*time.Second)
	if err != nil {
		die("connect tcp-echo %s:%d: %v", host, port, err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("ping")); err != nil {
		die("echo write: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(20 * time.Second))
	buf := make([]byte, 16)
	n, err := conn.Read(buf)
	if err != nil || string(buf[:n]) != "ping" {
		die("echo mismatch: n=%d err=%v", n, err)
	}
	fmt.Println("ECHOED")
}

// connectUDP sends one datagram and waits for an echo.
func connectUDP(host string, port int) {
	conn, err := net.DialTimeout("udp", net.JoinHostPort(host, strconv.Itoa(port)), 8*time.Second)
	if err != nil {
		die("dial udp: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("x")); err != nil {
		die("send udp: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(20 * time.Second))
	buf := make([]byte, 64)
	if _, err := conn.Read(buf); err != nil {
		die("no udp reply: %v", err)
	}
	fmt.Println("REPLIED")
}

// floodUDP sends count datagrams to host:port without waiting for
// replies, as fast as the kernel accepts them. It is used to overflow
// the NFQUEUE queue: the kernel drops what the daemon cannot absorb.
func floodUDP(host string, port, count int) {
	conn, err := net.Dial("udp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		die("dial udp flood: %v", err)
	}
	defer conn.Close()
	payload := []byte("flood")
	sent := 0
	for range count {
		if _, err := conn.Write(payload); err != nil {
			break
		}
		sent++
	}
	fmt.Printf("FLOODED %d\n", sent)
}

// httpGet performs an HTTP GET and prints "HTTP <code>"; exits nonzero
// on transport errors. Used by integration tests to probe the daemon's
// /healthz and /readyz inside the router namespace (no curl dependency).
func httpGet(host string, port int, path string) {
	url := fmt.Sprintf("http://%s:%d%s", host, port, path)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		die("http get %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("HTTP %d\n", resp.StatusCode)
	fmt.Print(string(body))
}

func main() {
	if len(os.Args) < 4 {
		die("usage: probe {listen|connect|flood|httpget} {tcp|udp} PORT|HOST [PORT] [COUNT]")
	}
	op, proto := os.Args[1], os.Args[2]
	port := func(s string) int {
		n, err := strconv.Atoi(s)
		if err != nil {
			die("bad port %q", s)
		}
		return n
	}
	switch op {
	case "listen":
		p := port(os.Args[3])
		if proto == "tcp" {
			listenTCP(p)
		} else {
			listenUDP(p)
		}
	case "listen-echo":
		listenTCPEcho(port(os.Args[3]))
	case "listen-echo-report":
		listenTCPEchoReport(port(os.Args[3]))
	case "connect":
		if proto == "tcp" {
			connectTCP(os.Args[3], port(os.Args[4]))
		} else {
			connectUDP(os.Args[3], port(os.Args[4]))
		}
	case "connect-echo":
		connectTCPEcho(os.Args[3], port(os.Args[4]))
	case "connect-echo-report":
		connectTCPEchoReport(os.Args[3], port(os.Args[4]))
	case "hold":
		talkHold(os.Args[3], port(os.Args[4]), port(os.Args[5]))
	case "flood":
		if proto != "udp" {
			die("flood only supports udp")
		}
		count := 10000
		if len(os.Args) > 5 {
			count = port(os.Args[5])
		}
		floodUDP(os.Args[3], port(os.Args[4]), count)
	case "httpget":
		if len(os.Args) < 5 {
			die("httpget requires HOST PORT PATH")
		}
		httpGet(os.Args[3], port(os.Args[4]), os.Args[5])
	default:
		die("unknown op %q", op)
	}
}
