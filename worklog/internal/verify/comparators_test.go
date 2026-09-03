package verify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const workMDFixture = `# Worklog — active

## Now
- [~] **KID-LIVE** — Active child
  - **ID**: kid-live
  - **Started**: 2026-09-01
`

// TestVerifyDetectsWorkMDDrift is criterion 2: a field-level discrepancy
// on a WORK.md ticket block is reported localized to ticket + field.
func TestVerifyDetectsWorkMDDrift(t *testing.T) {
	live, rendered := t.TempDir(), t.TempDir()
	mustWriteFile(t, filepath.Join(live, "WORK.md"), workMDFixture)
	mustWriteFile(t, filepath.Join(rendered, "WORK.md"), strings.Replace(workMDFixture, "[~]", "[ ]", 1))

	drifts := compareWorkMD(live, rendered)
	if len(drifts) != 1 {
		t.Fatalf("expected exactly 1 drift, got %d: %+v", len(drifts), drifts)
	}
	d := drifts[0]
	// model.State is the raw checkbox character ("~" active, " " pending),
	// not a normalized word — see internal/parse/work.go's State(m[1]).
	if d.Ticket != "kid-live" || d.Field != "state" || d.Live != "~" || d.Rendered != " " {
		t.Errorf("unexpected drift: %+v", d)
	}
}

const archiveFixture = `# Archive — 2026-09

## 2026-09-02

### done-ticket — A finished thing
- **PR**: https://github.com/x/y/commit/abc
- **Started → Completed**: 2026-09-01 → 2026-09-02
`

// TestVerifyDetectsArchiveDrift is criterion 3.
func TestVerifyDetectsArchiveDrift(t *testing.T) {
	live, rendered := t.TempDir(), t.TempDir()
	mustWriteFile(t, filepath.Join(live, "archive", "2026-09.md"), archiveFixture)
	mustWriteFile(t, filepath.Join(rendered, "archive", "2026-09.md"),
		strings.Replace(archiveFixture, "commit/abc", "commit/DIFFERENT", 1))

	drifts := compareArchive(live, rendered)
	if len(drifts) != 1 {
		t.Fatalf("expected exactly 1 drift, got %d: %+v", len(drifts), drifts)
	}
	d := drifts[0]
	if d.Ticket != "done-ticket" || d.Field != "pr" {
		t.Errorf("unexpected drift: %+v", d)
	}
}

const notesFixture = `# An epic

## 2026-09-02 10:00
First entry body.
`

// TestVerifyDetectsNotesDrift is criterion 4 — the net-new comparator.
func TestVerifyDetectsNotesDrift(t *testing.T) {
	live, rendered := t.TempDir(), t.TempDir()
	mustWriteFile(t, filepath.Join(live, "notes", "an-epic.md"), notesFixture)
	mustWriteFile(t, filepath.Join(rendered, "notes", "an-epic.md"),
		strings.Replace(notesFixture, "First entry body.", "Changed entry body.", 1))

	drifts := compareNotes(live, rendered)
	if len(drifts) != 1 {
		t.Fatalf("expected exactly 1 drift, got %d: %+v", len(drifts), drifts)
	}
	d := drifts[0]
	if d.Ticket != "an-epic" || d.Field != "entries[0].body" {
		t.Errorf("unexpected drift: %+v", d)
	}
}

// TestVerifyDetectsIndexDrift is criterion 5 (detection half).
func TestVerifyDetectsIndexDrift(t *testing.T) {
	live, rendered := t.TempDir(), t.TempDir()
	mustWriteFile(t, filepath.Join(live, "WORK.md"), workMDFixture)
	// Rendered side is missing the ticket entirely: byTicket should drift.
	mustWriteFile(t, filepath.Join(rendered, "WORK.md"), "# Worklog — active\n\n## Now\n")

	drifts := compareIndex(live, rendered)
	if len(drifts) == 0 {
		t.Fatal("expected at least one drift")
	}
	found := false
	for _, d := range drifts {
		if d.Field == "byTicket" && d.Live == "1" && d.Rendered == "0" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a byTicket drift 1 -> 0, got: %+v", drifts)
	}
}

// TestVerifyNeverWritesLiveIndex is criterion 5 (the direct proof against
// Decision #3's blocker: reindex.Run writes INDEX.md unless DryRun:true).
func TestVerifyNeverWritesLiveIndex(t *testing.T) {
	live, rendered := t.TempDir(), t.TempDir()
	mustWriteFile(t, filepath.Join(live, "WORK.md"), workMDFixture)
	mustWriteFile(t, filepath.Join(rendered, "WORK.md"), workMDFixture)

	compareIndex(live, rendered)

	for _, dir := range []string{live, rendered} {
		if _, err := os.Stat(filepath.Join(dir, "INDEX.md")); !os.IsNotExist(err) {
			t.Errorf("expected no INDEX.md written under %s, stat err = %v", dir, err)
		}
	}
}

const feedbackFixture = `# Friction log

## 1000 — missing-feature
**Trigger**: wanted a verb
`

// TestVerifyDetectsFeedbackDrift is criterion 6.
func TestVerifyDetectsFeedbackDrift(t *testing.T) {
	live, rendered := t.TempDir(), t.TempDir()
	mustWriteFile(t, filepath.Join(live, "FEEDBACK.md"), feedbackFixture)
	mustWriteFile(t, filepath.Join(rendered, "FEEDBACK.md"),
		strings.Replace(feedbackFixture, "wanted a verb", "wanted a different verb", 1))

	drifts := compareFeedback(live, rendered)
	if len(drifts) != 1 {
		t.Fatalf("expected exactly 1 drift, got %d: %+v", len(drifts), drifts)
	}
	d := drifts[0]
	if d.Ticket != "1000" || d.Field != "trigger" {
		t.Errorf("unexpected drift: %+v", d)
	}
}

// TestVerifyDetectsBoardDrift is criterion 7.
func TestVerifyDetectsBoardDrift(t *testing.T) {
	live, rendered := t.TempDir(), t.TempDir()
	mustCopyDir(t, corpusDir, live)
	mustCopyDir(t, corpusDir, rendered)

	yamlPath := filepath.Join(rendered, "devboard", "ai-devboard", "an-epic.yaml")
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(yamlPath, []byte(strings.Replace(string(data), "custom_top: survives", "custom_top: CHANGED", 1)), 0o644); err != nil {
		t.Fatal(err)
	}

	drifts := compareBoard(live, rendered)
	if len(drifts) != 1 {
		t.Fatalf("expected exactly 1 drift, got %d: %+v", len(drifts), drifts)
	}
	d := drifts[0]
	if d.Ticket != "an-epic" || d.Field != "task" {
		t.Errorf("unexpected drift: %+v", d)
	}
	if !strings.Contains(d.Live, "survives") || !strings.Contains(d.Rendered, "CHANGED") {
		t.Errorf("drift values don't reflect the change: %+v", d)
	}
}

// TestVerifyIgnoresBareProducerFiles: a devboard file with no worklog:
// join key is producer-owned, not canon (store-design.md) — the store
// legitimately never renders it, and that must not report as drift. The
// fixture's devboard/nole/bare.yaml has no other task in its repo group,
// so this also exercises the whole-repo-missing (repo-presence) path.
func TestVerifyIgnoresBareProducerFiles(t *testing.T) {
	live, rendered := t.TempDir(), t.TempDir()
	mustCopyDir(t, corpusDir, live)
	mustCopyDir(t, corpusDir, rendered)

	// Simulate what RenderTo actually produces: the bare file is absent
	// from the store-rendered side entirely, and (since it was the only
	// task in its repo group) the whole repo group is absent too.
	if err := os.RemoveAll(filepath.Join(rendered, "devboard", "nole")); err != nil {
		t.Fatal(err)
	}

	drifts := compareBoard(live, rendered)
	if len(drifts) != 0 {
		t.Errorf("expected no drift for a bare producer file, got %+v", drifts)
	}
}

// TestVerifyStillDetectsRealPresenceDriftAlongsideBareFiles: the bare-file
// exemption must not swallow a genuine missing joined ticket in the same
// repo group.
func TestVerifyStillDetectsRealPresenceDriftAlongsideBareFiles(t *testing.T) {
	live, rendered := t.TempDir(), t.TempDir()
	mustCopyDir(t, corpusDir, live)
	mustCopyDir(t, corpusDir, rendered)

	mustWriteFile(t, filepath.Join(live, "devboard", "ai-devboard", "bare-sibling.yaml"),
		"schema: 1\ntitle: Producer-owned, no join\nphase: done\n")
	// A second joined ticket, present on both sides, keeps the repo group
	// non-empty on the rendered side — otherwise removing the only real
	// ticket next would drop the whole group (a repo-presence drift, a
	// different code path from the per-ticket one this test targets).
	otherFixture := "schema: 1\ntitle: Another real ticket\nworklog: another-ticket\nphase: done\n"
	mustWriteFile(t, filepath.Join(live, "devboard", "ai-devboard", "another-ticket.yaml"), otherFixture)
	mustWriteFile(t, filepath.Join(rendered, "devboard", "ai-devboard", "another-ticket.yaml"), otherFixture)
	// bare-sibling.yaml is NOT copied into rendered — matches what a real
	// store render would do — but an-epic.yaml (a real joined ticket) is
	// also deliberately dropped from rendered to prove it's still caught.
	if err := os.Remove(filepath.Join(rendered, "devboard", "ai-devboard", "an-epic.yaml")); err != nil {
		t.Fatal(err)
	}

	drifts := compareBoard(live, rendered)
	if len(drifts) != 1 {
		t.Fatalf("expected exactly 1 drift (the real missing ticket), got %d: %+v", len(drifts), drifts)
	}
	if drifts[0].Ticket != "an-epic" || drifts[0].Field != "presence" {
		t.Errorf("unexpected drift: %+v", drifts[0])
	}
}
