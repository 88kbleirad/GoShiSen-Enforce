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
	"strconv"
	"strings"
	"time"
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
	// HitsAvailable is true when at least one flowtable exposes a hit
	// counter (measurement supported).
	HitsAvailable bool
	// HitsPackets and HitsBytes are the cumulative flowtable hit
	// counters across all flowtables (0 when not available).
	HitsPackets uint64
	HitsBytes   uint64
}

// flowRule is a parsed flow-offload rule identified by handle.
type flowRule struct {
	family    string // "inet", "ip", "ip6"
	table     string
	chain     string
	handle    string
	statement string
}

// flowTable is a parsed flowtable object with its hit counters (when
// the flowtable carries a `counter` statement).
type flowTable struct {
	family string
	table  string
	name   string
	// CounterAvailable is true when the flowtable has a counter
	// statement (hit observability enabled).
	CounterAvailable bool
	// HitsPackets and HitsBytes are the flowtable's cumulative hit
	// counters (monotonic; 0 when not available).
	HitsPackets uint64
	HitsBytes   uint64
}

// parseRuleset extracts flow-offload rules and flowtables from `nft -a
// list ruleset` output. Rules that contain a `flow offload` or
// `flow add @` statement are collected with their handles; flowtable
// object definitions are collected separately.
func parseRuleset(text string) (rules []flowRule, tables []flowTable) {
	curFamily := ""
	curTable := ""
	curChain := ""
	// inFlowTable tracks whether we are inside a flowtable object block
	// so a `counter` line inside it attaches to that flowtable.
	inFlowTable := false
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
			inFlowTable = false
		case strings.HasPrefix(line, "chain ") && strings.Contains(line, "{"):
			// chain forward {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				curChain = strings.Trim(fields[1], "{")
			}
			inFlowTable = false
		case strings.HasPrefix(line, "flowtable ") && strings.Contains(line, "{"):
			// flowtable ft {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				name := strings.Trim(fields[1], "{")
				tables = append(tables, flowTable{family: curFamily, table: curTable, name: name})
				inFlowTable = true
			}
		case strings.HasPrefix(line, "}"):
			inFlowTable = false
		case strings.Contains(line, "flow offload") || strings.Contains(line, "flow add @"):
			rules = append(rules, flowRule{
				family:    curFamily,
				table:     curTable,
				chain:     curChain,
				handle:    ruleHandle(line),
				statement: line,
			})
		case inFlowTable && strings.HasPrefix(line, "counter"):
			// Counter line inside a flowtable: `counter` (no values)
			// or `counter packets N bytes M`.
			if n := len(tables); n > 0 {
				t := &tables[n-1]
				t.CounterAvailable = true
				if p, b, ok := parseCounter(line); ok {
					t.HitsPackets = p
					t.HitsBytes = b
				}
			}
		}
	}
	return rules, tables
}

// parseCounter parses a `counter packets N bytes M` line. It returns the
// packet/byte counts, or ok=false when the line has no values (a bare
// `counter` statement with counts zeroed by the kernel until first use).
func parseCounter(line string) (packets, bytes uint64, ok bool) {
	fields := strings.Fields(line)
	// Expected: counter [packets N] [bytes M]
	for i := 0; i < len(fields)-1; i++ {
		switch fields[i] {
		case "packets":
			if v, err := strconv.ParseUint(fields[i+1], 10, 64); err == nil {
				packets = v
				ok = true
				i++
			}
		case "bytes":
			if v, err := strconv.ParseUint(fields[i+1], 10, 64); err == nil {
				bytes = v
				i++
			}
		}
	}
	return packets, bytes, ok
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
	var hitsPackets, hitsBytes uint64
	hitsAvailable := false
	for _, r := range rules {
		parts = append(parts, fmt.Sprintf("flow-offload rule %s/%s/%s: %s", r.family, r.table, r.chain, r.statement))
	}
	for _, t := range tables {
		parts = append(parts, fmt.Sprintf("flowtable %s/%s/%s", t.family, t.table, t.name))
		if t.CounterAvailable {
			hitsAvailable = true
			hitsPackets += t.HitsPackets
			hitsBytes += t.HitsBytes
		}
	}
	return FlowOffloadReport{
		State:         FlowOffloadEnabled,
		Active:        true,
		Detail:        strings.Join(parts, "; "),
		HitsAvailable: hitsAvailable,
		HitsPackets:   hitsPackets,
		HitsBytes:     hitsBytes,
	}
}

// FlowOffloadSample is one read-only measurement of flowtable hit
// counters and the current enforcement state.
type FlowOffloadSample struct {
	// At is the UTC sampling time.
	At string
	// State is the enforcement state (disabled/enabled/unknown/failed).
	State FlowOffloadState
	// Flowtables lists each flowtable with its individual hit counter
	// (best-effort; empty when none found).
	Flowtables []FlowTableHit
	// HitsAvailable is true when counters could be read.
	HitsAvailable bool
	// HitsPackets/HitsBytes are the aggregated cumulative counters.
	HitsPackets uint64
	HitsBytes   uint64
	// Confidence: "high" when counters present, "limited" when
	// flowtables exist but counters are unavailable, "unknown" when
	// detection failed.
	Confidence string
	// Reason explains limited/unknown confidence.
	Reason string
}

// FlowTableHit is one flowtable's observed counters.
type FlowTableHit struct {
	Family string
	Table  string
	Name   string
	// Packets/Bytes are cumulative (monotonic) hit counters.
	Packets uint64
	Bytes   uint64
}

// ObserveFlowOffload takes a read-only measurement snapshot of flowtable
// hit counters and the enforcement state. It never modifies the
// ruleset, routes, interfaces, or connection state.
func ObserveFlowOffload() FlowOffloadSample {
	rep := DetectFlowOffload()
	s := FlowOffloadSample{
		At:    time.Now().UTC().Format(time.RFC3339Nano),
		State: rep.State,
	}
	switch rep.State {
	case FlowOffloadDisabled:
		s.Confidence = "high"
		s.Reason = "no flowtable / flow-offload rules"
		return s
	case FlowOffloadUnknown:
		s.Confidence = "unknown"
		s.Reason = rep.Detail
		return s
	case FlowOffloadFailed:
		s.Confidence = "limited"
		s.Reason = rep.Detail
	}

	// Now read flowtables with counters for the hit observability.
	out, err := exec.Command("nft", "-a", "list", "ruleset").CombinedOutput()
	if err != nil {
		s.Confidence = "unknown"
		s.Reason = fmt.Sprintf("nft -a list ruleset failed: %v", err)
		return s
	}
	_, tables := parseRuleset(string(out))
	if len(tables) == 0 {
		s.Confidence = "limited"
		s.Reason = "flowtable present per detection but not found in ruleset parse"
		return s
	}
	for _, t := range tables {
		hit := FlowTableHit{Family: t.family, Table: t.table, Name: t.name, Packets: t.HitsPackets, Bytes: t.HitsBytes}
		s.Flowtables = append(s.Flowtables, hit)
		if t.CounterAvailable {
			s.HitsAvailable = true
			s.HitsPackets += t.HitsPackets
			s.HitsBytes += t.HitsBytes
		}
	}
	if s.HitsAvailable {
		s.Confidence = "high"
	} else {
		s.Confidence = "limited"
		s.Reason = "flowtable(s) present but no counter statement (hit measurement unavailable)"
	}
	return s
}

// DeltaHits computes the positive delta between two cumulative hit
// samples, handling counter resets and wraparound. A reset (new < old)
// yields the new value (counters re-based); a wraparound (new < old and
// new small) is treated as a reset. Negative deltas are impossible for
// monotonic counters and yield 0.
func DeltaHits(after, before FlowOffloadSample) (packets, bytes uint64) {
	p, b := after.HitsPackets, after.HitsBytes
	if p >= before.HitsPackets {
		packets = p - before.HitsPackets
	} else {
		packets = p // counter reset/wrap: report current value
	}
	if b >= before.HitsBytes {
		bytes = b - before.HitsBytes
	} else {
		bytes = b
	}
	return packets, bytes
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
