package sync

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureRepo lays out a tiny "repo" with the three skill source files and
// returns its root. The repo gets a go.mod so FindRepoRoot will work.
func fixtureRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	must := func(rel, content string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must("go.mod", "module x\n")
	must("skill/SKILL.md", "skill content\n")
	must("skill/claude/command.md", "command content\n")
	return root
}

func TestDefaultPairsShape(t *testing.T) {
	pairs := DefaultPairs("/repo", "/home/u")
	want := []Pair{
		{"/repo/skill/SKILL.md", "/home/u/.cursor/skills/worklog/SKILL.md"},
		{"/repo/skill/SKILL.md", "/home/u/.claude/skills/worklog/SKILL.md"},
		{"/repo/skill/claude/command.md", "/home/u/.claude/commands/worklog.md"},
	}
	if len(pairs) != len(want) {
		t.Fatalf("got %d pairs, want %d", len(pairs), len(want))
	}
	for i := range pairs {
		if pairs[i] != want[i] {
			t.Errorf("pair %d = %+v, want %+v", i, pairs[i], want[i])
		}
	}
}

func TestFindRepoRoot(t *testing.T) {
	root := fixtureRepo(t)
	deep := filepath.Join(root, "internal", "cli")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := FindRepoRoot(deep)
	if err != nil {
		t.Fatal(err)
	}
	if got != root {
		t.Errorf("FindRepoRoot(%s) = %s, want %s", deep, got, root)
	}
}

func TestFindRepoRootMissing(t *testing.T) {
	tmp := t.TempDir()
	_, err := FindRepoRoot(tmp)
	if err == nil {
		t.Errorf("expected error when no go.mod above %s", tmp)
	}
}

func TestRunDryRunWritesNothing(t *testing.T) {
	root := fixtureRepo(t)
	home := t.TempDir()
	var buf bytes.Buffer

	pairs := DefaultPairs(root, home)
	mismatch, err := Run(ModeDryRun, pairs, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if mismatch {
		t.Errorf("dry-run should not report mismatch")
	}

	// home must still be empty (no .cursor / .claude)
	entries, _ := os.ReadDir(home)
	if len(entries) != 0 {
		t.Errorf("dry-run wrote into home: %v", entries)
	}

	out := buf.String()
	for _, p := range pairs {
		if !strings.Contains(out, "would: cp "+p.Src+" -> "+p.Dst) {
			t.Errorf("missing dry-run cp line for %s -> %s\nstdout:\n%s", p.Src, p.Dst, out)
		}
	}
}

func TestRunCheckEmptyHome(t *testing.T) {
	root := fixtureRepo(t)
	home := t.TempDir()
	var buf bytes.Buffer

	mismatch, err := Run(ModeCheck, DefaultPairs(root, home), &buf)
	if err != nil {
		t.Fatal(err)
	}
	if !mismatch {
		t.Errorf("check on empty home should report mismatch")
	}
	count := strings.Count(buf.String(), "differ:")
	if count != 3 {
		t.Errorf("got %d differ lines, want 3:\n%s", count, buf.String())
	}
}

func TestRunDefaultEndToEnd(t *testing.T) {
	root := fixtureRepo(t)
	home := t.TempDir()
	pairs := DefaultPairs(root, home)

	var buf bytes.Buffer
	mismatch, err := Run(ModeDefault, pairs, &buf)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if mismatch {
		t.Errorf("default mode should not report mismatch")
	}

	// every target must exist and equal its source.
	for _, p := range pairs {
		if _, err := os.Stat(p.Dst); err != nil {
			t.Errorf("target missing after sync: %s", p.Dst)
			continue
		}
		da, _ := os.ReadFile(p.Src)
		db, _ := os.ReadFile(p.Dst)
		if !bytes.Equal(da, db) {
			t.Errorf("post-copy mismatch for %s -> %s", p.Src, p.Dst)
		}
	}
	if !strings.Contains(buf.String(), "synced:") {
		t.Errorf("expected 'synced:' in output, got:\n%s", buf.String())
	}
}

func TestRunDefaultIdempotent(t *testing.T) {
	root := fixtureRepo(t)
	home := t.TempDir()
	pairs := DefaultPairs(root, home)

	var buf1 bytes.Buffer
	if _, err := Run(ModeDefault, pairs, &buf1); err != nil {
		t.Fatal(err)
	}

	var buf2 bytes.Buffer
	if _, err := Run(ModeDefault, pairs, &buf2); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf2.String(), "unchanged:") {
		t.Errorf("second run should be unchanged, got:\n%s", buf2.String())
	}
	if strings.Contains(buf2.String(), "synced:") {
		t.Errorf("second run should NOT re-sync, got:\n%s", buf2.String())
	}
}

func TestRunCheckAfterSync(t *testing.T) {
	root := fixtureRepo(t)
	home := t.TempDir()
	pairs := DefaultPairs(root, home)

	if _, err := Run(ModeDefault, pairs, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	mismatch, err := Run(ModeCheck, pairs, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if mismatch {
		t.Errorf("post-sync check should report no mismatch, got:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "match:") {
		t.Errorf("expected 'match:' in output, got:\n%s", buf.String())
	}
}

func TestRunMissingSource(t *testing.T) {
	root := fixtureRepo(t)
	home := t.TempDir()
	pairs := DefaultPairs(root, home)
	// Remove one of the sources
	if err := os.Remove(pairs[0].Src); err != nil {
		t.Fatal(err)
	}

	_, err := Run(ModeDefault, pairs, &bytes.Buffer{})
	if !errors.Is(err, ErrSrcMissing) {
		t.Errorf("expected ErrSrcMissing, got %v", err)
	}
}

func TestRunRefusesTargetDir(t *testing.T) {
	root := fixtureRepo(t)
	home := t.TempDir()
	pairs := DefaultPairs(root, home)
	// Make the first target a directory.
	if err := os.MkdirAll(pairs[0].Dst, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := Run(ModeDefault, pairs, &bytes.Buffer{})
	if !errors.Is(err, ErrTargetIsDir) {
		t.Errorf("expected ErrTargetIsDir, got %v", err)
	}
}
