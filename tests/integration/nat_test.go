// NAT integration tests (Phase 6).
//
// Real DNAT and MASQUERADE through kernel nftables + conntrack. The
// daemon is started with a resolved file carrying the NAT config, inside
// the router namespace. These tests require root (CAP_NET_ADMIN) and are
// skipped with an explicit reason otherwise; no mocks.
package integration

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// startFirewallNAT starts the daemon with a config file (new NAT) plus
// policy file, inside the router namespace.
func startFirewallNAT(t *testing.T, natYAML, policyYAML string) {
	t.Helper()
	root := repoRoot(t)
	dir := t.TempDir()
	polPath := filepath.Join(dir, "policy.yaml")
	if err := os.WriteFile(polPath, []byte(policyYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	confPath := filepath.Join(dir, "cfg.yaml")
	conf := "interface: gsh-fw1\npolicy_file: " + polPath + "\n" + natYAML + "\n"
	if err := os.WriteFile(confPath, []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "goshisen")
	{
		cmd := exec.Command("go", "build", "-o", bin, "./cmd/goshisen")
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build daemon: %v: %s", err, out)
		}
	}

	cmd := exec.Command("ip", "netns", "exec", "gsh_fw", bin,
		"--config", confPath, "--log-level", "debug")
	cmd.Dir = root
	logFile, err := os.Create(filepath.Join(dir, "daemon.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { _, _ = cmd.Process.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
		}
	})
	time.Sleep(900 * time.Millisecond)
}

func TestNATDnat(t *testing.T) {
	if skip := checkPrereqs(t); skip != "" {
		t.Skip(skip)
	}
	buildProbe(t)
	runLab(t, true)
	defer runLab(t, false)

	// Server on 10.99.1.2:8080. Public IP 10.99.0.100:80 → DNAT.
	startStateProbe(t, "gsh_server", probeBin, "listen-echo-report", "tcp", "8080")

	nat := `
nat:
  enabled: true
  dnat:
    - name: web
      protocol: tcp
      pub_ip: 10.99.0.100
      pub_port: 80
      target_ip: 10.99.1.2
      target_port: 8080
`
	policy := `
version: 1
default_action: DROP
rules:
  - id: allow-dnat
    action: ACCEPT
    protocol: tcp
    src: 10.99.0.0/24
    dst: 10.99.1.0/24
    dst_port: 8080
    state: [NEW]
  - id: allow-estab
    action: ACCEPT
    protocol: tcp
    state: [ESTABLISHED, RELATED]
`
	startFirewallNAT(t, nat, policy)

	// Client connects to the public IP:80 → DNAT to server:8080. The
	// server sees the client's real (pre-MASQ) address.
	code, out := runStateProbe(t, "connect-echo-report", "tcp", "10.99.0.100", "80")
	if code != 0 {
		t.Fatalf("dnat connect failed: %d %s", code, out)
	}
	if !strings.Contains(out, "from 10.99.0.2") {
		t.Errorf("server saw %q, want from 10.99.0.2", out)
	}
}

func TestNATMasquerade(t *testing.T) {
	if skip := checkPrereqs(t); skip != "" {
		t.Skip(skip)
	}
	buildProbe(t)
	runLab(t, true)
	defer runLab(t, false)

	startStateProbe(t, "gsh_server", probeBin, "listen-echo-report", "tcp", "8081")

	nat := `
nat:
  enabled: true
  masquerade:
    enabled: true
    out_iface: gsh-fw1
`
	policy := `
version: 1
default_action: DROP
rules:
  - id: allow-http
    action: ACCEPT
    protocol: tcp
    dst: 10.99.1.2
    dst_port: 8081
    state: [NEW]
  - id: allow-estab
    action: ACCEPT
    protocol: tcp
    state: [ESTABLISHED, RELATED]
`
	startFirewallNAT(t, nat, policy)

	// Client→server: MASQUERADE rewrites the source to the router's
	// egress IP (10.99.1.1).
	code, out := runStateProbe(t, "connect-echo-report", "tcp", "10.99.1.2", "8081")
	if code != 0 {
		t.Fatalf("masq connect failed: %d %s", code, out)
	}
	if !strings.Contains(out, "from 10.99.1.1") {
		t.Errorf("server saw %q, want from 10.99.1.1", out)
	}
}

func TestNATReturnTraffic(t *testing.T) {
	if skip := checkPrereqs(t); skip != "" {
		t.Skip(skip)
	}
	buildProbe(t)
	runLab(t, true)
	defer runLab(t, false)

	startStateProbe(t, "gsh_server", probeBin, "listen-echo", "tcp", "8082")

	nat := `
nat:
  enabled: true
  dnat:
    - name: web
      protocol: tcp
      pub_ip: 10.99.0.100
      pub_port: 82
      target_ip: 10.99.1.2
      target_port: 8082
  masquerade:
    enabled: true
    out_iface: gsh-fw1
`
	policy := `
version: 1
default_action: DROP
rules:
  - id: allow-dnat
    action: ACCEPT
    protocol: tcp
    src: 10.99.0.0/24
    dst: 10.99.1.0/24
    dst_port: 8082
    state: [NEW]
  - id: allow-estab
    action: ACCEPT
    protocol: tcp
    state: [ESTABLISHED, RELATED]
`
	startFirewallNAT(t, nat, policy)

	// DNAT in + MASQUERADE out: bidirectional traffic must work (echo
	// back to the client).
	code, out := runStateProbe(t, "connect-echo", "tcp", "10.99.0.100", "82")
	if code != 0 {
		t.Fatalf("return traffic failed: %d %s", code, out)
	}
}

func TestNATWithFiltering(t *testing.T) {
	if skip := checkPrereqs(t); skip != "" {
		t.Skip(skip)
	}
	buildProbe(t)
	runLab(t, true)
	defer runLab(t, false)

	startStateProbe(t, "gsh_server", probeBin, "listen-echo-report", "tcp", "8083")

	nat := `
nat:
  enabled: true
  dnat:
    - name: web
      protocol: tcp
      pub_ip: 10.99.0.100
      pub_port: 83
      target_ip: 10.99.1.2
      target_port: 8083
`
	// Filtering runs AFTER DNAT: the engine sees the post-DNAT dst
	// (10.99.1.2:8083). A rule matching only the public IP would miss.
	// We prove the engine sees post-DNAT by allowing only the internal
	// target; allowing only the public dst would drop everything.
	policy := `
version: 1
default_action: DROP
rules:
  - id: allow-internal
    action: ACCEPT
    protocol: tcp
    src: 10.99.0.0/24
    dst: 10.99.1.2
    dst_port: 8083
    state: [NEW]
  - id: allow-estab
    action: ACCEPT
    protocol: tcp
    state: [ESTABLISHED, RELATED]
`
	startFirewallNAT(t, nat, policy)

	code, out := runStateProbe(t, "connect-echo-report", "tcp", "10.99.0.100", "83")
	if code != 0 {
		t.Fatalf("nat+filter failed: %d %s", code, out)
	}
	if !strings.Contains(out, "from 10.99.0.2") {
		t.Errorf("server saw %q", out)
	}
}

func TestNATReloadWhileConnections(t *testing.T) {
	if skip := checkPrereqs(t); skip != "" {
		t.Skip(skip)
	}
	buildProbe(t)
	runLab(t, true)
	defer runLab(t, false)

	startStateProbe(t, "gsh_server", probeBin, "listen-echo", "tcp", "8084")

	nat := `
nat:
  enabled: true
  dnat:
    - name: web
      protocol: tcp
      pub_ip: 10.99.0.100
      pub_port: 84
      target_ip: 10.99.1.2
      target_port: 8084
`
	policy1 := `
version: 1
default_action: DROP
rules:
  - id: allow-dnat
    action: ACCEPT
    protocol: tcp
    src: 10.99.0.0/24
    dst: 10.99.1.0/24
    dst_port: 8084
    state: [NEW]
  - id: allow-estab
    action: ACCEPT
    protocol: tcp
    state: [ESTABLISHED, RELATED]
`
	dir := t.TempDir()
	polPath := filepath.Join(dir, "policy.yaml")
	if err := os.WriteFile(polPath, []byte(policy1), 0o644); err != nil {
		t.Fatal(err)
	}
	confPath := filepath.Join(dir, "cfg.yaml")
	conf := "interface: gsh-fw1\npolicy_file: " + polPath + "\n" + nat + "\n"
	if err := os.WriteFile(confPath, []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}

	root := repoRoot(t)
	bin := filepath.Join(dir, "goshisen")
	{
		cmd := exec.Command("go", "build", "-o", bin, "./cmd/goshisen")
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build: %v: %s", err, out)
		}
	}
	cmd := exec.Command("ip", "netns", "exec", "gsh_fw", bin,
		"--config", confPath, "--log-level", "debug")
	cmd.Dir = root
	logFile, _ := os.Create(filepath.Join(dir, "daemon.log"))
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { _, _ = cmd.Process.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
		}
	})
	time.Sleep(200 * time.Millisecond)
	waitForDaemonReady(t, logFile.Name())

	// Establish a connection.
	code, out := runStateProbe(t, "connect-echo", "tcp", "10.99.0.100", "84")
	if code != 0 {
		t.Fatalf("initial connect failed: %d %s", code, out)
	}

	// listen-echo serves a single connection; the connect-echo probe
	// consumed the original listener. Start a fresh listener for the
	// hold probe.
	startStateProbe(t, "gsh_server", probeBin, "listen-echo", "tcp", "8084")

	// Hold a live connection open across the reload: the hold probe
	// dials the public IP (DNAT), exchanges one byte, then stays
	// connected for 4s doing periodic exchanges. The reload happens
	// mid-hold; the final exchange must still flow (conntrack keeps
	// the NAT transform; the accepted-set keeps ESTABLISHED).
	hold := exec.Command("ip", "netns", "exec", "gsh_client", probeBin,
		"hold", "tcp", "10.99.0.100", "84", "4")
	var holdOut bytes.Buffer
	hold.Stdout = &holdOut
	hold.Stderr = &holdOut
	if err := hold.Start(); err != nil {
		t.Fatalf("start hold: %v", err)
	}
	holdDone := make(chan error, 1)
	go func() { holdDone <- hold.Wait() }()
	t.Cleanup(func() {
		// holdDone is a one-shot channel: the test body consumes the
		// single send. If the body already did, the process is done —
		// never block waiting for a second send. If it hasn't, kill
		// and wait with a bound (killing `ip netns exec` may orphan
		// the probe inside the namespace; the lab teardown reaps it).
		select {
		case <-holdDone:
			return
		default:
		}
		_ = hold.Process.Kill()
		select {
		case <-holdDone:
		case <-time.After(5 * time.Second):
		}
	})
	time.Sleep(1500 * time.Millisecond) // handshake + first exchange done, holding

	// Reload: drop NEW for the internal target; established may stay.
	policy2 := `
version: 1
default_action: DROP
rules:
  - id: allow-estab
    action: ACCEPT
    protocol: tcp
    state: [ESTABLISHED, RELATED]
`
	if err := os.WriteFile(polPath, []byte(policy2), 0o644); err != nil {
		t.Fatal(err)
	}
	pidOut, err := runNs(t, "gsh_fw", "pgrep", "-f", "goshisen")
	if err != nil {
		t.Fatalf("pgrep: %v", err)
	}
	pid := strings.TrimSpace(pidOut)
	if _, err := runNs(t, "gsh_fw", "kill", "-HUP", pid); err != nil {
		t.Fatalf("HUP: %v", err)
	}
	time.Sleep(600 * time.Millisecond)

	// The held connection must survive the reload and complete its
	// final exchange (exit 0, HELD).
	select {
	case err := <-holdDone:
		if err != nil {
			t.Fatalf("held connection died across reload: %v (output: %s)", err, holdOut.String())
		}
		if !strings.Contains(holdOut.String(), "HELD") {
			t.Errorf("hold did not complete cleanly: %q", holdOut.String())
		}
	case <-time.After(8 * time.Second):
		t.Fatal("held connection did not finish within 8s")
	}

	// New connection to the public IP must now be dropped (NEW denied).
	code, _ = runStateProbe(t, "connect-echo", "tcp", "10.99.0.100", "84")
	if code == 0 {
		t.Fatal("new connection succeeded after reload dropped NEW")
	}
}
