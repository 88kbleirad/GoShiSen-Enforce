// Command goshisen is the GoShiSen-Enforce firewall daemon.
//
// The daemon enforces policy via NFQUEUE, provides a localhost REST API
// for policy management, and exposes Prometheus metrics.
package main

import (
	"fmt"
	"os"

	"github.com/88kbleirad/GoShiSen-Enforce/internal/app"
	"github.com/88kbleirad/GoShiSen-Enforce/internal/cli"
	"github.com/88kbleirad/GoShiSen-Enforce/internal/config"
	"github.com/88kbleirad/GoShiSen-Enforce/internal/hook"
	"github.com/88kbleirad/GoShiSen-Enforce/internal/policy"
)

func main() {
	opts, err := cli.Parse(os.Args[1:], os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\nTry '%s --help' for usage.\n", cli.Program, err, cli.Program)
		os.Exit(2)
	}
	if opts.ShowHelp {
		cli.Usage(os.Stdout)
		return
	}
	if opts.ShowVer {
		cli.PrintVersion(os.Stdout)
		return
	}

	if err := opts.ValidateCommand(); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", cli.Program, err)
		os.Exit(2)
	}

	switch opts.Command {
	case "config":
		if err := runConfig(opts); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", cli.Program, err)
			os.Exit(1)
		}
		return
	case "policy":
		if err := runPolicy(opts); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", cli.Program, err)
			os.Exit(1)
		}
		return
	case "hook":
		if err := runHook(opts); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", cli.Program, err)
			os.Exit(1)
		}
		return
	case "":
		// default: run the daemon
	default:
		// unreachable (ValidateCommand ensures this)
	}

	if err := app.RunCLI(opts.ConfigFile, opts.Overrides, os.LookupEnv); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", cli.Program, err)
		os.Exit(1)
	}
}

// runHook implements the `hook status` and `hook cleanup` subcommands.
// They manage only the application-owned nftables table (ip goshisen);
// no other nftables state is ever listed, flushed, or modified.
func runHook(opts *cli.Options) error {
	switch opts.Subcommand {
	case "", "status":
		if err := hook.Verify(); err != nil {
			return err
		}
		fmt.Printf("hook: table inet %s installed with queue rule\n", hook.Table)
		return nil
	case "cleanup":
		if err := hook.Cleanup(); err != nil {
			return err
		}
		fmt.Printf("hook: table inet %s removed\n", hook.Table)
		return nil
	}
	return fmt.Errorf("internal: unhandled hook subcommand %q", opts.Subcommand)
}

// runPolicy implements the `policy validate` and `policy simulate`
// subcommands. Both are offline: they compile a policy file and evaluate
// an in-memory packet; neither touches the network or firewall state.
func runPolicy(opts *cli.Options) error {
	// Args are all words after the first flag batch: ["policy", "simulate",
	// "--policy-file", …]. Skip the command and any subcommand.
	skip := 1
	if opts.Subcommand != "" {
		skip = 2
	}
	if err := cli.ApplyPolicyFlags(opts, opts.Args[skip:], os.Stderr); err != nil {
		return err
	}
	polFile := ""
	if opts.Overrides.PolicyFile != nil {
		polFile = *opts.Overrides.PolicyFile
	}
	if polFile == "" {
		return fmt.Errorf("policy: no policy file (use --policy-file)")
	}

	raw, err := os.ReadFile(polFile)
	if err != nil {
		return fmt.Errorf("policy: read %s: %w", polFile, err)
	}
	snap, err := policy.Compile(raw)
	if err != nil {
		return err
	}

	switch opts.Subcommand {
	case "", "validate":
		fmt.Printf("policy %s: valid, %d rule(s), default action %s\n",
			polFile, snap.Len(), snap.DefaultAction())
		for _, r := range snap.Rules() {
			fmt.Printf("  %s\n", r)
		}
		return nil
	case "simulate":
		tgt, err := cli.ParseSimTarget(opts)
		if err != nil {
			return err
		}
		v := snap.Decide(tgt.Packet)
		fmt.Printf("verdict: %s\n", v.Action)
		if v.Matched {
			fmt.Printf("matched rule: %s\n", v.RuleID)
		} else {
			fmt.Printf("matched rule: (default)\n")
		}
		return nil
	}
	return fmt.Errorf("internal: unhandled policy subcommand %q", opts.Subcommand)
}

// runConfig implements the `config show` and `config check` subcommands.
func runConfig(opts *cli.Options) error {
	resolved, err := cli.Resolve(opts, os.LookupEnv)
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	switch opts.Subcommand {
	case "show":
		fmt.Printf("config file: %s\n", resolved.ConfigFile)
		fmt.Printf("interface:    %q (from %s)\n", resolved.Interface, resolved.Source("interface"))
		fmt.Printf("policy_file:  %q (from %s)\n", resolved.PolicyFile, resolved.Source("policy_file"))
		fmt.Printf("log_level:    %q (from %s)\n", resolved.LogLevel, resolved.Source("log_level"))
		fmt.Printf("log_json:     %v (from %s)\n", resolved.LogJSON, resolved.Source("log_json"))
		fmt.Printf("api_host:     %q (from %s)\n", resolved.APIHost, resolved.Source("api_host"))
		fmt.Printf("api_port:     %d (from %s)\n", resolved.APIPort, resolved.Source("api_port"))
		fmt.Printf("metrics_port: %d (from %s)\n", resolved.MetricsPort, resolved.Source("metrics_port"))
		fmt.Printf("policy_dir:   %q (from %s)\n", resolved.PolicyDir, resolved.Source("policy_dir"))
		fmt.Printf("api_token:    %q (from %s)\n", config.RedactedValue(resolved.APIToken), resolved.Source("api_token"))
		return nil
	case "check":
		if err := resolved.Check(); err != nil {
			return fmt.Errorf("host validation failed: %w", err)
		}
		// InterfaceName performs the read-only interface existence check.
		iface, err := resolved.InterfaceName()
		if err != nil {
			return fmt.Errorf("host validation failed: %w", err)
		}
		fmt.Printf("ok: configuration valid; interface %q present and up; "+
			"policy file %q readable\n", iface, resolved.PolicyFile)
		return nil
	}
	return fmt.Errorf("internal: unhandled config subcommand %q", opts.Subcommand)
}
