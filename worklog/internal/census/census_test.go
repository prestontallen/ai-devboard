package census

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, root, rel string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func classOf(t *testing.T, entries []Entry, rel string) Class {
	t.Helper()
	for _, e := range entries {
		if e.Path == rel {
			return e.Class
		}
	}
	t.Fatalf("census never saw %q — a total enumeration cannot miss a file", rel)
	return ""
}

// TestCensusClassifiesTheCorpus covers the paths the converter reads.
func TestCensusClassifiesTheCorpus(t *testing.T) {
	live, board := t.TempDir(), t.TempDir()
	write(t, live, "WORK.md")
	write(t, live, "FEEDBACK.md")
	write(t, live, "INDEX.md")
	write(t, live, "archive/2026-09.md")
	write(t, live, "notes/a-slug.md")
	write(t, board, "ai-devboard/a-slug.yaml")
	write(t, board, "ai-devboard/_archive/old.yaml")

	r, err := Walk(live, board)
	if err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]Class{
		"WORK.md":            Canon,
		"FEEDBACK.md":        Canon,
		"INDEX.md":           Derived,
		"archive/2026-09.md": Canon,
		"notes/a-slug.md":    Canon,
	} {
		if got := classOf(t, r.Worklog, rel); got != want {
			t.Errorf("%s = %s, want %s", rel, got, want)
		}
	}
	for rel, want := range map[string]Class{
		"ai-devboard/a-slug.yaml":       Canon,
		"ai-devboard/_archive/old.yaml": Canon,
	} {
		if got := classOf(t, r.Devboard, rel); got != want {
			t.Errorf("devboard/%s = %s, want %s", rel, got, want)
		}
	}
	if u := r.Unclassified(); len(u) != 0 {
		t.Errorf("clean corpus reported unclassified %v", u)
	}
}

// TestCensusRefusesWhatTheReadersSkip is the point of the package. Each of
// these is a file convert.ReadCorpusDir silently does not read, so it would
// vanish from a conversion that then claims to be complete.
func TestCensusRefusesWhatTheReadersSkip(t *testing.T) {
	live, board := t.TempDir(), t.TempDir()
	write(t, live, "WORK.md")
	write(t, live, "notes/stale.md.bak")   // not *.md
	write(t, live, "archive/2026-08.MD")   // case-sensitive suffix
	write(t, live, "notes/deep/nested.md") // ReadCorpusDir is not recursive
	write(t, live, "stray.md")             // unknown top-level file
	write(t, board, "ai-devboard/notes.txt")
	write(t, board, "ai-devboard/a/b/deep.yaml") // deeper than <repo>/_archive/
	write(t, board, ".hidden/x.yaml")            // dot-prefixed group dir

	r, err := Walk(live, board)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, p := range r.Unclassified() {
		got[filepath.ToSlash(p)] = true
	}
	for _, want := range []string{
		"notes/stale.md.bak",
		"archive/2026-08.MD",
		"notes/deep/nested.md",
		"stray.md",
		"devboard/ai-devboard/notes.txt",
		"devboard/ai-devboard/a/b/deep.yaml",
		"devboard/.hidden/x.yaml",
	} {
		if !got[want] {
			t.Errorf("census did not refuse %q; the converter skips it silently", want)
		}
	}
}

// TestCensusRefusesNonRegularFiles: ReadCorpusDir will happily read through
// a symlinked notes file while migrate.listCorpusFiles requires IsRegular,
// so the two traversals disagree about whether it exists at all.
func TestCensusRefusesNonRegularFiles(t *testing.T) {
	live := t.TempDir()
	write(t, live, "WORK.md")
	write(t, live, "notes/real.md")
	if err := os.Symlink(filepath.Join(live, "notes", "real.md"), filepath.Join(live, "notes", "link.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	r, err := Walk(live, "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range r.Unclassified() {
		if p == "notes/link.md" {
			found = true
		}
	}
	if !found {
		t.Errorf("census accepted a symlinked notes file; unclassified = %v", r.Unclassified())
	}
}

// TestCensusAllowsTransient keeps the tool's own scratch from blocking a
// run it would otherwise pass.
func TestCensusAllowsTransient(t *testing.T) {
	live := t.TempDir()
	write(t, live, "WORK.md")
	write(t, live, ".freeze")
	write(t, live, "worklog.db.bak.20260903T205543Z")

	r, err := Walk(live, "")
	if err != nil {
		t.Fatal(err)
	}
	if u := r.Unclassified(); len(u) != 0 {
		t.Errorf("transient scratch reported as unclassified: %v", u)
	}
}

// TestCensusMissingRootsAreEmpty: devboard is opt-in by directory presence,
// and a corpus with no archive or notes is legitimate.
func TestCensusHandlesAbsentDevboard(t *testing.T) {
	live := t.TempDir()
	write(t, live, "WORK.md")
	r, err := Walk(live, "")
	if err != nil {
		t.Fatalf("absent devboard should not error: %v", err)
	}
	if len(r.Devboard) != 0 {
		t.Errorf("Devboard = %v, want empty", r.Devboard)
	}
}

// TestCensusAgainstSnapshot runs the census over a real corpus named by
// WORKLOG_SNAPSHOT (and optionally DEVBOARD_SNAPSHOT), the same opt-in
// shape projection's live-snapshot test uses. It is the cheapest way to
// learn what a genuine corpus contains that the readers skip.
func TestCensusAgainstSnapshot(t *testing.T) {
	live := os.Getenv("WORKLOG_SNAPSHOT")
	if live == "" {
		t.Skip("set WORKLOG_SNAPSHOT to run the census against a real corpus")
	}
	r, err := Walk(live, os.Getenv("DEVBOARD_SNAPSHOT"))
	if err != nil {
		t.Fatalf("census: %v", err)
	}
	counts := map[Class]int{}
	for _, e := range append(append([]Entry{}, r.Worklog...), r.Devboard...) {
		counts[e.Class]++
	}
	t.Logf("canon=%d derived=%d transient=%d unclassified=%d",
		counts[Canon], counts[Derived], counts[Transient], counts[Unclassified])
	for _, p := range r.Unclassified() {
		t.Logf("UNCLASSIFIED %s", p)
	}
}
