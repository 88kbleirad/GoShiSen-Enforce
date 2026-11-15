// IPv6 stateful e2e (issue #5): runs scripts/openwrt-e2e-v6.sh against
// the dual-tap v6 lab (scripts/openwrt-lab-v6.sh up). The router
// forwards IPv6, so traffic hits the inet goshisen hook queue 101; every
// scenario carries its paired IPv4 control and counter evidence.
//
// The test requires:
//   - the v6 lab to be up (scripts/openwrt-lab-v6.sh up), with the
//     daemon running inside the VM and queues 100+101 bound,
//   - root for the client-namespace curl,
//   - python3 + curl + wget on the host.
//
// When the lab or tools are unavailable the test skips with an explicit
// reason; the CI openwrt-integration-v6 job provisions the lab from a
// pinned release image and rejects skips.
package openwrt_integration

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func v6LabUp() string {
	if os.Geteuid() != 0 {
		return "openwrt v6 e2e requires root for the client-namespace curl"
	}
	// Both v6 lab namespaces present.
	out, err := exec.Command("ip", "netns", "list").CombinedOutput()
	if err != nil {
		return fmt.Sprintf("ip netns list failed: %v", err)
	}
	for _, ns := range []string{"gsh_v6_lan", "gsh_v6_wan"} {
		if !strings.Contains(string(out), ns) {
			return fmt.Sprintf("openwrt v6 lab namespace %s is not up (run: scripts/openwrt-lab-v6.sh up)", ns)
		}
	}
	// Serial console reachable.
	port := os.Getenv("OPENWRT_V6_CONSOLE_PORT")
	if port == "" {
		port = "5555"
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", port), 3*time.Second)
	if err != nil {
		return fmt.Sprintf("openwrt v6 lab serial console 127.0.0.1:%s not reachable (lab up?)", port)
	}
	_ = conn.Close()
	return ""
}

func TestOpenWrtE2EV6(t *testing.T) {
	if reason := checkTools(); reason != "" {
		t.Skip(reason)
	}
	if reason := v6LabUp(); reason != "" {
		t.Skip(reason)
	}

	root := repoRoot(t)
	script := filepath.Join(root, "scripts", "openwrt-e2e-v6.sh")
	cmd := exec.Command("bash", script)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PATH="+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("openwrt v6 e2e failed: %v\n%s", err, out)
	}
	t.Logf("openwrt v6 e2e output:\n%s", out)
}
