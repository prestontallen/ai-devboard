package feedback

import (
	"path/filepath"
	"testing"
	"time"
)

func TestParseRoundTrip(t *testing.T) {
	want := []Entry{
		{Signal: SignalMissingFeature, Trigger: "due dates", Excerpt: "User: add due dates", Context: "triaging", Timestamp: 1000},
		{Signal: SignalTUIError, Trigger: "panic on p key", Excerpt: "panic: runtime error", Context: "TUI test", Timestamp: 2000},
		{Signal: SignalProfanity, Trigger: "swear word", Timestamp: 3000},
	}
	content := "# Worklog Feedback Log\n\n" +
		"## 1000 — missing-feature\n**Trigger**: due dates\n**Excerpt**:\n> User: add due dates\n**Context**: triaging\n\n" +
		"## 2000 — tui-error\n**Trigger**: panic on p key\n**Excerpt**:\n> panic: runtime error\n**Context**: TUI test\n\n" +
		"## 3000 — profanity\n**Trigger**: swear word\n\n"
	got := parseString(t, content)
	if len(got) != len(want) {
		t.Fatalf("len=%d, want %d", len(got), len(want))
	}
	for i, w := range want {
		g := got[i]
		if g.Timestamp != w.Timestamp {
			t.Errorf("[%d] Timestamp %d != %d", i, g.Timestamp, w.Timestamp)
		}
		if g.Signal != w.Signal {
			t.Errorf("[%d] Signal %q != %q", i, g.Signal, w.Signal)
		}
		if g.Trigger != w.Trigger {
			t.Errorf("[%d] Trigger %q != %q", i, g.Trigger, w.Trigger)
		}
		if g.Excerpt != w.Excerpt {
			t.Errorf("[%d] Excerpt %q != %q", i, g.Excerpt, w.Excerpt)
		}
		if g.Context != w.Context {
			t.Errorf("[%d] Context %q != %q", i, g.Context, w.Context)
		}
	}
}

func TestParseMissingFile(t *testing.T) {
	entries, err := Parse(filepath.Join(t.TempDir(), "FEEDBACK.md"))
	if err != nil {
		t.Fatalf("Parse on missing file: %v", err)
	}
	if entries != nil {
		t.Errorf("want nil, got %v", entries)
	}
}

func TestFilterBySignal(t *testing.T) {
	entries := []Entry{
		{Signal: SignalMissingFeature, Trigger: "a", Timestamp: 1},
		{Signal: SignalTUIError, Trigger: "b", Timestamp: 2},
		{Signal: SignalMissingFeature, Trigger: "c", Timestamp: 3},
	}
	got := Filter(entries, SignalTUIError, time.Time{}, false)
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	if got[0].Trigger != "b" {
		t.Errorf("Trigger = %q, want %q", got[0].Trigger, "b")
	}
}

func TestFilterBySince(t *testing.T) {
	entries := []Entry{
		{Signal: SignalMissingFeature, Trigger: "old", Timestamp: 1000},
		{Signal: SignalTUIError, Trigger: "new", Timestamp: 9000},
	}
	since := time.Unix(5000, 0)
	got := Filter(entries, "", since, false)
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	if got[0].Trigger != "new" {
		t.Errorf("Trigger = %q, want %q", got[0].Trigger, "new")
	}
}

func TestFilterUnresolved(t *testing.T) {
	entries := []Entry{
		{Signal: SignalMissingFeature, Trigger: "open", Timestamp: 1},
		{Signal: SignalMissingFeature, Trigger: "done", Timestamp: 2, Resolved: 99},
	}
	got := Filter(entries, "", time.Time{}, true)
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	if got[0].Trigger != "open" {
		t.Errorf("Trigger = %q, want %q", got[0].Trigger, "open")
	}
}

// TestFilterANDsTogether pins that the three filters compose rather than
// the last one winning.
func TestFilterANDsTogether(t *testing.T) {
	entries := []Entry{
		{Signal: SignalTUIError, Trigger: "old open", Timestamp: 1000},
		{Signal: SignalTUIError, Trigger: "new done", Timestamp: 9000, Resolved: 99},
		{Signal: SignalTUIError, Trigger: "new open", Timestamp: 9001},
		{Signal: SignalProfanity, Trigger: "other", Timestamp: 9002},
	}
	got := Filter(entries, SignalTUIError, time.Unix(5000, 0), true)
	if len(got) != 1 {
		t.Fatalf("want 1, got %d: %+v", len(got), got)
	}
	if got[0].Trigger != "new open" {
		t.Errorf("Trigger = %q, want %q", got[0].Trigger, "new open")
	}
}
