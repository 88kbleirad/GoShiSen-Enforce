package metrics

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestRegistryVerdicts(t *testing.T) {
	r := New()
	r.ObserveVerdict(VerdictLabels{Action: "accept", Protocol: "tcp", State: "new"})
	r.ObserveVerdict(VerdictLabels{Action: "accept", Protocol: "tcp", State: "new"})
	r.ObserveVerdict(VerdictLabels{Action: "drop", Protocol: "udp", State: "established"})

	var buf bytes.Buffer
	r.WritePrometheus(&buf)
	out := buf.String()

	if !strings.Contains(out, `goshisen_packets_total{action="accept",protocol="tcp"} 2`) {
		t.Errorf("missing accepted tcp counter:\n%s", out)
	}
	if !strings.Contains(out, `goshisen_packets_total{action="drop",protocol="udp"} 1`) {
		t.Errorf("missing dropped udp counter:\n%s", out)
	}
	if !strings.Contains(out, `goshisen_packets_by_state_total{state="new"} 2`) {
		t.Errorf("missing state counter:\n%s", out)
	}
	// No unbounded labels (no IPs/ports).
	if strings.Contains(out, "10.") || strings.Contains(out, "src=") || strings.Contains(out, "dst=") {
		t.Errorf("unbounded label leaked:\n%s", out)
	}
}

func TestRegistryBounds(t *testing.T) {
	r := New()
	// Cardinality is bounded by the fixed enum; hammer a few and ensure
	// the registry stays small.
	for i := 0; i < 100; i++ {
		r.ObserveVerdict(VerdictLabels{Action: "drop", Protocol: "tcp", State: "invalid"})
	}
	r.ObserveBackendError(BackendErrParse)
	r.ObserveBackendError(BackendErrParse)
	r.ObserveLatency(10 * time.Microsecond)
	r.ObserveLatency(2 * time.Millisecond)
	r.SetActivePolicyVersion(7)

	var buf bytes.Buffer
	r.WritePrometheus(&buf)
	out := buf.String()
	if !strings.Contains(out, "goshisen_backend_errors_total{kind=\"parse\"} 2") {
		t.Errorf("missing backend error counter:\n%s", out)
	}
	if !strings.Contains(out, "goshisen_active_policy_version 7") {
		t.Errorf("missing active version gauge:\n%s", out)
	}
	if !strings.Contains(out, "goshisen_packet_verdict_latency_seconds_count 2") {
		t.Errorf("missing histogram count:\n%s", out)
	}
}

func TestRegistryNoSecretValues(t *testing.T) {
	// The registry never carries token material by construction; this is
	// a guard that WritePrometheus output never contains a plausible
	// secret pattern.
	r := New()
	var buf bytes.Buffer
	r.WritePrometheus(&buf)
	if strings.Contains(buf.String(), "token") {
		t.Errorf("token appeared in metrics:\n%s", buf.String())
	}
}

func TestRegistryQueueOverflow(t *testing.T) {
	r := New()
	r.ObserveQueueOverflow(OverflowKernelDrop, 3)
	r.ObserveQueueOverflow(OverflowUserDrop, 1)
	r.ObserveQueueOverflow(OverflowKernelDrop, 2)

	var buf bytes.Buffer
	r.WritePrometheus(&buf)
	out := buf.String()
	if !strings.Contains(out, `goshisen_queue_overflow_total{cause="kernel_drop"} 5`) {
		t.Errorf("kernel_drop counter wrong:\n%s", out)
	}
	if !strings.Contains(out, `goshisen_queue_overflow_total{cause="user_drop"} 1`) {
		t.Errorf("user_drop counter wrong:\n%s", out)
	}
	// Zero increments must not create entries.
	r.ObserveQueueOverflow(OverflowKernelDrop, 0)
	var buf2 bytes.Buffer
	r.WritePrometheus(&buf2)
	if buf2.String() != out {
		t.Errorf("zero increment changed output:\n%s", buf2.String())
	}
}
