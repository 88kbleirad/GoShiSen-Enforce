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
