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
	root := t.TempDir()

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
	root := t.TempDir()

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
	root := t.TempDir()
	if _, err := invokeFeedback(t, root, "append",
		"--signal", "tui-error", "--trigger", "board blew up"); err != nil {
		t.Fatal(err)
	}

	entries, err := feedback.Parse(filepath.Join(root, "FEEDBACK.md"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("setup: %v, %d entries", err, len(entries))
	}
	ts := strconv.FormatInt(entries[0].Timestamp, 10)

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
	if entries[0].Resolved == 0 {
		t.Error("entry not marked resolved on disk")
	}
}

func TestFeedbackResolveUnknownExits64(t *testing.T) {
	root := t.TempDir()
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
	root := t.TempDir()
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
	root := t.TempDir()
	if _, err := invokeFeedback(t, root, "append",
		"--signal", "profanity", "--trigger", "x"); err != nil {
		t.Fatal(err)
	}
	entries, err := feedback.Parse(filepath.Join(root, "FEEDBACK.md"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("setup: %v", err)
	}
	ts := strconv.FormatInt(entries[0].Timestamp, 10)

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
	root := t.TempDir()
	for i, sig := range []string{"missing-feature", "tui-error"} {
		if _, err := invokeFeedback(t, root, "append",
			"--signal", sig, "--trigger", "entry "+strconv.Itoa(i)); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := feedback.Parse(filepath.Join(root, "FEEDBACK.md"))
	if err != nil || len(entries) != 2 {
		t.Fatalf("setup: %v, %d entries", err, len(entries))
	}
	// Both entries land in the same unix second, so address the one to
	// resolve by signal rather than by a handle they may share.
	if _, err := invokeFeedback(t, root, "resolve",
		strconv.FormatInt(entries[0].Timestamp, 10)); err != nil {
		t.Fatal(err)
	}

	out, err := invokeFeedback(t, root, "--unresolved", "--json")
	if err != nil {
		t.Fatalf("list: %v\nout: %s", err, out)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("json: %v\nout: %s", err, out)
	}
	if count, _ := result["count"].(float64); count != 1 {
		t.Errorf("count = %v, want 1", result["count"])
	}

	// --unresolved ANDs with --signal rather than replacing it.
	out, err = invokeFeedback(t, root, "--unresolved", "--signal", "missing-feature", "--json")
	if err != nil {
		t.Fatalf("list: %v\nout: %s", err, out)
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("json: %v\nout: %s", err, out)
	}
	if count, _ := result["count"].(float64); count != 0 {
		t.Errorf("count = %v, want 0 (the missing-feature entry is resolved)", result["count"])
	}
}
