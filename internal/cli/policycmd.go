package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/netip"
	"strconv"
	"strings"

	"github.com/88kbleirad/GoShiSen-Enforce/internal/policy"
)

// ApplyPolicyFlags parses the flags that follow a `policy` subcommand
// (e.g. `goshisen policy simulate --src 1.2.3.4`). It mutates o in
// place: a --policy-file given after the command overrides the global
// --policy-file.
func ApplyPolicyFlags(o *Options, rest []string, stderr io.Writer) error {
	fs := flag.NewFlagSet(Program+" policy", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, "usage: goshisen policy [validate|simulate] [flags]\n") }

	var policyFile, simProto, simSrc, simDst, simSport, simDport, simState string
	fs.StringVar(&policyFile, "policy-file", "", "path to YAML policy file")
	fs.StringVar(&simProto, "protocol", "", "L4 protocol: tcp, udp, any")
	fs.StringVar(&simSrc, "src", "", "simulated source IP")
	fs.StringVar(&simDst, "dst", "", "simulated destination IP")
	fs.StringVar(&simSport, "source-port", "", "simulated source port")
	fs.StringVar(&simDport, "dest-port", "", "simulated destination port")
	fs.StringVar(&simState, "state", "", "simulated connection state: new, established, related, invalid")

	if err := fs.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.Usage()
			return nil
		}
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments after policy subcommand: %v", fs.Args())
	}
	if policyFile != "" {
		o.Overrides.PolicyFile = &policyFile
	}
	o.SimProto = simProto
	o.SimSrc = simSrc
	o.SimDst = simDst
	o.SimSport = simSport
	o.SimDport = simDport
	o.SimState = simState
	return nil
}

// SimTarget is the parsed `policy simulate` packet description.
type SimTarget struct {
	Packet policy.Packet
	// ProtoGiven records whether --protocol was supplied.
	ProtoGiven bool
}

// ParseSimTarget converts the simulate subcommand's flags into a packet.
// Missing addresses are an error (a simulation needs a concrete tuple);
// protocol defaults to "any"; ports default to 0.
func ParseSimTarget(o *Options) (SimTarget, error) {
	if o.SimSrc == "" || o.SimDst == "" {
		return SimTarget{}, fmt.Errorf("policy simulate requires --src and --dst")
	}
	src, err := netip.ParseAddr(o.SimSrc)
	if err != nil {
		return SimTarget{}, fmt.Errorf("--src %q: not a valid IP address", o.SimSrc)
	}
	dst, err := netip.ParseAddr(o.SimDst)
	if err != nil {
		return SimTarget{}, fmt.Errorf("--dst %q: not a valid IP address", o.SimDst)
	}

	var proto policy.Proto
	protoGiven := o.SimProto != ""
	if protoGiven {
		switch strings.ToLower(o.SimProto) {
		case "tcp":
			proto = policy.ProtoTCP
		case "udp":
			proto = policy.ProtoUDP
		case "any":
			proto = policy.ProtoAny
		default:
			return SimTarget{}, fmt.Errorf("--protocol %q: must be tcp, udp, or any", o.SimProto)
		}
	}

	sport, err := parseSimPort("--source-port", o.SimSport)
	if err != nil {
		return SimTarget{}, err
	}
	dport, err := parseSimPort("--dest-port", o.SimDport)
	if err != nil {
		return SimTarget{}, err
	}

	var st policy.State
	if o.SimState != "" {
		st, err = policy.ParseState(o.SimState)
		if err != nil {
			return SimTarget{}, err
		}
	}

	return SimTarget{
		Packet: policy.Packet{
			Protocol: proto,
			Src:      src,
			Dst:      dst,
			SrcPort:  sport,
			DstPort:  dport,
			State:    st,
		},
		ProtoGiven: protoGiven,
	}, nil
}

func parseSimPort(flag, s string) (uint16, error) {
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 65535 {
		return 0, fmt.Errorf("%s %q: must be an integer in 1..65535", flag, s)
	}
	return uint16(n), nil
}
