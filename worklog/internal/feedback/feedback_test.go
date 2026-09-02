package feedback

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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

func TestResolveMarksEntryAndLeavesRestAlone(t *testing.T) {
	_, path := tmpWorkdir(t)

	for _, e := range []Entry{
		{Timestamp: 1000, Signal: SignalMissingFeature, Trigger: "first", Excerpt: "a\nb", Context: "ctx"},
		{Timestamp: 2000, Signal: SignalTUIError, Trigger: "second"},
	} {
		if _, err := Append(path, e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	at, already, err := Resolve(path, 1000)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if already {
		t.Error("already = true on a fresh entry")
	}
	if at == 0 {
		t.Error("Resolve returned a zero timestamp")
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// The only difference is one inserted line, at the end of entry 1000's
	// block — everything else is byte-identical.
	want := strings.Replace(string(before), "**Context**: ctx\n",
		"**Context**: ctx\n"+resolvedPrefix+strconv.FormatInt(at, 10)+"\n", 1)
	if string(after) != want {
		t.Errorf("file changed beyond the Resolved line.\n got: %q\nwant: %q", after, want)
	}

	entries, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}
	if entries[0].Resolved != at {
		t.Errorf("entry 1000 Resolved = %d, want %d", entries[0].Resolved, at)
	}
	if entries[1].Resolved != 0 {
		t.Errorf("entry 2000 Resolved = %d, want 0", entries[1].Resolved)
	}
}

func TestResolveUnknownTimestamp(t *testing.T) {
	_, path := tmpWorkdir(t)
	if _, err := Append(path, Entry{Timestamp: 1000, Signal: SignalProfanity, Trigger: "x"}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := Resolve(path, 4242); !errors.Is(err, ErrEntryNotFound) {
		t.Fatalf("err = %v, want ErrEntryNotFound", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Error("file was rewritten on a failed resolve")
	}
}

func TestResolveMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "FEEDBACK.md")
	if _, _, err := Resolve(path, 1); !errors.Is(err, ErrEntryNotFound) {
		t.Fatalf("err = %v, want ErrEntryNotFound", err)
	}
}

func TestResolveIdempotent(t *testing.T) {
	_, path := tmpWorkdir(t)
	if _, err := Append(path, Entry{Timestamp: 1000, Signal: SignalTUIError, Trigger: "x"}); err != nil {
		t.Fatal(err)
	}

	first, _, err := Resolve(path, 1000)
	if err != nil {
		t.Fatalf("first Resolve: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	at, already, err := Resolve(path, 1000)
	if err != nil {
		t.Fatalf("second Resolve: %v", err)
	}
	if !already {
		t.Error("already = false on a second resolve")
	}
	if at != first {
		t.Errorf("resolved at = %d, want the original %d", at, first)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Error("re-resolving rewrote the file")
	}
}

// TestResolveRoundTrip checks a resolved entry survives serialize→Parse with
// every field intact — the format is the contract between the Go writer and
// the devboard's Python reader.
func TestResolveRoundTrip(t *testing.T) {
	_, path := tmpWorkdir(t)
	want := Entry{
		Timestamp: 1000,
		Signal:    SignalAgentFrustration,
		Trigger:   "trigger line",
		Excerpt:   "line one\nline two",
		Context:   "context line",
	}
	if _, err := Append(path, want); err != nil {
		t.Fatal(err)
	}
	at, _, err := Resolve(path, 1000)
	if err != nil {
		t.Fatal(err)
	}
	want.Resolved = at

	entries, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	if entries[0] != want {
		t.Errorf("round trip lost data.\n got: %+v\nwant: %+v", entries[0], want)
	}
}

// TestResolveFirstMatchOnDuplicateTimestamps documents the collision case:
// entries are addressed by unix second, so two appended within the same
// second share a handle and resolve targets the earlier one.
func TestResolveFirstMatchOnDuplicateTimestamps(t *testing.T) {
	_, path := tmpWorkdir(t)
	for _, trig := range []string{"first", "second"} {
		if _, err := Append(path, Entry{Timestamp: 1000, Signal: SignalTUIError, Trigger: trig}); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := Resolve(path, 1000); err != nil {
		t.Fatal(err)
	}
	entries, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}
	if entries[0].Resolved == 0 {
		t.Error("first entry not resolved")
	}
	if entries[1].Resolved != 0 {
		t.Error("second entry resolved too — resolve should stop at the first match")
	}
}

// TestConcurrentWritesLoseNothing is the reason the write path is locked:
// Append and Resolve are both read-modify-write, so without the flock the
// slower writer overwrites the faster one's entry.
func TestConcurrentWritesLoseNothing(t *testing.T) {
	_, path := tmpWorkdir(t)

	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, n*2)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := Append(path, Entry{
				Timestamp: int64(1000 + i),
				Signal:    SignalMissingFeature,
				Trigger:   fmt.Sprintf("entry %d", i),
			})
			errs <- err
		}(i)
	}
	wg.Wait()

	// Now hammer the same file with resolves interleaved with more appends.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				_, _, err := Resolve(path, int64(1000+i))
				errs <- err
				return
			}
			_, err := Append(path, Entry{
				Timestamp: int64(5000 + i),
				Signal:    SignalTUIError,
				Trigger:   fmt.Sprintf("late %d", i),
			})
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent write: %v", err)
		}
	}

	entries, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != n+n/2 {
		t.Fatalf("want %d entries, got %d — a concurrent write was lost", n+n/2, len(entries))
	}

	seen := make(map[int64]Entry, len(entries))
	for _, e := range entries {
		seen[e.Timestamp] = e
	}
	for i := 0; i < n; i++ {
		e, ok := seen[int64(1000+i)]
		if !ok {
			t.Errorf("entry %d missing", 1000+i)
			continue
		}
		if i%2 == 0 && e.Resolved == 0 {
			t.Errorf("entry %d should be resolved", 1000+i)
		}
		if i%2 == 1 && e.Resolved != 0 {
			t.Errorf("entry %d should be unresolved", 1000+i)
		}
	}
}
