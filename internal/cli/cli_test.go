package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseHelp(t *testing.T) {
	for _, a := range [][]string{{"-h"}, {"--help"}, {"--help", "-c", "x.yaml"}} {
		var buf bytes.Buffer
		o, err := Parse(a, &buf)
		if err != nil {
			t.Fatalf("Parse(%v): %v", a, err)
		}
		if !o.ShowHelp {
			t.Errorf("Parse(%v): ShowHelp = false", a)
		}
	}
}

func TestParseVersion(t *testing.T) {
	for _, a := range [][]string{{"-v"}, {"--version"}} {
		o, err := Parse(a, &bytes.Buffer{})
		if err != nil {
			t.Fatalf("Parse(%v): %v", a, err)
		}
		if !o.ShowVer {
			t.Errorf("Parse(%v): ShowVer = false", a)
		}
	}
}

func TestParseFlagsAndCommand(t *testing.T) {
	o, err := Parse([]string{"-c", "a.yaml", "--interface", "eth1", "config", "check"}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if o.ConfigFile != "a.yaml" {
		t.Errorf("ConfigFile = %q, want a.yaml", o.ConfigFile)
	}
	if o.Command != "config" || o.Subcommand != "check" {
		t.Errorf("Command/Subcommand = %q/%q, want config/check", o.Command, o.Subcommand)
	}
	if o.Overrides.Interface == nil || *o.Overrides.Interface != "eth1" {
		t.Errorf("Overrides.Interface = %v, want eth1", o.Overrides.Interface)
	}
}

func TestParseFlagsAfterCommandIgnored(t *testing.T) {
	o, err := Parse([]string{"config", "show", "-c", "a.yaml"}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if o.Command != "config" || o.Subcommand != "show" {
		t.Errorf("Command/Subcommand = %q/%q, want config/show", o.Command, o.Subcommand)
	}
	if o.ConfigFile != "" {
		t.Errorf("ConfigFile = %q, want empty (flag after command not parsed)", o.ConfigFile)
	}
}

func TestParseHookSubcommand(t *testing.T) {
	// Regression: `hook cleanup` must parse its subcommand; otherwise the
	// daemon silently runs `hook status` instead of deleting the table.
	for _, sub := range []string{"status", "cleanup"} {
		o, err := Parse([]string{"hook", sub}, &bytes.Buffer{})
		if err != nil {
			t.Fatalf("Parse hook %s: %v", sub, err)
		}
		if o.Command != "hook" || o.Subcommand != sub {
			t.Errorf("hook %s parsed as Command=%q Subcommand=%q", sub, o.Command, o.Subcommand)
		}
	}
}

func TestParseQueueMaxlenFlag(t *testing.T) {
	o, err := Parse([]string{"--queue-maxlen", "8"}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if o.Overrides.QueueMaxlen == nil || *o.Overrides.QueueMaxlen != 8 {
		t.Errorf("Overrides.QueueMaxlen = %v, want 8", o.Overrides.QueueMaxlen)
	}
}

func TestParseUnknownFlagErrors(t *testing.T) {
	var buf bytes.Buffer
	if _, err := Parse([]string{"--nope"}, &buf); err == nil {
		t.Fatal("Parse succeeded, want error for unknown flag")
	} else if !strings.Contains(err.Error(), "nope") {
		t.Errorf("error %q does not mention flag", err)
	}
}

func TestValidateCommand(t *testing.T) {
	cases := []struct {
		cmd, sub string
		wantErr  bool
	}{
		{"", "", false},
		{"config", "", false},
		{"config", "show", false},
		{"config", "check", false},
		{"config", "what", true},
		{"hook", "", false},
		{"hook", "status", false},
		{"hook", "cleanup", false},
		{"hook", "what", true},
		{"run", "", true},
	}
	for _, c := range cases {
		o := &Options{Command: c.cmd, Subcommand: c.sub}
		err := o.ValidateCommand()
		if (err != nil) != c.wantErr {
			t.Errorf("ValidateCommand(%q/%q) err = %v, wantErr %v", c.cmd, c.sub, err, c.wantErr)
		}
	}
}

func TestPrintVersion(t *testing.T) {
	var buf bytes.Buffer
	PrintVersion(&buf)
	if !strings.HasPrefix(buf.String(), Program+" ") {
		t.Errorf("version output %q does not start with program name", buf.String())
	}
	if strings.TrimSpace(buf.String()) != Program+" "+Version {
		t.Errorf("version output %q does not match %q", buf.String(), Program+" "+Version)
	}
}
