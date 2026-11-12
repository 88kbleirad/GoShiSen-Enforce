// Package hook: flow-offload detection.
//
// Software or hardware flow offloading makes established flows skip
// nftables entirely, so NFQUEUE would never see them. The daemon must
// know when that is configured so it can warn prominently (fail-closed
// by presence: we never disable the user's offload).
package hook

import (
	"os/exec"
	"strings"
)

// FlowOffloadReport summarizes whether flow offloading is active in the
// current nftables ruleset.
type FlowOffloadReport struct {
	// Active is true when at least one flowtable exists or a rule
	// contains a `flow offload` statement.
	Active bool
	// Detail is a human-readable summary of what was found.
	Detail string
}

// DetectFlowOffload inspects the running nftables ruleset for flow
// offloading. It is read-only and returns an empty report when nft is
// unavailable or the list fails (best-effort detection).
func DetectFlowOffload() FlowOffloadReport {
	// Flowtables are plain objects; `nft list flowtables` prints a
	// table header even when the table has no flowtable, so we look
	// for actual `flowtable` object lines.
	flowOut, err := exec.Command("nft", "list", "flowtables").CombinedOutput()
	if err == nil {
		for _, line := range strings.Split(string(flowOut), "\n") {
			if strings.Contains(line, "flowtable") {
				return FlowOffloadReport{Active: true, Detail: "nftables flowtable(s) present: " + strings.TrimSpace(line)}
			}
		}
	}

	// A flow-offload rule in any table. fw4 emits
	// `meta l4proto { tcp, udp } flow add @ft` in the forward chain
	// (older/other setups use `flow offload @ft`).
	out, err := exec.Command("nft", "-a", "list", "ruleset").CombinedOutput()
	if err != nil {
		return FlowOffloadReport{}
	}
	text := string(out)
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "flow add @") || strings.Contains(line, "flow offload") {
			return FlowOffloadReport{Active: true, Detail: "nftables ruleset contains a flow-offload rule: " + strings.TrimSpace(line)}
		}
	}
	return FlowOffloadReport{}
}
