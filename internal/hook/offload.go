// Package hook: flow-offload detection and disable.
//
// Software or hardware flow offloading makes established flows skip
// nftables entirely, so NFQUEUE would never see them. The daemon must
// know when that is configured so it can warn prominently or, in
// enforce mode, actively close the gap. This package is read-only for
// detection and performs ONLY flow-offload deletes for disable; no
// other nftables state is ever touched.
package hook

import (
	"fmt"
	"os/exec"
	"strings"
)

// FlowOffloadState is the observable state of flow offloading.
type FlowOffloadState int

const (
	// FlowOffloadDisabled: no flowtable objects and no flow-offload
	// rules exist — enforcement gap absent.
	FlowOffloadDisabled FlowOffloadState = iota
	// FlowOffloadEnabled: at least one flowtable object or flow-offload
	// rule exists — established flows can bypass NFQUEUE.
	FlowOffloadEnabled
	// FlowOffloadUnknown: detection could not run (nft missing, list
	// error). Callers treat this as not provably safe.
	FlowOffloadUnknown
	// FlowOffloadFailed: disable was attempted but re-detection still
	// reports enabled.
	FlowOffloadFailed
)

func (s FlowOffloadState) String() string {
	switch s {
	case FlowOffloadDisabled:
		return "disabled"
	case FlowOffloadEnabled:
		return "enabled"
	case FlowOffloadFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// FlowOffloadReport summarizes whether flow offloading is active in the
// current nftables ruleset.
type FlowOffloadReport struct {
	// State is disabled, enabled, unknown, or failed.
	State FlowOffloadState
	// Active is true when State is enabled or failed (a gap is
	// present or was not closed).
	Active bool
	// Detail is a human-readable summary of what was found.
	Detail string
}

// flowRule is a parsed flow-offload rule identified by handle.
type flowRule struct {
	family    string // "inet", "ip", "ip6"
	table     string
	chain     string
	handle    string
	statement string
}

// flowTable is a parsed flowtable object.
type flowTable struct {
	family string
	table  string
	name   string
}

// parseRuleset extracts flow-offload rules and flowtables from `nft -a
// list ruleset` output. Rules that contain a `flow offload` or
// `flow add @` statement are collected with their handles; flowtable
// object definitions are collected separately.
func parseRuleset(text string) (rules []flowRule, tables []flowTable) {
	curFamily := ""
	curTable := ""
	curChain := ""
	lines := strings.Split(text, "\n")
	for i := range lines {
		line := strings.TrimSpace(lines[i])
		switch {
		case strings.HasPrefix(line, "table ") && strings.Contains(line, "{"):
			// table inet fw4 {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				curFamily = fields[1]
				curTable = fields[2]
			}
			curChain = ""
		case strings.HasPrefix(line, "chain ") && strings.Contains(line, "{"):
			// chain forward {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				curChain = strings.Trim(fields[1], "{")
			}
		case strings.HasPrefix(line, "flowtable ") && strings.Contains(line, "{"):
			// flowtable ft {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				name := strings.Trim(fields[1], "{")
				tables = append(tables, flowTable{family: curFamily, table: curTable, name: name})
			}
		case strings.Contains(line, "flow offload") || strings.Contains(line, "flow add @"):
			rules = append(rules, flowRule{
				family:    curFamily,
				table:     curTable,
				chain:     curChain,
				handle:    ruleHandle(line),
				statement: line,
			})
		}
	}
	return rules, tables
}

// ruleHandle extracts the `# handle N` suffix from an nft -a rule line.
func ruleHandle(line string) string {
	idx := strings.LastIndex(line, "# handle ")
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(line[idx+len("# handle "):])
}

// DetectFlowOffload inspects the running nftables ruleset for flow
// offloading.
//
// It returns:
//   - FlowOffloadDisabled when the ruleset is clean,
//   - FlowOffloadEnabled when a flowtable or flow-offload rule exists,
//   - FlowOffloadUnknown when nft is unavailable or listing fails
//     (the daemon must not assume safe).
func DetectFlowOffload() FlowOffloadReport {
	// Primary probe: full annotated ruleset. It contains flow-offload
	// rules (with handles) and flowtable objects.
	out, err := exec.Command("nft", "-a", "list", "ruleset").CombinedOutput()
	if err == nil {
		rules, tables := parseRuleset(string(out))
		if len(rules) > 0 || len(tables) > 0 {
			return reportFor(rules, tables)
		}
		return FlowOffloadReport{State: FlowOffloadDisabled}
	}
	// Primary probe failed — fall back to the flowtables-only listing so
	// a minimal environment that cannot dump the full ruleset still
	// reports flowtable objects.
	flowOut, flowErr := exec.Command("nft", "list", "flowtables").CombinedOutput()
	if flowErr == nil && strings.Contains(string(flowOut), "flowtable") {
		return FlowOffloadReport{
			State:  FlowOffloadEnabled,
			Active: true,
			Detail: "nftables flowtable(s) present: " + strings.TrimSpace(string(flowOut)),
		}
	}
	return FlowOffloadReport{
		State:  FlowOffloadUnknown,
		Active: false,
		Detail: fmt.Sprintf("nft -a list ruleset failed: %v", err),
	}
}

func reportFor(rules []flowRule, tables []flowTable) FlowOffloadReport {
	var parts []string
	for _, r := range rules {
		parts = append(parts, fmt.Sprintf("flow-offload rule %s/%s/%s: %s", r.family, r.table, r.chain, r.statement))
	}
	for _, t := range tables {
		parts = append(parts, fmt.Sprintf("flowtable %s/%s/%s", t.family, t.table, t.name))
	}
	return FlowOffloadReport{
		State:  FlowOffloadEnabled,
		Active: true,
		Detail: strings.Join(parts, "; "),
	}
}

// disableProgram builds a single nft transaction that deletes every
// flow-offload rule and flowtable found. It touches nothing else.
func disableProgram(rules []flowRule, tables []flowTable) string {
	var b strings.Builder
	// Deleting flowtables requires deleting referencing rules first;
	// nft applies the program sequentially in one transaction.
	for _, r := range rules {
		if r.handle != "" {
			fmt.Fprintf(&b, "delete rule %s %s %s handle %s\n", r.family, r.table, r.chain, r.handle)
		}
	}
	for _, t := range tables {
		fmt.Fprintf(&b, "delete flowtable %s %s %s\n", t.family, t.table, t.name)
	}
	return b.String()
}

// DisableFlowOffload attempts to remove all nftables flow-offload
// state. It returns the report AFTER re-detection:
//   - FlowOffloadDisabled when the delete succeeded,
//   - FlowOffloadFailed when something remains (or nft errored).
//
// The operation is idempotent: with nothing to disable it is a no-op
// (disabled). It only deletes flowtables and flow-offload rules; no
// other table, chain, or rule is touched.
func DisableFlowOffload() FlowOffloadReport {
	out, err := exec.Command("nft", "-a", "list", "ruleset").CombinedOutput()
	if err != nil {
		return FlowOffloadReport{
			State:  FlowOffloadUnknown,
			Active: false,
			Detail: fmt.Sprintf("nft -a list ruleset failed: %v", err),
		}
	}
	rules, tables := parseRuleset(string(out))
	if len(rules) == 0 && len(tables) == 0 {
		return FlowOffloadReport{State: FlowOffloadDisabled}
	}
	prog := disableProgram(rules, tables)
	cmd := exec.Command("nft", "-f", "-")
	cmd.Stdin = strings.NewReader(prog)
	if out, err := cmd.CombinedOutput(); err != nil {
		return FlowOffloadReport{
			State:  FlowOffloadFailed,
			Active: true,
			Detail: fmt.Sprintf("nft disable transaction failed: %v: %s", err, strings.TrimSpace(string(out))),
		}
	}
	// Re-detect to verify the disable took effect.
	rep := DetectFlowOffload()
	if rep.State == FlowOffloadEnabled {
		return FlowOffloadReport{
			State:  FlowOffloadFailed,
			Active: true,
			Detail: "disable applied but offload still present: " + rep.Detail,
		}
	}
	return FlowOffloadReport{State: FlowOffloadDisabled, Detail: "flow offload disabled"}
}
