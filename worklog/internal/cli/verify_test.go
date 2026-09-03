package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// invokeVerify drives the verify cobra subcommand, mirroring invokeMigrate's
// conventions.
func invokeVerify(t *testing.T, worklogDir string, args ...string) (string, error) {
	t.Helper()
	prev := flagDir
	flagDir = worklogDir
	t.Cleanup(func() { flagDir = prev })

	cmd := newVerifyCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

// TestVerifyJSONSingleDocument is criterion 10.
func TestVerifyJSONSingleDocument(t *testing.T) {
	worklogDir := newCLIFixtureDir(t)
	t.Setenv("DEVBOARD_DATA", t.TempDir())

	out, err := invokeVerify(t, worklogDir, "--json")
	if err != nil {
		t.Fatalf("expected a clean run, got error: %v\n%s", err, out)
	}

	dec := json.NewDecoder(strings.NewReader(out))
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, out)
	}
	if dec.More() {
		t.Fatalf("stdout has more than one JSON value:\n%s", out)
	}
	drifts, ok := doc["drifts"].([]any)
	if !ok {
		t.Fatalf("drifts should serialize as an array, not %#v (null breaks naive array consumers)", doc["drifts"])
	}
	if len(drifts) != 0 {
		t.Errorf("expected zero drifts for this minimal fixture, got %v", drifts)
	}
	if clean, _ := doc["clean"].(bool); !clean {
		t.Errorf("expected clean: true, got %v", doc["clean"])
	}
}

// TestVerifyTextSummary is criterion 11.
func TestVerifyTextSummary(t *testing.T) {
	worklogDir := newCLIFixtureDir(t)
	t.Setenv("DEVBOARD_DATA", t.TempDir())

	out, err := invokeVerify(t, worklogDir)
	if err != nil {
		t.Fatalf("expected a clean run, got error: %v\n%s", err, out)
	}
	if !strings.Contains(out, "clean") {
		t.Errorf("text summary missing the clean verdict:\n%s", out)
	}

	// Now introduce drift: the raw synthetic fixture corpus is NOT a render
	// fixpoint (see TestVerifyCleanCorpus's doc comment — it deliberately
	// contains a phase alias, an implicit epic/child parent relation, and a
	// bare producer YAML), so pointing verify at it directly is a reliable,
	// already-proven source of real drift.
	corpus, err := filepath.Abs(filepath.Join("..", "convert", "testdata", "corpus"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEVBOARD_DATA", filepath.Join(corpus, "devboard"))

	out2, err2 := invokeVerify(t, corpus)
	if err2 == nil {
		t.Fatalf("expected a non-nil error for a drifted run, got clean output:\n%s", out2)
	}
	if ec, ok := err2.(exitCoder); !ok || ec.ExitCode() != 2 {
		t.Fatalf("expected exit code 2 (drift found), got: %v", err2)
	}
	if !strings.Contains(out2, "drift") {
		t.Errorf("text summary missing a drift report:\n%s", out2)
	}
}

// TestVerifyIntegrationFixtureCorpus is criterion 14: the full command path
// runs unconditionally in CI against the synthetic fixture corpus, not
// gated behind WORKLOG_SNAPSHOT the way TestLiveSnapshot is.
func TestVerifyIntegrationFixtureCorpus(t *testing.T) {
	corpus, err := filepath.Abs(filepath.Join("..", "convert", "testdata", "corpus"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEVBOARD_DATA", filepath.Join(corpus, "devboard"))

	out, err := invokeVerify(t, corpus, "--json")
	if err != nil {
		if ec, ok := err.(exitCoder); !ok || ec.ExitCode() != 2 {
			t.Fatalf("expected either a clean run or exit code 2 (drift found), got: %v\n%s", err, out)
		}
	}

	dec := json.NewDecoder(strings.NewReader(out))
	var doc map[string]any
	if decErr := dec.Decode(&doc); decErr != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", decErr, out)
	}
	if dec.More() {
		t.Fatalf("stdout has more than one JSON value:\n%s", out)
	}
	if _, ok := doc["drifts"]; !ok {
		t.Errorf("JSON document missing drifts: %v", doc)
	}
}
