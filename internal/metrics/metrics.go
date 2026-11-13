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

// Label enum tables. The per-packet verdict counters are fixed arrays
// indexed by these values, so the hot path (ObservePacket) does no
// string construction and no map lookup — only a mutex and integer
// increments.
var (
	actionNames     = [2]string{"drop", "accept"}
	protocolNames   = [3]string{"any", "tcp", "udp"}
	stateNames      = [5]string{"unknown", "new", "established", "related", "invalid"}
	backendErrNames = [4]string{"receive", "send", "parse", "panic"}
	overflowNames   = [2]string{"kernel_drop", "user_drop"}
	outcomeNames    = [3]string{"success", "failure", "conflict"}
)

const (
	// ActionDropIdx and ActionAcceptIdx index actionNames.
	ActionDropIdx = iota
	ActionAcceptIdx
)

const (
	// ProtoAnyIdx, ProtoTCPIdx, ProtoUDPIdx index protocolNames.
	ProtoAnyIdx = iota
	ProtoTCPIdx
	ProtoUDPIdx
)

const (
	// StateUnknownIdx .. StateInvalidIdx index stateNames.
	StateUnknownIdx = iota
	StateNewIdx
	StateEstablishedIdx
	StateRelatedIdx
	StateInvalidIdx
)

// Registry is a small, hand-rolled Prometheus text-format registry. It
// deliberately avoids a third-party client library (stdlib-first).
type Registry struct {
	mu sync.Mutex

	counters  map[string]*counter
	gauges    map[string]*gauge
	histogram *histogram

	// Per-packet verdict counters: fixed arrays, indexed by the label
	// enums above. No allocation, no string keys, no map growth.
	packetsTotal   [2][3]int64
	packetsByState [5]int64
}

// New creates a registry with all daemon metrics registered.
func New() *Registry {
	r := &Registry{
		counters: make(map[string]*counter),
		gauges:   make(map[string]*gauge),
	}
	r.counters["goshisen_backend_errors_total"] = newCounter("goshisen_backend_errors_total",
		"Backend errors (receive/send/parse/panic).", "kind")
	r.counters["goshisen_queue_overflow_total"] = newCounter("goshisen_queue_overflow_total",
		"Packets dropped by the kernel before reaching the daemon, by cause. fail-closed by design.", "cause")
	r.counters["goshisen_policy_updates_total"] = newCounter("goshisen_policy_updates_total",
		"Policy publish attempts, by bounded outcome.", "outcome")
	r.gauges["goshisen_active_policy_version"] = newGauge("goshisen_active_policy_version",
		"Active policy version (0 when none).")
	r.gauges["goshisen_flow_offload_active"] = newGauge("goshisen_flow_offload_active",
		"1 when flow offloading is configured in nftables (established flows bypass the queue; enforcement is not applied to them), 0 otherwise.")
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

// VerdictLabels is the per-packet verdict label tuple as enum indexes.
// Indexes (not strings) keep the hot path allocation-free.
type VerdictLabels struct {
	ActionIdx   int
	ProtocolIdx int
	StateIdx    int
}

// ObservePacket records one packet decision: verdict counters, state
// counter, and processing latency under a single lock acquisition. This
// is the per-packet hot path of the daemon.
func (r *Registry) ObservePacket(v VerdictLabels, d time.Duration) {
	secs := d.Seconds()
	r.mu.Lock()
	r.packetsTotal[v.ActionIdx][v.ProtocolIdx]++
	r.packetsByState[v.StateIdx]++
	h := r.histogram
	h.sum += secs
	h.count++
	for i, b := range h.buckets {
		if secs <= b {
			h.counts[i]++
		}
	}
	r.mu.Unlock()
}

// ObserveBackendError records a backend error by bounded kind.
func (r *Registry) ObserveBackendError(kind BackendErrorKind) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counters["goshisen_backend_errors_total"].values[backendErrNames[kind]]++
}

// ObserveQueueOverflow records one or more packets dropped by the
// kernel queue. The cause is a fixed enum; cardinality stays bounded.
func (r *Registry) ObserveQueueOverflow(cause OverflowCause, n uint64) {
	if n == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counters["goshisen_queue_overflow_total"].values[overflowNames[cause]] += int64(n)
}

// ObservePolicyUpdate records one policy publish attempt outcome.
func (r *Registry) ObservePolicyUpdate(outcome PolicyUpdateOutcome) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counters["goshisen_policy_updates_total"].values[outcomeNames[outcome]]++
}

// SetActivePolicyVersion sets the active policy version gauge (0 = none).
func (r *Registry) SetActivePolicyVersion(version int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gauges["goshisen_active_policy_version"].value = version
}

// SetFlowOffloadActive records the result of the flow-offload
// detection (1 = offloading configured, 0 = not).
func (r *Registry) SetFlowOffloadActive(active bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v := int64(0)
	if active {
		v = 1
	}
	r.gauges["goshisen_flow_offload_active"].value = v
}

// PolicyUpdateOutcome is a bounded enum of policy publish results. Using
// a typed enum keeps the metric's label cardinality bounded by
// construction.
type PolicyUpdateOutcome uint8

const (
	// PolicyUpdateSuccess is a policy that was validated and published.
	PolicyUpdateSuccess PolicyUpdateOutcome = iota
	// PolicyUpdateFailure is a publish rejected because the policy was
	// invalid or the durable write failed; the previous policy stays
	// active.
	PolicyUpdateFailure
	// PolicyUpdateConflict is a publish rejected by optimistic
	// concurrency (base_version mismatch). Nothing changed.
	PolicyUpdateConflict
)

// String renders the outcome as its metric label spelling.
func (o PolicyUpdateOutcome) String() string { return outcomeNames[o] }

// OverflowCause is a bounded enum of kernel queue-drop causes.
type OverflowCause uint8

const (
	// OverflowKernelDrop is a packet dropped because the queue was full
	// (queue_maxlen exhausted) — the kernel's fail-closed path.
	OverflowKernelDrop OverflowCause = iota
	// OverflowUserDrop is a packet dropped because netlink unicast to
	// the daemon's socket failed (no listener / socket error). The
	// kernel still drops the packet — never fail-open.
	OverflowUserDrop
)

// String renders the cause as its metric label spelling.
func (c OverflowCause) String() string { return overflowNames[c] }

// BackendErrorKind is a bounded enum of backend error kinds. Using a
// typed enum (not a free-form string) keeps metric label cardinality
// bounded by construction.
type BackendErrorKind uint8

const (
	// BackendErrReceive is a netlink receive error in the queue loop.
	BackendErrReceive BackendErrorKind = iota
	// BackendErrSend is a netlink send (verdict) error.
	BackendErrSend
	// BackendErrParse is a packet parse failure.
	BackendErrParse
	// BackendErrPanic is a recovered engine panic.
	BackendErrPanic
)

// String renders the kind as its metric label spelling.
func (k BackendErrorKind) String() string { return backendErrNames[k] }

// Counters returns a race-safe snapshot of the total packets-decided
// counter (all action/protocol label combinations). It exists for tests
// and benchmarks that need an observable counter without parsing the
// Prometheus text form.
func (r *Registry) Counters() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	var total int64
	for a := range r.packetsTotal {
		for p := range r.packetsTotal[a] {
			total += r.packetsTotal[a][p]
		}
	}
	return total
}

// WritePrometheus renders the registry in Prometheus text exposition
// format.
func (r *Registry) WritePrometheus(w io.Writer) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Fixed per-packet verdict counters.
	fmt.Fprintf(w, "# HELP goshisen_packets_total %s\n", "Packets decided, by action and protocol.")
	fmt.Fprintf(w, "# TYPE goshisen_packets_total counter\n")
	for a := range r.packetsTotal {
		for p := range r.packetsTotal[a] {
			if n := r.packetsTotal[a][p]; n != 0 {
				fmt.Fprintf(w, "goshisen_packets_total{action=%q,protocol=%q} %d\n",
					actionNames[a], protocolNames[p], n)
			}
		}
	}
	fmt.Fprintf(w, "# HELP goshisen_packets_by_state_total %s\n", "Packets decided, by state.")
	fmt.Fprintf(w, "# TYPE goshisen_packets_by_state_total counter\n")
	for s := range r.packetsByState {
		if n := r.packetsByState[s]; n != 0 {
			fmt.Fprintf(w, "goshisen_packets_by_state_total{state=%q} %d\n", stateNames[s], n)
		}
	}

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
