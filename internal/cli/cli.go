// Package cli implements the goshisen command-line interface: flag
// parsing, help/version output, and subcommand resolution. It is a
// library consumed by cmd/goshisen; it contains no side effects.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/88kbleirad/GoShiSen-Enforce/internal/config"
)

// Version is the daemon version; set at build time with
// -ldflags "-X github.com/88kbleirad/GoShiSen-Enforce/internal/cli.Version=…".
const Version = "dev"

// Program is the binary name shown in help and error messages.
const Program = "goshisen"

const usageText = `goshisen - Go firewall daemon (policy engine over NFQUEUE)

USAGE:
  goshisen [flags] [command]

COMMANDS:
  config show   Print the resolved configuration and the precedence source
                (default/file/env/flag) of each value
  config check  Validate configuration and host read-only: policy file
                readable, interface present and up. Makes no system changes
  policy validate   Validate a policy file (--policy-file) and print a
                summary of the compiled rules. Makes no system changes
  policy simulate   Simulate a packet against the policy and print the
                verdict and matching rule ID. Makes no system changes.
                Requires --src/--dst; options --protocol (tcp|udp|any),
                --source-port, --dest-port
  hook status   Print whether the app-owned nftables table (ip goshisen)
                is installed. Read-only, no changes
  hook cleanup  Delete the app-owned nftables table (ip goshisen) only.
                Never touches any other nftables state. Requires root
                (CAP_NET_ADMIN)

FLAGS:
  -h, --help               Show help and exit
  -v, --version            Show version and exit
  -c, --config <path>      YAML configuration file
  -i, --interface <name>   Interface to attach to
      --policy-file <path> Path to the YAML policy file
      --log-level <lvl>    debug, info, warn, error
      --log-json           Emit structured JSON logs
      --queue-maxlen <n>  Kernel NFQUEUE queue length in packets (default 1024)
      --protocol <proto>   tcp, udp, or any (policy simulate; default any)
      --src <ip>           Simulated source IP (policy simulate; required)
      --dst <ip>           Simulated destination IP (policy simulate; required)
      --source-port <n>    Simulated source port 1..65535 (policy simulate)
      --dest-port <n>      Simulated destination port 1..65535 (policy simulate)
      --state <st>         Simulated connection state: new, established, related,
                           invalid (policy simulate; default stateless)

PRECEDENCE (low to high):
  built-in defaults < config file < environment < flags

ENVIRONMENT (prefix GOSHISEN_):
  GOSHISEN_INTERFACE   same as --interface
  GOSHISEN_POLICY_FILE same as --policy-file
  GOSHISEN_LOG_LEVEL   same as --log-level
  GOSHISEN_LOG_JSON    "true"/"false" (or 1/0, yes/no, on/off)

EXAMPLES:
  goshisen --config /etc/goshisen/goshisen.yaml
  goshisen --interface eth1 --log-level debug
  GOSHISEN_LOG_JSON=true goshisen -c goshisen.yaml
  goshisen config check -c goshisen.yaml   # validate without running
  goshisen policy validate --policy-file policies/my-rules.yaml
  goshisen policy simulate --policy-file policies/my-rules.yaml \
    --protocol tcp --state new --src 10.0.0.1 --dst 10.0.0.2 --dest-port 22
`

// Options is the parsed CLI invocation.
type Options struct {
	ConfigFile string
	Command    string // "" | "config" | "policy"
	Subcommand string // "" | "show" | "check" | "validate" | "simulate"
	ShowHelp   bool
	ShowVer    bool
	Overrides  config.Overrides

	// Args are the words after the command (e.g. ["simulate", "--src", …]).
	Args []string

	// policy simulate flags.
	SimProto string
	SimSrc   string
	SimDst   string
	SimSport string
	SimDport string
	SimState string
}

// Parse parses argv (excluding the program name). Flag parsing stops at
// the first non-flag word, so flags must precede the command:
//
//	goshisen -c f.yaml config check   (valid)
//	goshisen config check -c f.yaml   (flags after command are not parsed)
func Parse(args []string, stderr io.Writer) (*Options, error) {
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		return &Options{ShowHelp: true}, nil
	}
	if len(args) == 1 && (args[0] == "-v" || args[0] == "--version") {
		return &Options{ShowVer: true}, nil
	}

	fs := flag.NewFlagSet(Program, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, usageText) }

	var cfgPath, iface, policyFile, logLevel string
	var apiHost, policyDir string
	var apiPort, metricsPort, queueMaxlen int
	var logJSON bool
	var simProto, simSrc, simDst, simSport, simDport string
	fs.StringVar(&cfgPath, "config", "", "YAML configuration file")
	fs.StringVar(&cfgPath, "c", "", "YAML configuration file (alias)")
	fs.StringVar(&iface, "interface", "", "interface to attach to")
	fs.StringVar(&policyFile, "policy-file", "", "path to YAML policy file")
	fs.StringVar(&logLevel, "log-level", "", "log level: debug, info, warn, error")
	fs.BoolVar(&logJSON, "log-json", false, "emit structured JSON logs")
	fs.StringVar(&apiHost, "api-host", "", "API bind host (default 127.0.0.1)")
	fs.IntVar(&apiPort, "api-port", 0, "API port; 0 disables (default 8080)")
	fs.IntVar(&metricsPort, "metrics-port", 0, "metrics port; 0 disables (default 9090)")
	fs.StringVar(&policyDir, "policy-dir", "", "durable policy store directory")
	fs.IntVar(&queueMaxlen, "queue-maxlen", 0, "kernel NFQUEUE queue length in packets (default 1024)")
	fs.StringVar(&simProto, "protocol", "", "simulated L4 protocol: tcp, udp, any (policy simulate)")
	fs.StringVar(&simSrc, "src", "", "simulated source IP (policy simulate)")
	fs.StringVar(&simDst, "dst", "", "simulated destination IP (policy simulate)")
	fs.StringVar(&simSport, "source-port", "", "simulated source port (policy simulate)")
	fs.StringVar(&simDport, "dest-port", "", "simulated destination port (policy simulate)")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return &Options{ShowHelp: true}, nil
		}
		return nil, err
	}

	opts := &Options{ConfigFile: cfgPath}
	rest := fs.Args()
	opts.Args = rest
	if len(rest) > 0 {
		opts.Command = rest[0]
	}
	if opts.Command == "config" || opts.Command == "policy" {
		if len(rest) > 1 {
			opts.Subcommand = rest[1]
		}
	}
	if iface != "" {
		opts.Overrides.Interface = &iface
	}
	if policyFile != "" {
		opts.Overrides.PolicyFile = &policyFile
	}
	if logLevel != "" {
		opts.Overrides.LogLevel = &logLevel
	}
	if logJSON {
		opts.Overrides.LogJSON = &logJSON
	}
	if apiHost != "" {
		opts.Overrides.APIHost = &apiHost
	}
	if apiPort != 0 {
		opts.Overrides.APIPort = &apiPort
	}
	if metricsPort != 0 {
		opts.Overrides.MetricsPort = &metricsPort
	}
	if policyDir != "" {
		opts.Overrides.PolicyDir = &policyDir
	}
	if queueMaxlen != 0 {
		opts.Overrides.QueueMaxlen = &queueMaxlen
	}
	opts.SimProto = simProto
	opts.SimSrc = simSrc
	opts.SimDst = simDst
	opts.SimSport = simSport
	opts.SimDport = simDport
	return opts, nil
}

// Usage prints the full usage text.
func Usage(w io.Writer) { fmt.Fprint(w, usageText) }

// PrintVersion prints the version line.
func PrintVersion(w io.Writer) { fmt.Fprintf(w, "%s %s\n", Program, Version) }

// ErrUnknownCommand is returned for unknown commands/subcommands.
var ErrUnknownCommand = errors.New("unknown command")

// ValidateCommand checks the command/subcommand combination. It returns an
// error with a message suitable for the user.
func (o *Options) ValidateCommand() error {
	if o.Command == "" {
		return nil
	}
	switch o.Command {
	case "config":
		switch o.Subcommand {
		case "", "show", "check":
			return nil
		}
	case "policy":
		switch o.Subcommand {
		case "", "validate", "simulate":
			return nil
		}
	case "hook":
		switch o.Subcommand {
		case "", "status", "cleanup":
			return nil
		}
	}
	return fmt.Errorf("%w %q (see %s --help)", ErrUnknownCommand, o.Command+" "+o.Subcommand, Program)
}

// Resolve loads the daemon configuration from all layers (defaults, file,
// environment, flags). It validates the resolved values; host-specific
// checks belong to cfg.Check.
func Resolve(opts *Options, env func(string) (string, bool)) (*config.Options, error) {
	return config.Load(opts.ConfigFile, env, opts.Overrides)
}
