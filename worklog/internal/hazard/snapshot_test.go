package hazard

import (
	"os"
	"testing"
)

// TestScanSnapshot runs the detectors over a real corpus named by
// WORKLOG_SNAPSHOT (and optionally DEVBOARD_SNAPSHOT).
func TestScanSnapshot(t *testing.T) {
	live := os.Getenv("WORKLOG_SNAPSHOT")
	if live == "" {
		t.Skip("set WORKLOG_SNAPSHOT to scan a real corpus")
	}
	found, err := Scan(live, os.Getenv("DEVBOARD_SNAPSHOT"))
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	byConstruct := map[string]int{}
	for _, f := range found {
		byConstruct[f.Construct]++
	}
	t.Logf("findings=%d by-construct=%v", len(found), byConstruct)
	for i, f := range found {
		if i == 40 {
			t.Logf("... %d more", len(found)-40)
			break
		}
		t.Logf("%s", f)
	}
}
