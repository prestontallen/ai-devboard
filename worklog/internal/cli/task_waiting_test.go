package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func waitingTaskFile(t *testing.T, dir, worklogID string) string {
	t.Helper()
	p := filepath.Join(dir, "repo-x", "tkt.yaml")
	os.MkdirAll(filepath.Dir(p), 0o755)
	content := "schema: 1\ntitle: T\n"
	if worklogID != "" {
		content += "worklog: " + worklogID + "\n"
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestWaitingOnAddRequiresWho(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DEVBOARD_DATA", dir)
	waitingTaskFile(t, dir, "")

	_, _, err := runTask(t, "waiting-on", "add", "question?", "--id", "tkt")
	ec, ok := err.(exitCoder)
	if !ok || ec.ExitCode() != 64 || !strings.Contains(err.Error(), "--who is required") {
		t.Fatalf("expected exit 64 who-required, got %v", err)
	}

	if _, _, err := runTask(t, "waiting-on", "add", "question?", "--who", "platform", "--id", "tkt"); err != nil {
		t.Fatal(err)
	}
	task := loadTask(t, filepath.Join(dir, "repo-x", "tkt.yaml"))
	w := task.WaitingOn[0]
	if w.Who != "platform" || w.Asked == "" || w.Text != "question?" {
		t.Fatalf("entry = %+v", w)
	}
}

func TestWaitingOnResolveWithAnswerLiveTicket(t *testing.T) {
	dir, wl := t.TempDir(), t.TempDir()
	t.Setenv("DEVBOARD_DATA", dir)
	t.Setenv("WORKLOG_DIR", wl)
	os.WriteFile(filepath.Join(wl, "WORK.md"), []byte(`## Now
- [~] **TKT-1** — Live ticket
  - **ID**: tkt-1
  - **Started**: 2026-09-01

## Next

## Someday
`), 0o644)
	p := waitingTaskFile(t, dir, "tkt-1")

	mustRunT := func(args ...string) {
		t.Helper()
		if _, _, err := runTask(t, append(args, "--id", "tkt")...); err != nil {
			t.Fatal(err)
		}
	}
	mustRunT("waiting-on", "add", "rate limit?", "--who", "platform", "--asked", "2026-08-20")
	mustRunT("waiting-on", "resolve", "1", "--answer", "limit raised to 500/min")

	task := loadTask(t, p)
	if len(task.WaitingOn) != 0 {
		t.Fatalf("entry not removed: %+v", task.WaitingOn)
	}
	d := task.Decision[len(task.Decision)-1]
	if !strings.Contains(d.What, "platform answered: limit raised") || !strings.Contains(d.Why, "asked 2026-08-20") {
		t.Fatalf("decision = %+v", d)
	}
	notes, err := os.ReadFile(filepath.Join(wl, "notes", "tkt-1.md"))
	if err != nil || !strings.Contains(string(notes), "platform answered") ||
		!strings.Contains(string(notes), "limit raised to 500/min") {
		t.Fatalf("notes append missing: %v\n%s", err, notes)
	}
}

func TestWaitingOnResolveAnswerArchivedWithNotes(t *testing.T) {
	dir, wl := t.TempDir(), t.TempDir()
	t.Setenv("DEVBOARD_DATA", dir)
	t.Setenv("WORKLOG_DIR", wl)
	// WORK.md exists but the ticket is archived (not in it); notes file remains.
	os.WriteFile(filepath.Join(wl, "WORK.md"), []byte("## Now\n\n## Next\n\n## Someday\n"), 0o644)
	os.MkdirAll(filepath.Join(wl, "notes"), 0o755)
	os.WriteFile(filepath.Join(wl, "notes", "tkt-2.md"), []byte("# Notes — tkt-2\n"), 0o644)
	p := waitingTaskFile(t, dir, "tkt-2")

	if _, _, err := runTask(t, "waiting-on", "add", "q", "--who", "sec-team", "--id", "tkt"); err != nil {
		t.Fatal(err)
	}
	stdout, _, err := runTask(t, "waiting-on", "resolve", "1", "--answer", "approved", "--id", "tkt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "archived ticket") {
		t.Fatalf("expected archived-ticket path, got %q", stdout)
	}
	notes, _ := os.ReadFile(filepath.Join(wl, "notes", "tkt-2.md"))
	if !strings.Contains(string(notes), "sec-team answered") {
		t.Fatalf("notes not appended:\n%s", notes)
	}
	if task := loadTask(t, p); len(task.Decision) == 0 {
		t.Fatal("decision missing")
	}
}

func TestWaitingOnResolveAnswerArchivedNoNotes(t *testing.T) {
	dir, wl := t.TempDir(), t.TempDir()
	t.Setenv("DEVBOARD_DATA", dir)
	t.Setenv("WORKLOG_DIR", wl)
	os.WriteFile(filepath.Join(wl, "WORK.md"), []byte("## Now\n\n## Next\n\n## Someday\n"), 0o644)
	p := waitingTaskFile(t, dir, "gone-1")

	if _, _, err := runTask(t, "waiting-on", "add", "q", "--who", "x", "--id", "tkt"); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runTask(t, "waiting-on", "resolve", "1", "--answer", "yes", "--id", "tkt")
	if err != nil {
		t.Fatalf("must never hard-fail: %v", err)
	}
	if !strings.Contains(stdout, "no notes file") || !strings.Contains(stderr, "task decision only") {
		t.Fatalf("expected decision-only fallback, out=%q err=%q", stdout, stderr)
	}
	task := loadTask(t, p)
	if len(task.WaitingOn) != 0 || !strings.Contains(task.Decision[len(task.Decision)-1].What, "x answered: yes") {
		t.Fatalf("task = %+v", task)
	}
}

func TestWaitingOnResolveWithoutAnswer(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DEVBOARD_DATA", dir)
	p := waitingTaskFile(t, dir, "")

	runTask(t, "waiting-on", "add", "q", "--who", "y", "--id", "tkt")
	if _, _, err := runTask(t, "waiting-on", "resolve", "1", "--id", "tkt"); err != nil {
		t.Fatal(err)
	}
	task := loadTask(t, p)
	if !strings.Contains(task.Decision[len(task.Decision)-1].What, "closed unanswered: q (y)") {
		t.Fatalf("decision = %+v", task.Decision)
	}
}

func TestWaitingOnResolveAllIsCloseOut(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DEVBOARD_DATA", dir)
	p := waitingTaskFile(t, dir, "")

	runTask(t, "waiting-on", "add", "q1", "--who", "a", "--id", "tkt")
	runTask(t, "waiting-on", "add", "q2", "--who", "b", "--id", "tkt")
	if _, _, err := runTask(t, "waiting-on", "resolve", "all", "--id", "tkt"); err != nil {
		t.Fatal(err)
	}
	task := loadTask(t, p)
	if len(task.WaitingOn) != 0 || len(task.Decision) != 2 {
		t.Fatalf("task = %+v", task)
	}
	for _, d := range task.Decision {
		if !strings.Contains(d.What, "unanswered at close:") {
			t.Fatalf("decision = %+v", d)
		}
	}
}
