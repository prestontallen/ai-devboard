package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
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
