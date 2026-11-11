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

func TestDetectFlowOffloadRule(t *testing.T) {
	ruleset := "table inet fw4 {\n\tchain forward {\n\t\tflow offload @ft\n\t}\n}\n"
	dir := fakeNft(t, ruleset)
	old := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+old)

	rep := DetectFlowOffload()
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
	old := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+old)

	rep := DetectFlowOffload()
	if !rep.Active {
		t.Fatal("DetectFlowOffload = inactive, want active for flowtable")
	}
}

func TestDetectFlowOffloadNone(t *testing.T) {
	ruleset := "table inet fw4 {\n\tchain forward {\n\t\tcounter\n\t}\n}\n"
	dir := fakeNft(t, ruleset)
	old := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+old)

	rep := DetectFlowOffload()
	if rep.Active {
		t.Fatalf("DetectFlowOffload = active, want inactive; detail=%q", rep.Detail)
	}
}
