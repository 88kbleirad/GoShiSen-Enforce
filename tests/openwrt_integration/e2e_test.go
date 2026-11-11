// Package openwrt_integration contains the OpenWrt QEMU lab
// end-to-end enforcement test.
//
// It runs scripts/openwrt-e2e.sh against the live lab (scripts/
// openwrt-lab.sh up). The script itself performs the two controlled
// HTTP transactions (explicit DROP must block, explicit ACCEPT must
// pass with the marker body) and asserts the kernel queue / nftables
// hook / daemon verdict metrics all advance in lockstep.
//
// The test requires:
//   - the lab to be up (scripts/openwrt-lab.sh up), with the daemon
//     running inside the VM and the NFQUEUE queue (100) bound,
//   - root for the client-namespace curl,
//   - python3 + curl + wget on the host.
//
// When the lab or tools are unavailable the test skips with an
// explicit reason; the CI openwrt-integration job provisions the lab
// from a pinned release image and rejects skips.
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

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func checkTools() string {
	// Host-side requirements only. wget/nft are used *inside* the VM
	// via the serial console and must not be required here.
	for _, tool := range []string{"bash", "python3", "curl", "ip"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Sprintf("required tool %q not in PATH", tool)
		}
	}
	return ""
}

func labUp() string {
	if os.Geteuid() != 0 {
		return "openwrt e2e requires root for the client-namespace curl"
	}
	// Client namespace present.
	out, err := exec.Command("ip", "netns", "list").CombinedOutput()
	if err != nil {
		return fmt.Sprintf("ip netns list failed: %v", err)
	}
	if !strings.Contains(string(out), "gsh_owrt_lan") {
		return "openwrt lab client namespace gsh_owrt_lan is not up (run: scripts/openwrt-lab.sh up)"
	}
	// Serial console reachable.
	port := os.Getenv("OPENWRT_CONSOLE_PORT")
	if port == "" {
		port = "5555"
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", port), 3*time.Second)
	if err != nil {
		return fmt.Sprintf("openwrt lab serial console 127.0.0.1:%s not reachable (lab up?)", port)
	}
	_ = conn.Close()
	return ""
}

func TestOpenWrtE2E(t *testing.T) {
	if reason := checkTools(); reason != "" {
		t.Skip(reason)
	}
	if reason := labUp(); reason != "" {
		t.Skip(reason)
	}

	root := repoRoot(t)
	script := filepath.Join(root, "scripts", "openwrt-e2e.sh")
	cmd := exec.Command("bash", script)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PATH="+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("openwrt e2e failed: %v\n%s", err, out)
	}
	t.Logf("openwrt e2e output:\n%s", out)
}
