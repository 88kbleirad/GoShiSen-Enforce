// Package policy implements the YAML policy parser and the evaluation
// engine for GoShiSen-Enforce. It is backend-independent: evaluation
// consumes a Packet metadata value, so the engine can be built and tested
// without root privileges or a live network.
//
// Session: this package implements the schema subset listed in
// docs/design/architecture.md §4 that does not depend on the conntrack
// mirror or NFQUEUE interface metadata. The `state`, `in_iface`, and
// `out_iface` fields are recognized but rejected at compile time with a
// clear "reserved for a later phase" error.
package policy

import (
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// SchemaVersion is the only accepted policy file version.
const SchemaVersion = 1

// Action is a verdict.
type Action int

const (
	// ActionDrop discards the packet.
	ActionDrop Action = iota
	// ActionAccept forwards the packet.
	ActionAccept
)

// String renders the action as its policy spelling.
func (a Action) String() string {
	switch a {
	case ActionAccept:
		return "ACCEPT"
	case ActionDrop:
		return "DROP"
	}
	return "UNKNOWN"
}

// ParseAction converts a YAML action string.
func ParseAction(s string) (Action, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "ACCEPT":
		return ActionAccept, nil
	case "DROP":
		return ActionDrop, nil
	}
	return 0, fmt.Errorf("action %q must be ACCEPT or DROP", s)
}

// Proto is an L4 protocol matcher.
type Proto int

const (
	// ProtoAny matches TCP and UDP (all queued traffic).
	ProtoAny Proto = iota
	// ProtoTCP matches TCP.
	ProtoTCP
	// ProtoUDP matches UDP.
	ProtoUDP
)

// String renders the protocol as its policy spelling.
func (p Proto) String() string {
	switch p {
	case ProtoTCP:
		return "tcp"
	case ProtoUDP:
		return "udp"
	}
	return "any"
}

// ParseProto converts a YAML protocol string. Empty means "any".
func ParseProto(s string) (Proto, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "any":
		return ProtoAny, nil
	case "tcp":
		return ProtoTCP, nil
	case "udp":
		return ProtoUDP, nil
	}
	return 0, fmt.Errorf("protocol %q must be tcp, udp, or any", s)
}

// PortRange is an inclusive port interval [Lo, Hi].
type PortRange struct {
	Lo uint16
	Hi uint16
}

// Contains reports whether p lies within the range.
func (r PortRange) Contains(p uint16) bool {
	return p >= r.Lo && p <= r.Hi
}

// String renders the range as its policy spelling.
func (r PortRange) String() string {
	if r.Lo == r.Hi {
		return fmt.Sprintf("%d", r.Lo)
	}
	return fmt.Sprintf("%d:%d", r.Lo, r.Hi)
}

// parsePortRange parses "N" or "LO:HI" (inclusive). Both endpoints must
// be 1..65535 and LO <= HI.
func parsePortRange(s string) (PortRange, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return PortRange{}, errors.New("port matcher is empty")
	}
	if strings.Count(s, ":") > 1 {
		return PortRange{}, fmt.Errorf("port matcher %q has more than one ':'", s)
	}
	loStr, hiStr, hasHi := strings.Cut(s, ":")
	lo, err := parsePortNum(loStr)
	if err != nil {
		return PortRange{}, fmt.Errorf("port matcher %q: %w", s, err)
	}
	if !hasHi {
		return PortRange{Lo: lo, Hi: lo}, nil
	}
	hi, err := parsePortNum(hiStr)
	if err != nil {
		return PortRange{}, fmt.Errorf("port matcher %q: %w", s, err)
	}
	if lo > hi {
		return PortRange{}, fmt.Errorf("port matcher %q has lo > hi", s)
	}
	return PortRange{Lo: lo, Hi: hi}, nil
}

func parsePortNum(s string) (uint16, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("empty port")
	}
	if s == "0" {
		return 0, errors.New("port 0 is invalid (valid range 1..65535)")
	}
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return 0, fmt.Errorf("port %q is not an integer", s)
	}
	if n < 1 || n > 65535 {
		return 0, fmt.Errorf("port %d is outside 1..65535", n)
	}
	return uint16(n), nil
}

// parseStates converts a YAML state list into a validated, de-duplicated
// state set. An empty list means "all states".
func parseStates(in []string) ([]State, error) {
	if len(in) == 0 {
		return nil, nil
	}
	seen := make(map[State]bool, len(in))
	out := make([]State, 0, len(in))
	for _, s := range in {
		st, err := ParseState(s)
		if err != nil {
			return nil, err
		}
		if !seen[st] {
			seen[st] = true
			out = append(out, st)
		}
	}
	return out, nil
}

// yamlRule mirrors the YAML rule schema. Reserved fields are typed so we
// can detect presence and reject them with a targeted message.
type yamlRule struct {
	ID          string   `yaml:"id"`
	Action      string   `yaml:"action"`
	Protocol    string   `yaml:"protocol"`
	Src         string   `yaml:"src"`
	Dst         string   `yaml:"dst"`
	SrcPort     string   `yaml:"src_port"`
	DstPort     string   `yaml:"dst_port"`
	State       []string `yaml:"state"`
	InIface     string   `yaml:"in_iface"`
	OutIface    string   `yaml:"out_iface"`
	Log         bool     `yaml:"log"`
	Description string   `yaml:"description"`
}

// yamlPolicy mirrors the YAML policy file.
type yamlPolicy struct {
	Version       int        `yaml:"version"`
	DefaultAction string     `yaml:"default_action"`
	Rules         []yamlRule `yaml:"rules"`
}

// Compile parses and validates policy YAML into an immutable Snapshot.
// It returns every validation error, not just the first.
func Compile(data []byte) (*Snapshot, error) {
	var doc yamlPolicy
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("policy: parse: %w", err)
	}
	if doc.Version != SchemaVersion {
		return nil, fmt.Errorf("policy: version %d is not supported (want %d)", doc.Version, SchemaVersion)
	}

	defaultAction := ActionDrop
	if doc.DefaultAction != "" {
		a, err := ParseAction(doc.DefaultAction)
		if err != nil {
			return nil, fmt.Errorf("policy: default_action: %w", err)
		}
		defaultAction = a
	}

	seen := make(map[string]struct{}, len(doc.Rules))
	compiled := make([]Rule, 0, len(doc.Rules))
	var errs []error

	for i, yr := range doc.Rules {
		fail := func(format string, args ...any) {
			errs = append(errs, fmt.Errorf("policy: rule %d (%s): %s", i+1, yr.ID, fmt.Sprintf(format, args...)))
		}
		before := len(errs)

		if yr.ID == "" {
			fail("id is empty; every rule needs a unique id")
		} else if _, dup := seen[yr.ID]; dup {
			fail("duplicate id %q", yr.ID)
		} else {
			seen[yr.ID] = struct{}{}
		}

		var statesList []State
		if yr.State != nil {
			states, err := parseStates(yr.State)
			if err != nil {
				fail("%v", err)
			} else {
				statesList = states
			}
		}
		if yr.InIface != "" || yr.OutIface != "" {
			fail("fields \"in_iface\"/\"out_iface\" are reserved for interface matching (later phase); remove them")
		}

		action, aerr := ParseAction(yr.Action)
		if aerr != nil {
			fail("%v", aerr)
		}
		proto, perr := ParseProto(yr.Protocol)
		if perr != nil {
			fail("%v", perr)
		}
		protoOK := perr == nil

		src, serr := ParsePrefix(yr.Src)
		if serr != nil {
			fail("src: %v", serr)
		}
		dst, derr := ParsePrefix(yr.Dst)
		if derr != nil {
			fail("dst: %v", derr)
		}

		var srcPort, dstPort *PortRange
		if yr.SrcPort != "" {
			if protoOK && proto == ProtoAny {
				fail("src_port requires protocol tcp or udp (got %q)", yr.Protocol)
			} else if pr, err := parsePortRange(yr.SrcPort); err != nil {
				fail("src_port: %v", err)
			} else if protoOK {
				srcPort = &pr
			}
		}
		if yr.DstPort != "" {
			if protoOK && proto == ProtoAny {
				fail("dst_port requires protocol tcp or udp (got %q)", yr.Protocol)
			} else if pr, err := parsePortRange(yr.DstPort); err != nil {
				fail("dst_port: %v", err)
			} else if protoOK {
				dstPort = &pr
			}
		}

		if len(errs) == before {
			compiled = append(compiled, Rule{
				ID:          yr.ID,
				Action:      action,
				Protocol:    proto,
				Src:         src,
				Dst:         dst,
				SrcPort:     srcPort,
				DstPort:     dstPort,
				States:      statesList,
				Log:         yr.Log,
				Description: yr.Description,
			})
		}
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return NewSnapshot(defaultAction, compiled), nil
}
