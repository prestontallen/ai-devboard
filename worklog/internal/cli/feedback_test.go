package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/feedback"
)

// entryByTrigger finds the entry with the given trigger text, failing the
// test if it's missing or ambiguous — storeWriteFixture's shared corpus
// carries its own pre-existing feedback entries, so a test appending one
// new entry can no longer assume it lands alone or at index 0.
func entryByTrigger(t *testing.T, entries []feedback.Entry, trigger string) feedback.Entry {
	t.Helper()
	var found []feedback.Entry
	for _, e := range entries {
		if e.Trigger == trigger {
			found = append(found, e)
		}
	}
	if len(found) != 1 {
		t.Fatalf("trigger %q: found %d entries, want 1 (all: %+v)", trigger, len(found), entries)
	}
	return found[0]
}

// invokeFeedback drives the feedback cobra subcommand and captures stdout.
func invokeFeedback(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	prev := flagDir
	flagDir = dir
	t.Cleanup(func() { flagDir = prev })

	cmd := newFeedbackCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestFeedbackAppendBasic(t *testing.T) {
	root, _, _ := storeWriteFixture(t)

	out, err := invokeFeedback(t, root, "append",
		"--signal", "missing-feature",
		"--trigger", "User asked for due dates",
		"--json",
	)
	if err != nil {
		t.Fatalf("append: %v\nout: %s", err, out)
	}

	var e feedback.Entry
	if err := json.Unmarshal([]byte(out), &e); err != nil {
		t.Fatalf("json: %v\nout: %s", err, out)
	}
	if e.Signal != feedback.SignalMissingFeature {
		t.Errorf("signal = %q, want %q", e.Signal, feedback.SignalMissingFeature)
	}
	if e.Trigger != "User asked for due dates" {
		t.Errorf("trigger = %q", e.Trigger)
	}
	if e.Timestamp == 0 {
		t.Error("timestamp not set")
	}

	data, _ := os.ReadFile(filepath.Join(root, "FEEDBACK.md"))
	if !strings.Contains(string(data), "missing-feature") {
		t.Error("FEEDBACK.md missing signal")
	}
}

func TestFeedbackAppendBadSignal(t *testing.T) {
	root := t.TempDir()
	_, err := invokeFeedback(t, root, "append",
		"--signal", "bogus",
		"--trigger", "x",
		"--json",
	)
	if err == nil {
		t.Fatal("expected error for bad signal")
	}
	ec, ok := err.(exitCoder)
	if !ok {
		t.Fatalf("expected exitCoder, got %T: %v", err, err)
	}
	if ec.ExitCode() != 64 {
		t.Errorf("exit = %d, want 64", ec.ExitCode())
	}
}

func TestFeedbackAppendEmptyTrigger(t *testing.T) {
	root := t.TempDir()
	_, err := invokeFeedback(t, root, "append",
		"--signal", "tui-error",
		"--trigger", "",
		"--json",
	)
	if err == nil {
		t.Fatal("expected error for empty trigger")
	}
	ec, ok := err.(exitCoder)
	if !ok {
		t.Fatalf("expected exitCoder, got %T: %v", err, err)
	}
	if ec.ExitCode() != 64 {
		t.Errorf("exit = %d, want 64", ec.ExitCode())
	}
}

func TestFeedbackListEmpty(t *testing.T) {
	root := t.TempDir()
	out, err := invokeFeedback(t, root, "--json")
	if err != nil {
		t.Fatalf("list: %v\nout: %s", err, out)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("json: %v\nout: %s", err, out)
	}
	count, ok := result["count"].(float64)
	if !ok || count != 0 {
		t.Errorf("count = %v, want 0", result["count"])
	}
	entries, ok := result["entries"].([]any)
	if !ok {
		t.Fatalf("entries field missing or wrong type")
	}
	if len(entries) != 0 {
		t.Errorf("entries len = %d, want 0", len(entries))
	}
}

func TestFeedbackListFilter(t *testing.T) {
	root, _, _ := storeWriteFixture(t)

	for _, sig := range []string{"missing-feature", "profanity", "tui-error"} {
		if _, err := invokeFeedback(t, root, "append",
			"--signal", sig,
			"--trigger", "entry for "+sig,
		); err != nil {
			t.Fatalf("append %s: %v", sig, err)
		}
	}

	out, err := invokeFeedback(t, root, "--signal", "profanity", "--json")
	if err != nil {
		t.Fatalf("list filter: %v\nout: %s", err, out)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("json: %v\nout: %s", err, out)
	}
	count, _ := result["count"].(float64)
	if count != 1 {
		t.Errorf("count = %v, want 1", result["count"])
	}
}

// exitCode digs the process exit code out of a subcommand error.
func exitCode(t *testing.T, err error) int {
	t.Helper()
	var ec exitCoder
	if !errors.As(err, &ec) {
		t.Fatalf("error %v does not carry an exit code", err)
	}
	return ec.ExitCode()
}

func TestFeedbackResolve(t *testing.T) {
	root, _, _ := storeWriteFixture(t)
	if _, err := invokeFeedback(t, root, "append",
		"--signal", "tui-error", "--trigger", "board blew up"); err != nil {
		t.Fatal(err)
	}

	entries, err := feedback.Parse(filepath.Join(root, "FEEDBACK.md"))
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	entry := entryByTrigger(t, entries, "board blew up")
	ts := strconv.FormatInt(entry.Timestamp, 10)

	out, err := invokeFeedback(t, root, "resolve", ts, "--json")
	if err != nil {
		t.Fatalf("resolve: %v\nout: %s", err, out)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("json: %v\nout: %s", err, out)
	}
	if already, _ := result["already"].(bool); already {
		t.Error("already = true on a fresh resolve")
	}
	if resolved, _ := result["resolved"].(float64); resolved == 0 {
		t.Errorf("resolved = %v, want a timestamp", result["resolved"])
	}

	entries, err = feedback.Parse(filepath.Join(root, "FEEDBACK.md"))
	if err != nil {
		t.Fatal(err)
	}
	if entryByTrigger(t, entries, "board blew up").Resolved == 0 {
		t.Error("entry not marked resolved on disk")
	}
}

func TestFeedbackResolveUnknownExits64(t *testing.T) {
	root, _, _ := storeWriteFixture(t)
	if _, err := invokeFeedback(t, root, "append",
		"--signal", "tui-error", "--trigger", "x"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, "FEEDBACK.md"))
	if err != nil {
		t.Fatal(err)
	}

	out, err := invokeFeedback(t, root, "resolve", "4242")
	if err == nil {
		t.Fatalf("want an error, got none\nout: %s", out)
	}
	if got := exitCode(t, err); got != 64 {
		t.Errorf("exit code = %d, want 64", got)
	}
	if !strings.Contains(err.Error(), "4242") {
		t.Errorf("error %q does not name the bad handle", err.Error())
	}

	after, err := os.ReadFile(filepath.Join(root, "FEEDBACK.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Error("file was rewritten on a failed resolve")
	}
}

func TestFeedbackResolveNonNumericExits64(t *testing.T) {
	root, _, _ := storeWriteFixture(t)
	out, err := invokeFeedback(t, root, "resolve", "3")
	if err == nil {
		t.Fatalf("want an error, got none\nout: %s", out)
	}
	if got := exitCode(t, err); got != 64 {
		t.Errorf("exit code = %d, want 64", got)
	}

	out, err = invokeFeedback(t, root, "resolve", "not-a-stamp")
	if err == nil {
		t.Fatalf("want an error, got none\nout: %s", out)
	}
	if got := exitCode(t, err); got != 64 {
		t.Errorf("exit code = %d, want 64", got)
	}
}

func TestFeedbackResolveIdempotent(t *testing.T) {
	root, _, _ := storeWriteFixture(t)
	if _, err := invokeFeedback(t, root, "append",
		"--signal", "profanity", "--trigger", "x"); err != nil {
		t.Fatal(err)
	}
	entries, err := feedback.Parse(filepath.Join(root, "FEEDBACK.md"))
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	ts := strconv.FormatInt(entryByTrigger(t, entries, "x").Timestamp, 10)

	if _, err := invokeFeedback(t, root, "resolve", ts); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, "FEEDBACK.md"))
	if err != nil {
		t.Fatal(err)
	}

	out, err := invokeFeedback(t, root, "resolve", ts)
	if err != nil {
		t.Fatalf("second resolve should succeed: %v\nout: %s", err, out)
	}
	if !strings.Contains(out, "already resolved") {
		t.Errorf("out = %q, want an already-resolved notice", out)
	}

	after, err := os.ReadFile(filepath.Join(root, "FEEDBACK.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Error("re-resolving rewrote the file")
	}
}

func TestFeedbackListUnresolved(t *testing.T) {
	root, _, _ := storeWriteFixture(t)

	// storeWriteFixture's shared corpus carries its own feedback entries,
	// so every assertion below is a delta against a measured baseline
	// rather than an absolute count.
	countUnresolved := func(extraArgs ...string) float64 {
		t.Helper()
		out, err := invokeFeedback(t, root, append([]string{"--unresolved", "--json"}, extraArgs...)...)
		if err != nil {
			t.Fatalf("list: %v\nout: %s", err, out)
		}
		var result map[string]any
		if err := json.Unmarshal([]byte(out), &result); err != nil {
			t.Fatalf("json: %v\nout: %s", err, out)
		}
		count, _ := result["count"].(float64)
		return count
	}
	baseUnresolved := countUnresolved()
	baseUnresolvedMissingFeature := countUnresolved("--signal", "missing-feature")

	for i, sig := range []string{"missing-feature", "tui-error"} {
		if _, err := invokeFeedback(t, root, "append",
			"--signal", sig, "--trigger", "entry "+strconv.Itoa(i)); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := feedback.Parse(filepath.Join(root, "FEEDBACK.md"))
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	ts := entryByTrigger(t, entries, "entry 0").Timestamp
	if _, err := invokeFeedback(t, root, "resolve", strconv.FormatInt(ts, 10)); err != nil {
		t.Fatal(err)
	}

	// Two new entries appended, one resolved: net +1 unresolved overall.
	if got, want := countUnresolved(), baseUnresolved+1; got != want {
		t.Errorf("unresolved count = %v, want %v", got, want)
	}

	// --unresolved ANDs with --signal rather than replacing it: the one
	// new missing-feature entry ("entry 0") is now resolved, so that
	// filtered count is back at its baseline.
	if got, want := countUnresolved("--signal", "missing-feature"), baseUnresolvedMissingFeature; got != want {
		t.Errorf("unresolved missing-feature count = %v, want %v (entry 0 is resolved)", got, want)
	}
}
