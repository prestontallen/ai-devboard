package feedback

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
)

func tmpWorkdir(t *testing.T) (model.Workdir, string) {
	t.Helper()
	root := t.TempDir()
	wd, err := model.NewWorkdir(root)
	if err != nil {
		t.Fatal(err)
	}
	return wd, wd.FeedbackMD()
}

func TestAppendCreatesFile(t *testing.T) {
	_, path := tmpWorkdir(t)

	e := Entry{Signal: SignalMissingFeature, Trigger: "User asked for due dates"}
	got, err := Append(path, e)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if got.Timestamp == 0 {
		t.Error("Append did not set Timestamp")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "# Worklog Feedback Log") {
		t.Error("missing header")
	}
	if !strings.Contains(s, "missing-feature") {
		t.Error("missing signal in file")
	}
	if !strings.Contains(s, "**Trigger**: User asked for due dates") {
		t.Error("missing trigger in file")
	}
}

func TestAppendIdempotency(t *testing.T) {
	_, path := tmpWorkdir(t)

	e1 := Entry{Signal: SignalMissingFeature, Trigger: "first", Timestamp: 1000}
	e2 := Entry{Signal: SignalTUIError, Trigger: "second", Timestamp: 2000}

	if _, err := Append(path, e1); err != nil {
		t.Fatalf("first Append: %v", err)
	}
	if _, err := Append(path, e2); err != nil {
		t.Fatalf("second Append: %v", err)
	}

	entries, err := Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}
	if entries[0].Trigger != "first" {
		t.Errorf("entry[0].Trigger = %q, want %q", entries[0].Trigger, "first")
	}
	if entries[1].Trigger != "second" {
		t.Errorf("entry[1].Trigger = %q, want %q", entries[1].Trigger, "second")
	}
}

func TestParseRoundTrip(t *testing.T) {
	_, path := tmpWorkdir(t)

	want := []Entry{
		{Signal: SignalMissingFeature, Trigger: "due dates", Excerpt: "User: add due dates", Context: "triaging", Timestamp: 1000},
		{Signal: SignalTUIError, Trigger: "panic on p key", Excerpt: "panic: runtime error", Context: "TUI test", Timestamp: 2000},
		{Signal: SignalProfanity, Trigger: "swear word", Timestamp: 3000},
	}
	for _, e := range want {
		if _, err := Append(path, e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	got, err := Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
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
	got := Filter(entries, SignalTUIError, time.Time{})
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
	got := Filter(entries, "", since)
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	if got[0].Trigger != "new" {
		t.Errorf("Trigger = %q, want %q", got[0].Trigger, "new")
	}
}
