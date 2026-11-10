// Package metrics exposes the daemon's Prometheus metrics. Label
// cardinality is bounded by design: verdict labels come from a fixed
// enum (action, protocol, state); never from addresses, ports, or flow
// IDs.
package metrics

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
)

// Registry is a small, hand-rolled Prometheus text-format registry. It
// deliberately avoids a third-party client library (stdlib-first).
type Registry struct {
	mu sync.Mutex

	counters  map[string]*counter
	gauges    map[string]*gauge
	histogram *histogram
}

// New creates a registry with all daemon metrics registered.
func New() *Registry {
	r := &Registry{
		counters: make(map[string]*counter),
		gauges:   make(map[string]*gauge),
	}
	r.counters["goshisen_packets_total"] = newCounter("goshisen_packets_total",
		"Packets decided, by action and protocol.", "action", "protocol")
	r.counters["goshisen_packets_by_state_total"] = newCounter("goshisen_packets_by_state_total",
		"Packets decided, by state.", "state")
	r.counters["goshisen_backend_errors_total"] = newCounter("goshisen_backend_errors_total",
		"Backend errors (receive/send/parse/panic).", "kind")
	r.counters["goshisen_queue_overflow_total"] = newCounter("goshisen_queue_overflow_total",
		"Packets dropped by the kernel before reaching the daemon, by cause. fail-closed by design.", "cause")
	r.gauges["goshisen_active_policy_version"] = newGauge("goshisen_active_policy_version",
		"Active policy version (0 when none).")
	r.histogram = newHistogram("goshisen_packet_verdict_latency_seconds",
		"Per-packet processing latency.")
	return r
}

type counter struct {
	name   string
	help   string
	labels []string
	values map[string]int64
}

func newCounter(name, help string, labels ...string) *counter {
	return &counter{name: name, help: help, labels: labels, values: make(map[string]int64)}
}

type gauge struct {
	name  string
	help  string
	value int64
}

func newGauge(name, help string) *gauge { return &gauge{name: name, help: help} }

type histogram struct {
	name    string
	help    string
	buckets []float64
	counts  []int64
	sum     float64
	count   int64
}

var defaultBuckets = []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1}

func newHistogram(name, help string) *histogram {
	return &histogram{name: name, help: help, buckets: defaultBuckets, counts: make([]int64, len(defaultBuckets))}
}

// VerdictLabels is a bounded set of verdict labels.
type VerdictLabels struct {
	Action   string // "accept" | "drop"
	Protocol string // "tcp" | "udp" | "any"
	State    string // "new" | "established" | "related" | "invalid" | "unknown"
}

// ObserveVerdict records a packet verdict.
func (r *Registry) ObserveVerdict(v VerdictLabels) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := v.Action + "|" + v.Protocol
	r.counters["goshisen_packets_total"].values[key]++
	skey := v.State
	if skey == "" {
		skey = "unknown"
	}
	r.counters["goshisen_packets_by_state_total"].values[skey]++
}

// BackendErrorKind is a bounded enum of backend error kinds. Using a
// typed enum (not a free-form string) keeps metric label cardinality
// bounded by construction.
type BackendErrorKind string

const (
	// BackendErrReceive is a netlink receive error in the queue loop.
	BackendErrReceive BackendErrorKind = "receive"
	// BackendErrSend is a netlink send (verdict) error.
	BackendErrSend BackendErrorKind = "send"
	// BackendErrParse is a packet parse failure.
	BackendErrParse BackendErrorKind = "parse"
	// BackendErrPanic is a recovered engine panic.
	BackendErrPanic BackendErrorKind = "panic"
)

// ObserveBackendError records a backend error by bounded kind.
func (r *Registry) ObserveBackendError(kind BackendErrorKind) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counters["goshisen_backend_errors_total"].values[string(kind)]++
}

// OverflowCause is a bounded enum of kernel queue-drop causes.
type OverflowCause string

const (
	// OverflowKernelDrop is a packet dropped because the queue was full
	// (queue_maxlen exhausted) — the kernel's fail-closed path.
	OverflowKernelDrop OverflowCause = "kernel_drop"
	// OverflowUserDrop is a packet dropped because netlink unicast to
	// the daemon's socket failed (no listener / socket error). The
	// kernel still drops the packet — never fail-open.
	OverflowUserDrop OverflowCause = "user_drop"
)

// ObserveQueueOverflow records one or more packets dropped by the
// kernel queue. The cause is a fixed enum; cardinality stays bounded.
func (r *Registry) ObserveQueueOverflow(cause OverflowCause, n uint64) {
	if n == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counters["goshisen_queue_overflow_total"].values[string(cause)] += int64(n)
}

// SetActivePolicyVersion sets the active policy version gauge (0 = none).
func (r *Registry) SetActivePolicyVersion(version int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gauges["goshisen_active_policy_version"].value = version
}

// ObserveLatency records packet processing latency.
func (r *Registry) ObserveLatency(d time.Duration) {
	secs := d.Seconds()
	r.mu.Lock()
	defer r.mu.Unlock()
	h := r.histogram
	h.sum += secs
	h.count++
	for i, b := range h.buckets {
		if secs <= b {
			h.counts[i]++
		}
	}
}

// WritePrometheus renders the registry in Prometheus text exposition
// format.
func (r *Registry) WritePrometheus(w io.Writer) {
	r.mu.Lock()
	defer r.mu.Unlock()

	names := make([]string, 0, len(r.counters)+len(r.gauges))
	for n := range r.counters {
		names = append(names, n)
	}
	for n := range r.gauges {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if c, ok := r.counters[n]; ok {
			fmt.Fprintf(w, "# HELP %s %s\n", c.name, c.help)
			fmt.Fprintf(w, "# TYPE %s counter\n", c.name)
			keys := make([]string, 0, len(c.values))
			for k := range c.values {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				parts := strings.Split(k, "|")
				fmt.Fprintf(w, "%s{", c.name)
				for i, l := range c.labels {
					if i > 0 {
						fmt.Fprint(w, ",")
					}
					fmt.Fprintf(w, "%s=%q", l, parts[i])
				}
				fmt.Fprintf(w, "} %d\n", c.values[k])
			}
			continue
		}
		if g, ok := r.gauges[n]; ok {
			fmt.Fprintf(w, "# HELP %s %s\n", g.name, g.help)
			fmt.Fprintf(w, "# TYPE %s gauge\n", g.name)
			fmt.Fprintf(w, "%s %d\n", g.name, g.value)
		}
	}
	h := r.histogram
	fmt.Fprintf(w, "# HELP %s %s\n", h.name, h.help)
	fmt.Fprintf(w, "# TYPE %s histogram\n", h.name)
	cum := int64(0)
	for i, b := range h.buckets {
		cum += h.counts[i]
		fmt.Fprintf(w, "%s_bucket{le=%q} %d\n", h.name, fmt.Sprintf("%g", b), cum)
	}
	fmt.Fprintf(w, "%s_bucket{le=\"+Inf\"} %d\n", h.name, h.count)
	fmt.Fprintf(w, "%s_sum %g\n", h.name, h.sum)
	fmt.Fprintf(w, "%s_count %d\n", h.name, h.count)
}
