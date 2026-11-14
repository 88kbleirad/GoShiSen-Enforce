package hook

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeNft builds a tiny shell script standing in for `nft`. The ruleset
// is written to a file and the script cats it for `nft -a list ruleset`;
// `nft list flowtables` prints nothing (no flowtables). Returns the
// directory to prepend to PATH.
func fakeNft(t *testing.T, ruleset string) string {
	t.Helper()
	dir := t.TempDir()
	rsFile := filepath.Join(dir, "ruleset.txt")
	if err := os.WriteFile(rsFile, []byte(ruleset), 0o644); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "nft")
	content := "#!/bin/sh\n" +
		"case \"$*\" in\n" +
		"  \"list flowtables\") ;; # empty\n" +
		"  \"-a list ruleset\") cat " + rsFile + " ;;\n" +
		"  *) exit 1 ;;\n" +
		"esac\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// fakeNftWithDisable additionally emulates the `-f -` disable program:
// it deletes the flowtable/rule by removing the matching lines from a
// copy of the ruleset the next list returns.
func fakeNftWithDisable(t *testing.T, ruleset string) string {
	t.Helper()
	dir := t.TempDir()
	rsFile := filepath.Join(dir, "ruleset.txt")
	if err := os.WriteFile(rsFile, []byte(ruleset), 0o644); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "nft")
	// The fake needs a stateful "delete" step: on `-f -` we receive the
	// program on stdin; simplest is to re-write the ruleset file to an
	// empty one (verifying the program was non-empty is done in the
	// test via the state transition).
	content := "#!/bin/sh\n" +
		"case \"$*\" in\n" +
		"  \"list flowtables\") ;; # empty\n" +
		"  \"-a list ruleset\") cat " + rsFile + " ;;\n" +
		"  \"-f -\") cat > /dev/null; : > " + rsFile + " ;;\n" +
		"  *) exit 1 ;;\n" +
		"esac\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func withPath(t *testing.T, dir string) {
	t.Helper()
	old := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+old)
}

func TestDetectFlowOffloadRule(t *testing.T) {
	ruleset := "table inet fw4 {\n\tchain forward {\n\t\tflow offload @ft\n\t}\n}\n"
	dir := fakeNft(t, ruleset)
	withPath(t, dir)

	rep := DetectFlowOffload()
	if rep.State != FlowOffloadEnabled {
		t.Fatalf("DetectFlowOffload.State = %s, want enabled", rep.State)
	}
	if !rep.Active {
		t.Fatal("DetectFlowOffload = inactive, want active for 'flow offload' rule")
	}
	if rep.Detail == "" {
		t.Fatal("detail must be non-empty")
	}
}

func TestDetectFlowOffloadFlowtable(t *testing.T) {
	// A flowtable exists but no rule references it.
	dir := t.TempDir()
	script := filepath.Join(dir, "nft")
	content := "#!/bin/sh\n" +
		"case \"$*\" in\n" +
		"  \"list flowtables\") echo 'table inet fw4 {'; echo '  flowtable ft {'; echo '    hook ingress priority filter'; echo '  }'; echo '}';;\n" +
		"  *) exit 1 ;;\n" +
		"esac\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	withPath(t, dir)

	rep := DetectFlowOffload()
	if rep.State != FlowOffloadEnabled {
		t.Fatalf("DetectFlowOffload.State = %s, want enabled", rep.State)
	}
	if !rep.Active {
		t.Fatal("DetectFlowOffload = inactive, want active for flowtable")
	}
}

func TestDetectFlowOffloadNone(t *testing.T) {
	ruleset := "table inet fw4 {\n\tchain forward {\n\t\tcounter\n\t}\n}\n"
	dir := fakeNft(t, ruleset)
	withPath(t, dir)

	rep := DetectFlowOffload()
	if rep.State != FlowOffloadDisabled {
		t.Fatalf("DetectFlowOffload.State = %s, want disabled", rep.State)
	}
	if rep.Active {
		t.Fatalf("DetectFlowOffload = active, want inactive; detail=%q", rep.Detail)
	}
}

func TestDetectFlowOffloadUnknownWhenNftFails(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "nft")
	content := "#!/bin/sh\n" +
		"echo nft-not-found >&2\n" +
		"exit 127\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	withPath(t, dir)

	rep := DetectFlowOffload()
	if rep.State != FlowOffloadUnknown {
		t.Fatalf("DetectFlowOffload.State = %s, want unknown", rep.State)
	}
}

func TestDisableFlowOffloadSuccess(t *testing.T) {
	ruleset := "table inet fw4 {\n\tchain forward {\n\t\tflow offload @ft\n\t}\n}\n"
	dir := fakeNftWithDisable(t, ruleset)
	withPath(t, dir)

	if rep := DetectFlowOffload(); rep.State != FlowOffloadEnabled {
		t.Fatalf("precondition: want enabled, got %s", rep.State)
	}
	rep := DisableFlowOffload()
	if rep.State != FlowOffloadDisabled {
		t.Fatalf("DisableFlowOffload.State = %s, want disabled; detail=%q", rep.State, rep.Detail)
	}
	if rep.Active {
		t.Fatal("DisableFlowOffload = active after successful disable")
	}
}

func TestDisableFlowOffloadIdempotentDisabled(t *testing.T) {
	ruleset := "table inet fw4 {\n\tchain forward {\n\t\tcounter\n\t}\n}\n"
	dir := fakeNftWithDisable(t, ruleset)
	withPath(t, dir)

	rep := DisableFlowOffload()
	if rep.State != FlowOffloadDisabled {
		t.Fatalf("DisableFlowOffload.State = %s, want disabled (no-op)", rep.State)
	}
}

func TestDisableFlowOffloadFailsWhenNftFails(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "nft")
	content := "#!/bin/sh\n" +
		"case \"$*\" in\n" +
		"  \"-a list ruleset\") cat <<'EOF'\ntable inet fw4 {\n chain forward {\n  flow offload @ft\n }\n}\nEOF\n;;\n" +
		"  \"-f -\") echo 'nft: transaction failed' >&2; exit 1 ;;\n" +
		"  *) exit 1 ;;\n" +
		"esac\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	withPath(t, dir)

	rep := DisableFlowOffload()
	if rep.State != FlowOffloadFailed {
		t.Fatalf("DisableFlowOffload.State = %s, want failed", rep.State)
	}
	if !rep.Active {
		t.Fatal("DisableFlowOffload must remain active after failed disable")
	}
}

func TestParseRulesetFlowTableCounters(t *testing.T) {
	ruleset := `table inet fw4 {
	flowtable ft {
		hook ingress priority filter
		devices = { "br-lan", "eth0" }
		counter packets 42 bytes 1337
	}
	chain forward {
		type filter hook forward priority filter; policy accept;
		meta l4proto { tcp, udp } flow add @ft
	}
}
`
	rules, tables := parseRuleset(ruleset)
	if len(rules) != 1 {
		t.Fatalf("rules = %d, want 1", len(rules))
	}
	if len(tables) != 1 {
		t.Fatalf("tables = %d, want 1", len(tables))
	}
	ft := tables[0]
	if !ft.CounterAvailable {
		t.Fatal("flowtable counter not marked available")
	}
	if ft.HitsPackets != 42 || ft.HitsBytes != 1337 {
		t.Fatalf("hits = %d/%d, want 42/1337", ft.HitsPackets, ft.HitsBytes)
	}
	if ft.name != "ft" || ft.table != "fw4" || ft.family != "inet" {
		t.Fatalf("flowtable identity = %s/%s/%s", ft.family, ft.table, ft.name)
	}
}

func TestParseRulesetFlowTableBareCounter(t *testing.T) {
	// A bare `counter` statement (no values) means the counter exists
	// but has not recorded hits yet — available, zero values.
	ruleset := `table inet fw4 {
	flowtable ft {
		counter
	}
}
`
	_, tables := parseRuleset(ruleset)
	if len(tables) != 1 {
		t.Fatalf("tables = %d, want 1", len(tables))
	}
	if !tables[0].CounterAvailable {
		t.Fatal("bare counter must still mark availability")
	}
	if tables[0].HitsPackets != 0 || tables[0].HitsBytes != 0 {
		t.Fatalf("bare counter hits = %d/%d, want 0/0", tables[0].HitsPackets, tables[0].HitsBytes)
	}
}

func TestParseRulesetNoCounters(t *testing.T) {
	ruleset := `table inet fw4 {
	flowtable ft {
		hook ingress priority filter
		devices = { "eth0" }
	}
}
`
	_, tables := parseRuleset(ruleset)
	if len(tables) != 1 {
		t.Fatalf("tables = %d, want 1", len(tables))
	}
	if tables[0].CounterAvailable {
		t.Fatal("flowtable without counter must not be marked available")
	}
}

func TestParseRulesetMalformedCounter(t *testing.T) {
	// Malformed counter lines must not panic and must not poison the
	// flowtable record.
	ruleset := `table inet fw4 {
	flowtable ft {
		counter packets notanumber bytes 12
	}
}
`
	_, tables := parseRuleset(ruleset)
	if len(tables) != 1 {
		t.Fatalf("tables = %d, want 1", len(tables))
	}
	if !tables[0].CounterAvailable {
		t.Fatal("malformed counter line still marks availability")
	}
	if tables[0].HitsPackets != 0 {
		t.Fatalf("malformed packets must stay 0, got %d", tables[0].HitsPackets)
	}
}

func TestCounterDelta(t *testing.T) {
	before := FlowOffloadSample{HitsPackets: 100, HitsBytes: 1000}
	after := FlowOffloadSample{HitsPackets: 150, HitsBytes: 1500}
	p, b := DeltaHits(after, before)
	if p != 50 || b != 500 {
		t.Fatalf("delta = %d/%d, want 50/500", p, b)
	}

	// Reset (new < old): report current value, not a huge negative.
	afterReset := FlowOffloadSample{HitsPackets: 5, HitsBytes: 50}
	p, b = DeltaHits(afterReset, before)
	if p != 5 || b != 50 {
		t.Fatalf("reset delta = %d/%d, want 5/50", p, b)
	}

	// No movement.
	p, b = DeltaHits(before, before)
	if p != 0 || b != 0 {
		t.Fatalf("zero delta = %d/%d, want 0/0", p, b)
	}
}

func TestObserveFlowOffloadDisabled(t *testing.T) {
	ruleset := "table inet fw4 {\n\tchain forward {\n\t\tcounter\n\t}\n}\n"
	dir := fakeNft(t, ruleset)
	withPath(t, dir)

	s := ObserveFlowOffload()
	if s.State != FlowOffloadDisabled {
		t.Fatalf("state = %s, want disabled", s.State)
	}
	if s.Confidence != "high" {
		t.Fatalf("confidence = %s, want high", s.Confidence)
	}
	if len(s.Flowtables) != 0 {
		t.Fatalf("flowtables = %d, want 0", len(s.Flowtables))
	}
}

func TestObserveFlowOffloadUnknownWhenNftFails(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "nft")
	content := "#!/bin/sh\n" +
		"echo nft-missing >&2\n" +
		"exit 127\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	withPath(t, dir)

	s := ObserveFlowOffload()
	if s.State != FlowOffloadUnknown {
		t.Fatalf("state = %s, want unknown", s.State)
	}
	if s.Confidence != "unknown" {
		t.Fatalf("confidence = %s, want unknown", s.Confidence)
	}
}

func TestObserveFlowOffloadPositiveHits(t *testing.T) {
	ruleset := `table inet fw4 {
	flowtable ft {
		counter packets 500 bytes 100000
	}
	chain forward {
		meta l4proto { tcp, udp } flow add @ft
	}
}
`
	dir := fakeNft(t, ruleset)
	withPath(t, dir)

	s := ObserveFlowOffload()
	if s.State != FlowOffloadEnabled {
		t.Fatalf("state = %s, want enabled", s.State)
	}
	if !s.HitsAvailable {
		t.Fatal("hits should be available")
	}
	if s.HitsPackets != 500 || s.HitsBytes != 100000 {
		t.Fatalf("hits = %d/%d, want 500/100000", s.HitsPackets, s.HitsBytes)
	}
	if s.Confidence != "high" {
		t.Fatalf("confidence = %s, want high", s.Confidence)
	}
	if len(s.Flowtables) != 1 {
		t.Fatalf("flowtables = %d, want 1", len(s.Flowtables))
	}
}

func TestObserveFlowOffloadLimitedWhenNoCounter(t *testing.T) {
	ruleset := `table inet fw4 {
	flowtable ft {
		hook ingress priority filter
		devices = { "eth0" }
	}
}
`
	dir := fakeNft(t, ruleset)
	withPath(t, dir)

	s := ObserveFlowOffload()
	if s.State != FlowOffloadEnabled {
		t.Fatalf("state = %s, want enabled", s.State)
	}
	if s.HitsAvailable {
		t.Fatal("hits must NOT be available without a counter statement")
	}
	if s.Confidence != "limited" {
		t.Fatalf("confidence = %s, want limited", s.Confidence)
	}
	if s.Reason == "" {
		t.Fatal("limited confidence must have a reason")
	}
}
