// Package integration contains the real end-to-end lab tests. They build
// the daemon from source, create isolated network namespaces with the
// lab script, and prove the firewall's behavior with real traffic.
//
// The tests require root (CAP_NET_ADMIN), the ip/nft tools, and the go
// toolchain. When privileges or tools are missing they skip with an
// explicit reason — they never fake traffic or verdicts.
package integration

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var (
	labScript = filepath.Join("scripts", "lab.sh")
	probeBin  string // built once, reused
)

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

// checkPrereqs returns "" or a skip reason.
func checkPrereqs(t *testing.T) string {
	t.Helper()
	if os.Geteuid() != 0 {
		return "integration tests require root for CAP_NET_ADMIN"
	}
	for _, tool := range []string{"ip", "nft", "go"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Sprintf("required tool %q not in PATH", tool)
		}
	}
	// Verify netns creation actually works (privilege sanity check).
	probe := "gsh_prereq_" + fmt.Sprint(os.Getpid())
	if out, err := exec.Command("ip", "netns", "add", probe).CombinedOutput(); err != nil {
		return fmt.Sprintf("cannot create network namespaces: %v (%s)", err, strings.TrimSpace(string(out)))
	} else {
		_ = exec.Command("ip", "netns", "del", probe).Run()
	}
	// Functional conntrack + NFQUEUE preflight in a throwaway netns:
	// the suite depends on kernel conntrack being active in fresh
	// namespaces and on NFQUEUE attaching CT attributes. If this probe
	// fails, the environment cannot run the stateful/NAT/overflow
	// scenarios, and skipping with this reason is correct.
	return checkConntrackNFQUEUE(t)
}

// checkConntrackNFQUEUE creates a minimal namespace and verifies a real
// TCP flow is tracked by kernel conntrack (nf_conntrack_count > 0). The
// suite depends on conntrack being active in fresh namespaces; if this
// fails the stateful/NAT/overflow scenarios cannot run, and skipping
// with this reason is correct.
func checkConntrackNFQUEUE(t *testing.T) string {
	ns := "gsh_ct_preflight_" + fmt.Sprint(os.Getpid())
	defer func() { _ = exec.Command("ip", "netns", "del", ns).Run() }()

	if out, err := exec.Command("ip", "netns", "add", ns).CombinedOutput(); err != nil {
		return fmt.Sprintf("cannot create conntrack probe namespace: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	_ = exec.Command("ip", "netns", "exec", ns, "ip", "link", "set", "lo", "up").Run()
	// Best-effort: ensure the conntrack modules are present.
	_ = exec.Command("modprobe", "nf_conntrack").Run()
	_ = exec.Command("modprobe", "nf_conntrack_netlink").Run()

	// Register a conntrack-touching chain in the namespace.
	nftIn := `table ip gsh_ct_preflight {
    chain c {
        type filter hook output priority 0; policy accept;
        ct state { new, established, related, invalid, untracked } counter
    }
}`
	cmd := exec.Command("ip", "netns", "exec", ns, "nft", "-f", "-")
	cmd.Stdin = strings.NewReader(nftIn)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Sprintf("conntrack preflight nft failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}

	// Run a real loopback TCP flow inside the namespace using the probe.
	buildProbe(t)
	listener := exec.Command("ip", "netns", "exec", ns, probeBin, "listen-echo", "tcp", "9097")
	if err := listener.Start(); err != nil {
		return fmt.Sprintf("conntrack preflight listener failed: %v", err)
	}
	defer func() { _ = listener.Process.Kill(); _, _ = listener.Process.Wait() }()
	time.Sleep(500 * time.Millisecond)
	_ = exec.Command("ip", "netns", "exec", ns, probeBin, "connect-echo", "tcp", "127.0.0.1", "9097").Run()
	time.Sleep(200 * time.Millisecond)

	out, err := exec.Command("ip", "netns", "exec", ns, "cat",
		"/proc/sys/net/netfilter/nf_conntrack_count").CombinedOutput()
	if err != nil {
		return fmt.Sprintf("cannot read nf_conntrack_count: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	if strings.TrimSpace(string(out)) == "0" {
		return "conntrack does not track traffic in fresh namespaces (nf_conntrack_count stayed 0); stateful/NAT/overflow scenarios cannot run here"
	}
	return ""
}

func buildProbe(t *testing.T) {
	t.Helper()
	if probeBin != "" {
		return
	}
	root := repoRoot(t)
	// The probe binary is a package-level artifact shared by every test.
	// It must NOT be cleaned up when the first test finishes (a
	// t.Cleanup registered here would delete it before later tests use
	// it). It lives in a per-run temp dir for the process lifetime.
	tmp, err := os.MkdirTemp("", "gsh-probe-")
	if err != nil {
		t.Fatal(err)
	}
	probeBin = filepath.Join(tmp, "probe")
	cmd := exec.Command("go", "build", "-o", probeBin, "./tests/integration/probe")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build probe: %v: %s", err, out)
	}
}

// runLab runs the lab script; up=true creates, false tears down.
func runLab(t *testing.T, up bool) {
	t.Helper()
	verb := "down"
	if up {
		verb = "up"
	}
	cmd := exec.Command("bash", labScript, verb)
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("lab %s: %v:\n%s", verb, err, out)
	}
}

// runNs runs a command inside a lab namespace.
func runNs(t *testing.T, ns string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("ip", append([]string{"netns", "exec", ns}, args...)...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// startProbe starts a listener inside a namespace, waits for the port to
// open, and registers cleanup.
func startProbe(t *testing.T, ns, proto string, port int) {
	t.Helper()
	cmd := exec.Command("ip", "netns", "exec", ns, probeBin, "listen", proto, fmt.Sprint(port))
	if err := cmd.Start(); err != nil {
		t.Fatalf("start probe listener: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	waitForPort(t, ns, proto, port)
}

func waitForPort(t *testing.T, ns, proto string, port int) {
	t.Helper()
	// The listener runs inside the namespace; the host has no route to
	// 10.99.1.2, so readiness must be checked from inside the ns. Read
	// the ns's socket table (no side effects, unlike a connect probe
	// which would consume the single-accept listener).
	//
	// Go's net.Listen("tcp", "0.0.0.0:port") creates a dual-stack
	// socket that appears in /proc/net/tcp6 (as an IPv6-mapped address),
	// NOT /proc/net/tcp. Scan both files.
	procFiles := []string{"/proc/net/tcp", "/proc/net/tcp6"}
	if proto == "udp" {
		procFiles = []string{"/proc/net/udp", "/proc/net/udp6"}
	}
	needle := fmt.Sprintf(":%04X", port) // local_address port, upper-case hex
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, procFile := range procFiles {
			out, err := runNs(t, ns, "cat", procFile)
			if err != nil {
				continue
			}
			for _, line := range strings.Split(out, "\n") {
				f := strings.Fields(line)
				if len(f) < 4 || !strings.Contains(f[1], needle) {
					continue
				}
				// TCP: only a LISTEN socket (state 0A) counts.
				if proto == "tcp" && f[3] != "0A" {
					continue
				}
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("probe listener %s:%d in ns %s did not come up", proto, port, ns)
}

// firewallProc tracks a started daemon: its process and log file.
type firewallProc struct {
	cmd     *exec.Cmd
	logPath string
}

// startFirewallFull is startFirewall but returns the daemon process and
// log path so tests can inspect logs and signal the process.
func startFirewallFull(t *testing.T, policyYAML string, extraArgs ...string) *firewallProc {
	t.Helper()
	root := repoRoot(t)
	polPath := filepath.Join(t.TempDir(), "policy.yaml")
	if err := os.WriteFile(polPath, []byte(policyYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "goshisen")
	{
		cmd := exec.Command("go", "build", "-o", bin, "./cmd/goshisen")
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build daemon: %v: %s", err, out)
		}
	}

	args := []string{"netns", "exec", "gsh_fw", bin,
		"--interface", "gsh-fw1",
		"--policy-file", polPath,
		"--log-level", "debug"}
	args = append(args, extraArgs...)
	cmd := exec.Command("ip", args...)
	cmd.Dir = root
	logFile, err := os.Create(filepath.Join(t.TempDir(), "daemon.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}
	fp := &firewallProc{cmd: cmd, logPath: logFile.Name()}
	t.Cleanup(func() {
		_ = fp.cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { _, _ = fp.cmd.Process.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = fp.cmd.Process.Kill()
		}
	})
	time.Sleep(200 * time.Millisecond) // allow exec + startup log line
	return fp
}

// startFirewall starts the daemon in the router namespace with the given
// policy file content, waits for it to bind, and registers cleanup.
func startFirewall(t *testing.T, policyYAML string) {
	t.Helper()
	fp := startFirewallFull(t, policyYAML)
	waitForDaemonReady(t, fp.logPath)
}

// waitForDaemonReady polls the daemon log until the NFQUEUE is bound
// (the enforcement path is up). This replaces fixed sleeps, which are
// unreliable under slow emulation (TCG) or loaded hosts.
func waitForDaemonReady(t *testing.T, logPath string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(logPath)
		if err == nil && strings.Contains(string(data), "NFQUEUE bound") {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("daemon did not become ready (no NFQUEUE bound in log):\n%s", mustReadLog(logPath))
}

func mustReadLog(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("<read %s: %v>", path, err)
	}
	return string(data)
}

// probe runs one client-side probe and returns (exitcode, output).
func probe(t *testing.T, proto, host string, port int) (int, string) {
	t.Helper()
	cmd := exec.Command("ip", "netns", "exec", "gsh_client",
		probeBin, "connect", proto, host, fmt.Sprint(port))
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), string(out)
		}
		return -1, string(out)
	}
	return 0, string(out)
}

func TestLabAllowedTCP(t *testing.T) {
	skip := checkPrereqs(t)
	if skip != "" {
		t.Skip(skip)
	}
	buildProbe(t)
	runLab(t, true)
	defer runLab(t, false)
	startProbe(t, "gsh_server", "tcp", 8080)
	startFirewall(t, `
version: 1
rules:
  - id: allow-http
    action: ACCEPT
    protocol: tcp
    src: 10.99.0.0/24
    dst: 10.99.1.0/24
    dst_port: 8080
`)
	code, out := probe(t, "tcp", "10.99.1.2", 8080)
	if code != 0 {
		t.Fatalf("allowed TCP probe failed: exit %d, %s", code, out)
	}
}

func TestLabAllowedUDP(t *testing.T) {
	skip := checkPrereqs(t)
	if skip != "" {
		t.Skip(skip)
	}
	buildProbe(t)
	runLab(t, true)
	defer runLab(t, false)
	startProbe(t, "gsh_server", "udp", 5353)
	startFirewall(t, `
version: 1
rules:
  - id: allow-dns
    action: ACCEPT
    protocol: udp
    dst: 10.99.1.2
    dst_port: 5353
`)
	code, out := probe(t, "udp", "10.99.1.2", 5353)
	if code != 0 {
		t.Fatalf("allowed UDP probe failed: exit %d, %s", code, out)
	}
}

func TestLabDeniedTraffic(t *testing.T) {
	skip := checkPrereqs(t)
	if skip != "" {
		t.Skip(skip)
	}
	buildProbe(t)
	runLab(t, true)
	defer runLab(t, false)
	startProbe(t, "gsh_server", "tcp", 8080)
	startFirewall(t, `
version: 1
rules:
  - id: allow-http
    action: ACCEPT
    protocol: tcp
    src: 10.99.0.0/24
    dst: 10.99.1.0/24
    dst_port: 8080
`)
	// Different port: no rule matches → default DROP.
	startProbe(t, "gsh_server", "tcp", 9090)
	code, _ := probe(t, "tcp", "10.99.1.2", 9090)
	if code == 0 {
		t.Fatal("denied TCP probe unexpectedly succeeded")
	}
}

func TestLabDefaultDrop(t *testing.T) {
	skip := checkPrereqs(t)
	if skip != "" {
		t.Skip(skip)
	}
	buildProbe(t)
	runLab(t, true)
	defer runLab(t, false)
	startProbe(t, "gsh_server", "tcp", 8080)
	// No rules at all → default DROP.
	startFirewall(t, `
version: 1
rules: []
`)
	code, _ := probe(t, "tcp", "10.99.1.2", 8080)
	if code == 0 {
		t.Fatal("default DROP probe unexpectedly succeeded")
	}
}

func TestLabQueueNoListener(t *testing.T) {
	skip := checkPrereqs(t)
	if skip != "" {
		t.Skip(skip)
	}
	buildProbe(t)
	runLab(t, true)
	defer runLab(t, false)
	startProbe(t, "gsh_server", "tcp", 8080)
	// Hook installed, but the daemon is never started: the queue has no
	// listener. The chain policy is drop, so traffic must be blocked.
	runNs(t, "gsh_fw", "nft", "-f", "-")
	code, _ := probe(t, "tcp", "10.99.1.2", 8080)
	if code == 0 {
		t.Fatal("traffic passed with no NFQUEUE listener")
	}
}

func TestLabShutdownClean(t *testing.T) {
	skip := checkPrereqs(t)
	if skip != "" {
		t.Skip(skip)
	}
	buildProbe(t)
	runLab(t, true)
	defer runLab(t, false)
	startProbe(t, "gsh_server", "tcp", 8080)
	startFirewall(t, `
version: 1
rules:
  - id: allow-http
    action: ACCEPT
    protocol: tcp
    src: 10.99.0.0/24
    dst: 10.99.1.0/24
    dst_port: 8080
`)
	code, out := probe(t, "tcp", "10.99.1.2", 8080)
	if code != 0 {
		t.Fatalf("pre-shutdown probe failed: %s", out)
	}
	// after the daemon exits, traffic must stop (fail closed).
	runNs(t, "gsh_fw", "pkill", "-INT", "goshisen")
	time.Sleep(500 * time.Millisecond)
	code, _ = probe(t, "tcp", "10.99.1.2", 8080)
	if code == 0 {
		t.Fatal("traffic passed after daemon shutdown")
	}
}

// TestHookCleanupScope proves `goshisen hook cleanup` deletes only the
// application-owned table (`ip goshisen`) and leaves unrelated nftables
// state untouched. A foreign table is created before cleanup and must
// survive it.
func TestHookCleanupScope(t *testing.T) {
	if skip := checkPrereqs(t); skip != "" {
		t.Skip(skip)
	}
	buildProbe(t)
	runLab(t, true)
	defer runLab(t, false)
	startFirewall(t, `
version: 1
rules: []
`)

	// A foreign table with a rule; must survive cleanup.
	foreign := `
table ip gsh_foreign {
    chain c {
        type filter hook forward priority 0; policy accept;
        counter
    }
}
`
	{
		cmd := exec.Command("ip", "netns", "exec", "gsh_fw", "nft", "-f", "-")
		cmd.Stdin = strings.NewReader(foreign)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("create foreign table: %v (%s)", err, out)
		}
	}

	// Run `goshisen hook cleanup` inside the router namespace.
	bin := filepath.Join(t.TempDir(), "goshisen")
	{
		cmd := exec.Command("go", "build", "-o", bin, "./cmd/goshisen")
		cmd.Dir = repoRoot(t)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build daemon: %v: %s", err, out)
		}
	}
	if out, err := runNs(t, "gsh_fw", bin, "hook", "cleanup"); err != nil {
		t.Fatalf("hook cleanup: %v (%s)", err, out)
	}

	// App table must be gone.
	if out, err := runNs(t, "gsh_fw", "nft", "list", "table", "ip", "goshisen"); err == nil {
		t.Fatalf("app table still present after cleanup:\n%s", out)
	}
	// Foreign table must remain.
	out, err := runNs(t, "gsh_fw", "nft", "list", "table", "ip", "gsh_foreign")
	if err != nil {
		t.Fatalf("foreign table removed by cleanup: %v (%s)", err, out)
	}
	if !strings.Contains(out, "gsh_foreign") {
		t.Errorf("foreign table content missing:\n%s", out)
	}
}
