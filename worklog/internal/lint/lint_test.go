package lint

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExtractRulesValid(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "README.md", `# Title

Intro text

<!-- rules:start -->

1. Rule one.
2. Rule two.

<!-- rules:end -->

Outro.
`)
	got, err := ExtractRules(p)
	if err != nil {
		t.Fatal(err)
	}
	want := "1. Rule one.\n2. Rule two."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExtractRulesMissingStart(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "x.md", "no markers here\n<!-- rules:end -->\n")
	_, err := ExtractRules(p)
	if !errors.Is(err, ErrMissingMarker) {
		t.Errorf("expected ErrMissingMarker, got %v", err)
	}
}

func TestExtractRulesMissingEnd(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "x.md", "<!-- rules:start -->\n- rule\n")
	_, err := ExtractRules(p)
	if !errors.Is(err, ErrMissingMarker) {
		t.Errorf("expected ErrMissingMarker, got %v", err)
	}
}

func TestDiffIdentical(t *testing.T) {
	dir := t.TempDir()
	body := "<!-- rules:start -->\n- rule\n<!-- rules:end -->\n"
	a := writeFile(t, dir, "a.md", body)
	b := writeFile(t, dir, "b.md", body)
	d, err := Diff(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if d != "" {
		t.Errorf("expected empty diff, got %q", d)
	}
}

func TestDiffDifferent(t *testing.T) {
	dir := t.TempDir()
	a := writeFile(t, dir, "a.md", "<!-- rules:start -->\n- rule A\n<!-- rules:end -->\n")
	b := writeFile(t, dir, "b.md", "<!-- rules:start -->\n- rule B\n<!-- rules:end -->\n")
	d, err := Diff(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if d == "" {
		t.Error("expected non-empty diff")
	}
	if !strings.Contains(d, "rule A") || !strings.Contains(d, "rule B") {
		t.Errorf("diff missing block content:\n%s", d)
	}
}

func TestRunCheckNoDrift(t *testing.T) {
	dir := t.TempDir()
	body := "<!-- rules:start -->\n- same\n<!-- rules:end -->\n"
	a := writeFile(t, dir, "a.md", body)
	b := writeFile(t, dir, "b.md", body)
	c := writeFile(t, dir, "c.md", body)

	var buf bytes.Buffer
	drift, err := RunCheck([]string{a, b, c}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if drift {
		t.Errorf("expected no drift, got output:\n%s", buf.String())
	}
}

func TestRunCheckPairwiseDrift(t *testing.T) {
	dir := t.TempDir()
	a := writeFile(t, dir, "a.md", "<!-- rules:start -->\n- one\n<!-- rules:end -->\n")
	b := writeFile(t, dir, "b.md", "<!-- rules:start -->\n- two\n<!-- rules:end -->\n")
	c := writeFile(t, dir, "c.md", "<!-- rules:start -->\n- three\n<!-- rules:end -->\n")

	var buf bytes.Buffer
	drift, err := RunCheck([]string{a, b, c}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if !drift {
		t.Error("expected drift across 3 distinct files")
	}
	// All three pairs should emit a DRIFT block.
	count := strings.Count(buf.String(), "=== DRIFT")
	if count != 3 {
		t.Errorf("expected 3 DRIFT blocks, got %d:\n%s", count, buf.String())
	}
}

func TestRunPrint(t *testing.T) {
	dir := t.TempDir()
	a := writeFile(t, dir, "a.md", "<!-- rules:start -->\n- one\n<!-- rules:end -->\n")
	b := writeFile(t, dir, "b.md", "<!-- rules:start -->\n- two\n<!-- rules:end -->\n")

	var buf bytes.Buffer
	if err := RunPrint([]string{a, b}, &buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "=== "+a+" ===") {
		t.Errorf("missing header for %s:\n%s", a, out)
	}
	if !strings.Contains(out, "=== "+b+" ===") {
		t.Errorf("missing header for %s:\n%s", b, out)
	}
	if !strings.Contains(out, "- one") || !strings.Contains(out, "- two") {
		t.Errorf("missing rule content:\n%s", out)
	}
}

func TestRunCheckPropagatesMarkerError(t *testing.T) {
	dir := t.TempDir()
	a := writeFile(t, dir, "a.md", "<!-- rules:start -->\n- one\n<!-- rules:end -->\n")
	b := writeFile(t, dir, "b.md", "no markers")

	_, err := RunCheck([]string{a, b}, &bytes.Buffer{})
	if !errors.Is(err, ErrMissingMarker) {
		t.Errorf("expected ErrMissingMarker, got %v", err)
	}
}

func TestExtractRulesStripsBlankEdges(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "x.md", `<!-- rules:start -->




- only line


<!-- rules:end -->
`)
	got, err := ExtractRules(p)
	if err != nil {
		t.Fatal(err)
	}
	if got != "- only line" {
		t.Errorf("got %q, want %q", got, "- only line")
	}
}
