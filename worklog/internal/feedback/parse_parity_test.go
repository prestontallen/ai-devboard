package feedback

// Parity pins migrated from devboard/test_server.py before its retirement:
// that suite pinned the Python reader against this package's writer; these
// pin the one remaining parser against the same cases, and unlike the
// Python suite they run in CI.

import (
	"os"
	"path/filepath"
	"testing"
)

func parseString(t *testing.T, content string) []Entry {
	t.Helper()
	path := filepath.Join(t.TempDir(), "FEEDBACK.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return entries
}

func TestParseMissingFileIsEmptyNotAnError(t *testing.T) {
	entries, err := Parse(filepath.Join(t.TempDir(), "FEEDBACK.md"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("got %v, %v", entries, err)
	}
}

func TestParseHeaderOnlyFile(t *testing.T) {
	if entries := parseString(t, "# Worklog Feedback Log\n\n"); len(entries) != 0 {
		t.Fatalf("got %v", entries)
	}
}

func TestParseFullEntry(t *testing.T) {
	entries := parseString(t,
		"# Worklog Feedback Log\n\n"+
			"## 1788310587 — missing-feature\n"+
			"**Trigger**: worklog task refuses every command in a worktree\n"+
			"**Excerpt**:\n"+
			"> line one\n"+
			"> line two\n"+
			"**Context**: dispatcher note\n\n")
	if len(entries) != 1 {
		t.Fatalf("got %d entries", len(entries))
	}
	e := entries[0]
	if e.Timestamp != 1788310587 || e.Signal != "missing-feature" ||
		e.Trigger != "worklog task refuses every command in a worktree" ||
		e.Excerpt != "line one\nline two" || e.Context != "dispatcher note" || e.Resolved != 0 {
		t.Errorf("entry mismatch: %+v", e)
	}
}

func TestParseResolvedEntry(t *testing.T) {
	entries := parseString(t, "## 1000 — tui-error\n**Trigger**: t\n**Resolved**: 2000\n")
	if entries[0].Resolved != 2000 {
		t.Errorf("resolved: %d", entries[0].Resolved)
	}
}

func TestParseMultipleEntriesKeepFileOrder(t *testing.T) {
	entries := parseString(t,
		"## 1000 — tui-error\n**Trigger**: first\n\n"+
			"## 2000 — profanity\n**Trigger**: second\n**Resolved**: 3000\n")
	if len(entries) != 2 || entries[0].Timestamp != 1000 || entries[1].Timestamp != 2000 ||
		entries[0].Resolved != 0 || entries[1].Resolved != 3000 {
		t.Errorf("entries: %+v", entries)
	}
}

func TestParseUnknownFieldIsSkippedAndEntrySurvives(t *testing.T) {
	entries := parseString(t,
		"## 1000 — tui-error\n**Trigger**: t\n**SomethingNew**: from a future worklog\n**Context**: c\n")
	if len(entries) != 1 || entries[0].Trigger != "t" || entries[0].Context != "c" {
		t.Errorf("entries: %+v", entries)
	}
}

func TestParseUnknownFieldEndsExcerpt(t *testing.T) {
	// The unknown field must also terminate excerpt accumulation — lines
	// after it are not excerpt text (matches the retired Python reader).
	entries := parseString(t,
		"## 1000 — tui-error\n**Trigger**: t\n**Excerpt**:\n> kept\n**SomethingNew**: x\n> not kept\n")
	if entries[0].Excerpt != "kept" {
		t.Errorf("excerpt: %q", entries[0].Excerpt)
	}
}

func TestParseMalformedHeadingIsIgnored(t *testing.T) {
	entries := parseString(t,
		"## not an entry\n**Trigger**: orphaned\n\n## 1000 — tui-error\n**Trigger**: real\n")
	if len(entries) != 1 || entries[0].Trigger != "real" {
		t.Errorf("entries: %+v", entries)
	}
}

func TestParseNonNumericResolvedIsTreatedAsUnresolved(t *testing.T) {
	entries := parseString(t, "## 1000 — tui-error\n**Trigger**: t\n**Resolved**: yesterday\n")
	if entries[0].Resolved != 0 {
		t.Errorf("resolved: %d", entries[0].Resolved)
	}
}

func TestParseGarbageFileYieldsNoEntries(t *testing.T) {
	if entries := parseString(t, "\x00 not markdown at all \xff"); len(entries) != 0 {
		t.Errorf("entries: %+v", entries)
	}
}
