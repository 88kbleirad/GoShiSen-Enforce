package cli

import (
	"strings"
	"testing"

	"github.com/88kbleirad/GoShiSen-Enforce/internal/policy"
)

func TestParseSimTarget(t *testing.T) {
	base := func() Options {
		return Options{
			SimSrc:   "10.0.0.1",
			SimDst:   "10.0.0.2",
			SimProto: "tcp",
			SimSport: "12345",
			SimDport: "443",
		}
	}

	t.Run("full tuple", func(t *testing.T) {
		o := base()
		tgt, err := ParseSimTarget(&o)
		if err != nil {
			t.Fatal(err)
		}
		p := tgt.Packet
		if p.Protocol != policy.ProtoTCP || p.Src.String() != "10.0.0.1" || p.Dst.String() != "10.0.0.2" {
			t.Errorf("packet = %+v", p)
		}
		if p.SrcPort != 12345 || p.DstPort != 443 {
			t.Errorf("ports = %d/%d", p.SrcPort, p.DstPort)
		}
		if !tgt.ProtoGiven {
			t.Error("ProtoGiven = false")
		}
	})

	t.Run("defaults", func(t *testing.T) {
		o := base()
		o.SimProto = ""
		o.SimSport = ""
		o.SimDport = ""
		tgt, err := ParseSimTarget(&o)
		if err != nil {
			t.Fatal(err)
		}
		if tgt.ProtoGiven {
			t.Error("ProtoGiven = true for empty --protocol")
		}
		if tgt.Packet.Protocol != policy.ProtoAny {
			t.Errorf("proto = %d, want any", tgt.Packet.Protocol)
		}
		if tgt.Packet.SrcPort != 0 || tgt.Packet.DstPort != 0 {
			t.Errorf("ports = %d/%d, want 0/0", tgt.Packet.SrcPort, tgt.Packet.DstPort)
		}
	})

	cases := []struct {
		name string
		mut  func(*Options)
		want string
	}{
		{"missing src", func(o *Options) { o.SimSrc = "" }, "--src"},
		{"missing dst", func(o *Options) { o.SimDst = "" }, "--dst"},
		{"bad src", func(o *Options) { o.SimSrc = "not-an-ip" }, "--src"},
		{"bad protocol", func(o *Options) { o.SimProto = "sctp" }, "--protocol"},
		{"bad sport", func(o *Options) { o.SimSport = "abc" }, "--source-port"},
		{"sport out of range", func(o *Options) { o.SimSport = "70000" }, "--source-port"},
		{"bad dport", func(o *Options) { o.SimDport = "0" }, "--dest-port"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := base()
			tc.mut(&o)
			_, err := ParseSimTarget(&o)
			if err == nil {
				t.Fatal("ParseSimTarget succeeded, want error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}
