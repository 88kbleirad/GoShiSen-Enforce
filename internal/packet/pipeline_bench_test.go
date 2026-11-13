package packet

import (
	"encoding/binary"
	"sort"
	"testing"
	"time"

	"github.com/88kbleirad/GoShiSen-Enforce/internal/metrics"
	"github.com/88kbleirad/GoShiSen-Enforce/internal/policy"
)

// synthMsg frames one queued packet the way the kernel does:
// nfgenmsg(4) + NFQA_PACKET_HDR + NFQA_PAYLOAD + optional NFQA_CT_INFO +
// optional NFQA_CT. It reuses the attribute helpers from capture_test.go.
func synthMsg(id uint32, payload []byte, ctInfo byte, ctPresent, withCT bool) []byte {
	hdr := make([]byte, 8)
	binary.BigEndian.PutUint32(hdr[0:], id)
	out := []byte{2, 0, 0, 0} // nfgenmsg: family=AF_INET
	out = append(out, attr(nfqaPacketHdr, hdr)...)
	out = append(out, attr(nfqaPayload, payload)...)
	if ctPresent {
		ci := make([]byte, 4)
		binary.BigEndian.PutUint32(ci, uint32(ctInfo))
		out = append(out, attr(nfqaCTInfo, ci)...)
	}
	if withCT {
		out = append(out, attr(nfqaCT, synthCTAttr(5))...)
	}
	return out
}

// newPipelineBackend builds the per-packet backend the benchmarks reuse.
// In production the daemon creates one Backend and reuses it for every
// packet (internal/app/app.go), so the benchmarks do the same — otherwise
// the benchmark's own per-call allocation would be attributed to the
// measured path.
func newPipelineBackend(king *policy.King, reg *metrics.Registry) *Backend {
	b := &Backend{}
	b.decide = benchDecider{king: king, reg: reg}.decide
	return b
}

// processOne runs the exact per-packet pipeline the daemon executes
// (internal/app/app.go decider): evaluate → parse → applyCTInfo →
// king.Process → metrics. It returns the verdict and the registry's
// accumulated verdict count so the result is observable and cannot be
// dead-code eliminated.
func processOne(b *Backend, msg []byte, reg *metrics.Registry) (int, int64) {
	_, v := b.evaluate(packetAttrs(msg))
	return v, reg.Counters()
}

// benchDecider replicates the daemon's decider closure (internal/app/app.go).
type benchDecider struct {
	king *policy.King
	reg  *metrics.Registry
}

func (d benchDecider) decide(p policy.Packet) policy.Verdict {
	start := time.Now()
	v := d.king.Process(p)
	stateIdx := metrics.StateUnknownIdx
	switch p.State {
	case policy.StateNew:
		stateIdx = metrics.StateNewIdx
	case policy.StateEstablished:
		stateIdx = metrics.StateEstablishedIdx
	case policy.StateRelated:
		stateIdx = metrics.StateRelatedIdx
	case policy.StateInvalid:
		stateIdx = metrics.StateInvalidIdx
	}
	actionIdx := metrics.ActionDropIdx
	if v.Action == policy.ActionAccept {
		actionIdx = metrics.ActionAcceptIdx
	}
	protoIdx := metrics.ProtoAnyIdx
	switch p.Protocol {
	case policy.ProtoTCP:
		protoIdx = metrics.ProtoTCPIdx
	case policy.ProtoUDP:
		protoIdx = metrics.ProtoUDPIdx
	}
	d.reg.ObservePacket(metrics.VerdictLabels{
		ActionIdx: actionIdx, ProtocolIdx: protoIdx, StateIdx: stateIdx,
	}, time.Since(start))
	return v
}

func benchPolicy() *policy.Snapshot {
	rules := []policy.Rule{
		{ID: "drop-invalid", Action: policy.ActionDrop, StateMask: mask(policy.StateInvalid)},
		{ID: "allow-established", Action: policy.ActionAccept, StateMask: mask(policy.StateEstablished, policy.StateRelated)},
		{ID: "allow-http", Action: policy.ActionAccept, Protocol: policy.ProtoTCP,
			DstPort: policy.PortRange{Lo: 80, Hi: 80}, HasDstPort: true, StateMask: mask(policy.StateNew)},
		{ID: "allow-https", Action: policy.ActionAccept, Protocol: policy.ProtoTCP,
			DstPort: policy.PortRange{Lo: 443, Hi: 443}, HasDstPort: true, StateMask: mask(policy.StateNew)},
		{ID: "allow-dns", Action: policy.ActionAccept, Protocol: policy.ProtoUDP,
			DstPort: policy.PortRange{Lo: 53, Hi: 53}, HasDstPort: true, StateMask: mask(policy.StateNew)},
	}
	return policy.NewSnapshot(policy.ActionDrop, rules)
}

func mask(states ...policy.State) uint16 {
	var m uint16
	for _, s := range states {
		m |= 1 << uint(s)
	}
	return m
}

// TestPipelineRuns verifies the end-to-end pipeline benchmark path really
// exercises the decider and increments metrics. It guards the synthetic
// fixtures (attribute framing, conntrack info) against silently
// regressing to a dead path, which would render the pipeline benchmarks
// meaningless.
func TestPipelineRuns(t *testing.T) {
	king := policy.NewKing()
	king.Replace(benchPolicy())
	reg := metrics.New()
	be := newPipelineBackend(king, reg)
	msg := synthMsg(1, buildIPv4(0, 6, "10.0.0.1", "10.0.0.2", 0, 0, 5, tcp4(12345, 80, 0x02)), ipCTNew, true, true)
	for i := 0; i < 10; i++ {
		v, n := processOne(be, msg, reg)
		if v != nfAccept {
			t.Fatalf("iter %d: verdict=%d, want accept", i, v)
		}
		if n != int64(i+1) {
			t.Fatalf("iter %d: counters=%d, want %d", i, n, i+1)
		}
	}
}

func BenchmarkPipelineNewAccept(b *testing.B) {
	king := policy.NewKing()
	king.Replace(benchPolicy())
	reg := metrics.New()
	be := newPipelineBackend(king, reg)
	msg := synthMsg(1, buildIPv4(0, 6, "10.0.0.1", "10.0.0.2", 0, 0, 5, tcp4(12345, 80, 0x02)), ipCTNew, true, true)
	var sink int64
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, n := processOne(be, msg, reg)
		sink += n
	}
	b.ReportMetric(float64(sink), "verdicts")
}

func BenchmarkPipelineEstablishedReply(b *testing.B) {
	king := policy.NewKing()
	king.Replace(benchPolicy())
	reg := metrics.New()
	orig := synthMsg(1, buildIPv4(0, 6, "10.0.0.1", "10.0.0.2", 0, 0, 5, tcp4(12345, 80, 0x02)), ipCTNew, true, true)
	be := newPipelineBackend(king, reg)
	processOne(be, orig, reg) // prime accepted set
	msg := synthMsg(2, buildIPv4(0, 6, "10.0.0.2", "10.0.0.1", 0, 0, 5, tcp4(80, 12345, 0x10)), ipCTEstablished+ipCTIsReply, true, true)
	var sink int64
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, n := processOne(be, msg, reg)
		sink += n
	}
	b.ReportMetric(float64(sink), "verdicts")
}

func BenchmarkPipelineNewDrop(b *testing.B) {
	king := policy.NewKing()
	king.Replace(benchPolicy())
	reg := metrics.New()
	be := newPipelineBackend(king, reg)
	msg := synthMsg(1, buildIPv4(0, 6, "10.0.0.1", "10.0.0.2", 0, 0, 5, tcp4(12345, 9999, 0x02)), ipCTNew, true, true)
	var sink int64
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, n := processOne(be, msg, reg)
		sink += n
	}
	b.ReportMetric(float64(sink), "verdicts")
}

func BenchmarkPipelineMetricsOnly(b *testing.B) {
	reg := metrics.New()
	labels := metrics.VerdictLabels{ActionIdx: metrics.ActionAcceptIdx, ProtocolIdx: metrics.ProtoTCPIdx, StateIdx: metrics.StateNewIdx}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		reg.ObservePacket(labels, time.Microsecond)
	}
}

// BenchmarkPipelineLatencyPercentiles records per-packet end-to-end (in-
// process) pipeline latency and reports p50/p95/p99. It uses the same
// pipeline as BenchmarkPipelineNewAccept. Durations are collected into a
// preallocated slice outside the timed loop and sorted after timing
// stops, so the measurement itself is allocation-free.
func BenchmarkPipelineLatencyPercentiles(b *testing.B) {
	king := policy.NewKing()
	king.Replace(benchPolicy())
	reg := metrics.New()
	be := newPipelineBackend(king, reg)
	msg := synthMsg(1, buildIPv4(0, 6, "10.0.0.1", "10.0.0.2", 0, 0, 5, tcp4(12345, 80, 0x02)), ipCTNew, true, true)
	durs := make([]time.Duration, 0, 200000)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		start := time.Now()
		processOne(be, msg, reg)
		durs = append(durs, time.Since(start))
	}
	b.StopTimer()
	sort.Slice(durs, func(i, j int) bool { return durs[i] < durs[j] })
	if len(durs) == 0 {
		b.Fatal("no samples")
	}
	pct := func(q int) float64 {
		idx := len(durs) * q / 100
		return durs[idx].Seconds() * 1e6
	}
	b.ReportMetric(pct(50), "p50-us")
	b.ReportMetric(pct(95), "p95-us")
	b.ReportMetric(pct(99), "p99-us")
}
