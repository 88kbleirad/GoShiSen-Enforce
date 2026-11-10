package packet

import "testing"

const sampleQueueStats = ` 0.0 00000001 00000002 00000000 2 65535 0 0 0 0 0 0
  0.0 00000001 00000002 00000000 2 65535 0 0 0 0 0 0
  7   12345   3   2   65535   0   0   0   1
  0   12345   2   2   65535   12   3   4567   1
`

func TestParseQueueStatsFound(t *testing.T) {
	st, found, err := ParseQueueStats(sampleQueueStats, 0)
	if err != nil {
		t.Fatalf("ParseQueueStats: %v", err)
	}
	if !found {
		t.Fatal("queue 0 not found")
	}
	if st.QueueNum != 0 {
		t.Errorf("QueueNum = %d, want 0", st.QueueNum)
	}
	if st.Total != 2 {
		t.Errorf("Total = %d, want 2", st.Total)
	}
	if st.Dropped != 12 {
		t.Errorf("Dropped = %d, want 12", st.Dropped)
	}
	if st.UserDropped != 3 {
		t.Errorf("UserDropped = %d, want 3", st.UserDropped)
	}
	if st.Sequence != 4567 {
		t.Errorf("Sequence = %d, want 4567", st.Sequence)
	}
}

func TestParseQueueStatsMissing(t *testing.T) {
	_, found, err := ParseQueueStats(sampleQueueStats, 99)
	if err == nil {
		t.Fatal("ParseQueueStats for missing queue: want error")
	}
	if found {
		t.Fatal("found queue 99, want not found")
	}
}

func TestParseQueueStatsEmpty(t *testing.T) {
	_, _, err := ParseQueueStats("", 0)
	if err == nil {
		t.Fatal("ParseQueueStats empty: want error")
	}
}

func TestParseLineRejectsNonNumeric(t *testing.T) {
	for _, line := range []string{
		"header line",
		"  0   12345   3   2   65535   nope   0   0   1",
		"  0   12345   3",
		"",
	} {
		if _, ok := ParseLine(line); ok {
			t.Errorf("ParseLine(%q) = ok, want not ok", line)
		}
	}
}
