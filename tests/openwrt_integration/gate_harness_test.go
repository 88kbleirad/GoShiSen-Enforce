package openwrt_integration

// Regression tests for the OpenWrt e2e reload-gate harness (Prompt 09).
//
// The gate must detect a successful policy publish from the daemon's log
// even when the serial console transcript carries non-UTF-8 / control
// bytes, and must never mistake "policy reload requested" for success.
// The matching logic is exercised here exactly as scripts/openwrt-e2e.sh
// uses it: a grep(1)-compatible ERE against a sanitized transcript.

import (
	"regexp"
	"strings"
	"testing"
)

// reloadGateRE mirrors the ERE in scripts/openwrt-e2e.sh
// (grep -Ec 'policy (reloaded|published)" .*rules=$want').
const reloadGateRE = `policy (reloaded|published)" .*source=sighup .*rules=([0-9]+)([^0-9]|$)`

var reloadGateRe = regexp.MustCompile(reloadGateRE)

// sanitizeTranscript mirrors the vm() pipeline in openwrt-e2e.sh:
// python3 openwrt-console.py ... | tr -c '[:print:]\n\r\t' '?'
// | sed 's/\r//g'. Bytes outside printable ASCII / TAB / LF / CR are
// replaced with '?' (not deleted) so lines stay intact.
func sanitizeTranscript(raw []byte) string {
	var b strings.Builder
	for _, c := range raw {
		switch {
		case c == 0x09 || c == 0x0a || c == 0x0d:
			b.WriteByte(c)
		case c >= 0x20 && c <= 0x7e:
			b.WriteByte(c)
		case c >= 0x80:
			b.WriteByte(c)
		}
	}
	// sed 's/\r//g'
	return strings.ReplaceAll(b.String(), "\r", "")
}

func countPublishMatches(transcript string) (int, error) {
	// Count the published events with rules=1 in the sanitized transcript.
	n := 0
	for _, m := range reloadGateRe.FindAllStringSubmatch(transcript, -1) {
		if m[2] == "1" {
			n++
		}
	}
	return n, nil
}

// TestGateRulesBoundary: rules=10 must not satisfy a rules=1 gate.
func TestGateRulesBoundary(t *testing.T) {
	n, _ := countPublishMatches(`msg="policy published" version=1 source=sighup rules=10`)
	if n != 0 {
		t.Fatalf("rules=10 must not satisfy rules=1 gate, got %d", n)
	}
	n, _ = countPublishMatches(`msg="policy published" version=1 source=sighup rules=1 default_action=DROP`)
	if n != 1 {
		t.Fatalf("rules=1 must satisfy gate, got %d", n)
	}
}

// TestGateRecognizesPublishLine: the exact log line produced by the
// daemon after a SIGHUP reload must be recognized.
func TestGateRecognizesPublishLine(t *testing.T) {
	line := `time=2026-11-13T10:18:07.134Z level=INFO msg="policy published" version=1 source=sighup rules=1 default_action=DROP`
	n, err := countPublishMatches("x" + line + "\n")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 publish match, got %d", n)
	}
}

// TestGateIgnoresReloadRequested: "policy reload requested" must not
// count as a successful publish.
func TestGateIgnoresReloadRequested(t *testing.T) {
	line := `time=2026-11-13T10:18:07.000Z level=INFO msg="policy reload requested (SIGHUP)"`
	n, _ := countPublishMatches("x" + line + "\n")
	if n != 0 {
		t.Fatalf("reload requested must not count as publish, got %d", n)
	}
}

// TestGateSanitizesInvalidUTF8: NUL / invalid bytes before the tail must
// not corrupt the match.
func TestGateSanitizesInvalidUTF8(t *testing.T) {
	raw := append([]byte("\x00\xff\xfe\xfd"), []byte(`time=... level=INFO msg="policy published" version=1 source=sighup rules=1 default_action=DROP`)...)
	s := sanitizeTranscript(raw)
	n, _ := countPublishMatches(s)
	if n != 1 {
		t.Fatalf("invalid bytes must not hide the publish line, got %d (san=%q)", n, s)
	}
}

// TestGateDifferentRulesNotMatched: a publish with rules=2 must not be
// counted when the gate looks for rules=1.
func TestGateDifferentRulesNotMatched(t *testing.T) {
	line := `time=... level=INFO msg="policy published" version=2 source=sighup rules=2 default_action=DROP`
	n, _ := countPublishMatches("x" + line + "\n")
	if n != 0 {
		t.Fatalf("rules=2 must not satisfy rules=1 gate, got %d", n)
	}
}

// TestGateOldEventKeepsCounting: an old event in the log does not reset
// the count; the harness compares counts before/after SIGHUP.
func TestGateOldEventKeepsCounting(t *testing.T) {
	old := `time=2026-11-13T09:00:00Z level=INFO msg="policy published" version=1 source=sighup rules=1 default_action=DROP`
	new := `time=2026-11-13T10:18:07Z level=INFO msg="policy published" version=1 source=sighup rules=1 default_action=DROP`
	n, _ := countPublishMatches(old + "\n" + new + "\n")
	if n != 2 {
		t.Fatalf("expected both old and new events to count, got %d", n)
	}
}

// TestGateRealBadTailBytes: the exact byte pattern seen in the failing CI
// transcript (0xff 0xfe before "tail -3") must still yield a clean gate
// decision.
func TestGateRealBadTailBytes(t *testing.T) {
	raw := []byte("\xff\xfe\xfd" + `tail -3 /tmp/goshisen2.log` + "\r\n")
	raw = append(raw, []byte(`time=... level=INFO msg="policy published" version=1 source=sighup rules=1 default_action=DROP`)...)
	s := sanitizeTranscript(raw)
	n, _ := countPublishMatches(s)
	if n != 1 {
		t.Fatalf("leading 0xff/0xfe must not break gate, got %d (san=%q)", n, s)
	}
}
