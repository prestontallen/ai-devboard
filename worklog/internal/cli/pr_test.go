package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// invokePR drives the `pr` cobra subcommand and captures stdout.
func invokePR(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	prev := flagDir
	flagDir = dir
	t.Cleanup(func() { flagDir = prev })

	cmd := newPrCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestPrCLISetWritesField(t *testing.T) {
	live, _, _ := storeWriteFixture(t)
	out, err := invokePR(t, live, "solo", "https://example.com/pull/42", "--json")
	if err != nil {
		t.Fatalf("invokePR: %v\nout: %s", err, out)
	}
	var res map[string]string
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("json: %v\nout: %s", err, out)
	}
	if res["pr"] != "https://example.com/pull/42" {
		t.Errorf("pr = %q", res["pr"])
	}
	if res["previous"] != "" {
		t.Errorf("previous = %q, want empty", res["previous"])
	}
	data, _ := os.ReadFile(live + "/WORK.md")
	if !strings.Contains(string(data), "  - **PR**: https://example.com/pull/42") {
		t.Errorf("WORK.md missing new PR line:\n%s", string(data))
	}
}

func TestPrCLIClearKeepsLine(t *testing.T) {
	live, _, _ := storeWriteFixture(t)
	if _, err := invokePR(t, live, "solo", "https://example.com/pull/1", "--json"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if _, err := invokePR(t, live, "solo", "--clear", "--json"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	data, _ := os.ReadFile(live + "/WORK.md")
	if !strings.Contains(string(data), "  - **PR**: \n") {
		t.Errorf("expected `  - **PR**: ` (trailing space) line preserved:\n%q", string(data))
	}
}

func TestPrCLINoArgsShowsCurrent(t *testing.T) {
	live, _, _ := storeWriteFixture(t)
	if _, err := invokePR(t, live, "solo", "https://example.com/pull/7", "--json"); err != nil {
		t.Fatalf("set: %v", err)
	}
	out, err := invokePR(t, live, "solo", "--json")
	if err != nil {
		t.Fatalf("read: %v\nout: %s", err, out)
	}
	var res map[string]string
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("json: %v\nout: %s", err, out)
	}
	if res["pr"] != "https://example.com/pull/7" {
		t.Errorf("pr = %q", res["pr"])
	}
}

func TestPrCLIConflictURLAndClear(t *testing.T) {
	dir := t.TempDir()
	_, err := invokePR(t, dir, "solo", "https://example.com", "--clear", "--json")
	if err == nil {
		t.Fatal("expected error from URL + --clear")
	}
	ec, ok := err.(exitCoder)
	if !ok {
		t.Fatalf("expected exitCoder, got %T: %v", err, err)
	}
	if ec.ExitCode() != 64 {
		t.Errorf("exit = %d, want 64", ec.ExitCode())
	}
}

func TestPrCLIConflictURLAndEdit(t *testing.T) {
	dir := t.TempDir()
	_, err := invokePR(t, dir, "solo", "https://example.com", "--edit", "--json")
	if err == nil {
		t.Fatal("expected error from URL + --edit")
	}
	ec, ok := err.(exitCoder)
	if !ok {
		t.Fatalf("expected exitCoder, got %T: %v", err, err)
	}
	if ec.ExitCode() != 64 {
		t.Errorf("exit = %d, want 64", ec.ExitCode())
	}
}

func TestPrCLIConflictClearAndEdit(t *testing.T) {
	dir := t.TempDir()
	_, err := invokePR(t, dir, "solo", "--clear", "--edit", "--json")
	if err == nil {
		t.Fatal("expected error from --clear + --edit")
	}
	ec, ok := err.(exitCoder)
	if !ok {
		t.Fatalf("expected exitCoder, got %T: %v", err, err)
	}
	if ec.ExitCode() != 64 {
		t.Errorf("exit = %d, want 64", ec.ExitCode())
	}
}
