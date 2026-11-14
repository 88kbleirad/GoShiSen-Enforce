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
	"github.com/88kbleirad/GoShiSen-Enforce/internal/store"
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
	case "offload":
		if err := runOffload(opts); err != nil {
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

// runOffload implements the `offload status` and `offload disable`
// subcommands. status is read-only; disable removes nftables
// flow-offload state (flowtables + flow-offload rules only) and
// re-verifies. Nothing else is touched.
func runOffload(opts *cli.Options) error {
	switch opts.Subcommand {
	case "", "status":
		rep := hook.DetectFlowOffload()
		switch rep.State {
		case hook.FlowOffloadDisabled:
			fmt.Println("offload: disabled (no flowtable / flow-offload rules)")
		case hook.FlowOffloadEnabled:
			fmt.Printf("offload: ENABLED (enforcement gap): %s\n", rep.Detail)
		case hook.FlowOffloadFailed:
			fmt.Printf("offload: FAILED (disable did not take effect): %s\n", rep.Detail)
		default:
			fmt.Printf("offload: UNKNOWN (cannot prove safety): %s\n", rep.Detail)
		}
		return nil
	case "disable":
		rep := hook.DisableFlowOffload()
		switch rep.State {
		case hook.FlowOffloadDisabled:
			fmt.Println("offload: disabled (verified)")
			return nil
		case hook.FlowOffloadFailed:
			return fmt.Errorf("offload: disable FAILED (still active): %s", rep.Detail)
		default:
			return fmt.Errorf("offload: disable failed: state=%s: %s", rep.State, rep.Detail)
		}
	}
	return fmt.Errorf("internal: unhandled offload subcommand %q", opts.Subcommand)
}

// runPolicy implements the `policy validate`, `policy simulate`, and
// `policy dry-run` subcommands. All are offline: they compile a policy
// file and evaluate an in-memory packet; neither touches the network or
// firewall state. dry-run reads the durable policy store (never writes)
// to preview the exact publish the daemon would perform.
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
	case "dry-run":
		return runPolicyDryRun(opts, polFile, raw, snap)
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

// runPolicyDryRun validates a policy and previews the publish the
// daemon would perform against the durable store: the next version
// that would be assigned, the rule delta vs the active policy, and any
// default-action change. It is strictly read-only.
func runPolicyDryRun(opts *cli.Options, polFile string, raw []byte, snap *policy.Snapshot) error {
	resolved, err := cli.Resolve(opts, os.LookupEnv)
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	st, err := store.OpenReadOnly(resolved.PolicyDir)
	if err != nil {
		return fmt.Errorf("policy dry-run: open store: %w", err)
	}
	next, err := st.NextVersion()
	if err != nil {
		return fmt.Errorf("policy dry-run: %w", err)
	}

	active, err := st.Active()
	if err != nil {
		return fmt.Errorf("policy dry-run: %w", err)
	}
	activeRules, activeDefault := -1, "none"
	if active != "" {
		data, rerr := st.ReadPolicy(active)
		if rerr != nil {
			return fmt.Errorf("policy dry-run: read active: %w", rerr)
		}
		asnap, cerr := policy.Compile(data)
		if cerr != nil {
			return fmt.Errorf("policy dry-run: active policy %s is corrupt: %w", active, cerr)
		}
		activeRules, activeDefault = asnap.Len(), asnap.DefaultAction().String()
	}

	fmt.Printf("policy %s: valid, %d rule(s), default action %s\n",
		polFile, snap.Len(), snap.DefaultAction())
	fmt.Printf("dry-run: would publish version %s (active: %s)\n", next, active)
	fmt.Printf("  rules: %d -> %d (%s)\n", activeRules, snap.Len(),
		func() string {
			if activeRules < 0 {
				return "first policy"
			}
			switch d := snap.Len() - activeRules; {
			case d > 0:
				return fmt.Sprintf("+%d", d)
			case d == 0:
				return "no change"
			default:
				return fmt.Sprintf("%d", d)
			}
		}())
	fmt.Printf("  default action: %s -> %s\n", activeDefault, snap.DefaultAction())
	if active != "" {
		fmt.Printf("  target: %s (version %s) — nothing written\n", st.VersionPath(next), next)
	}
	return nil
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
