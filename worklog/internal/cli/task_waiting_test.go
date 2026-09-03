package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/convert"
	"github.com/prestontallen/ai-devboard/worklog/internal/devboard"
	"github.com/prestontallen/ai-devboard/worklog/internal/projection"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/memstore"
)

// waitingOnStoreFixture builds a canonical (render-fixpoint) worklog dir
// from workMD/archiveMD — going through convert+RenderAll first, same as
// canonicalWorklogFixture, rather than writing hand-authored markdown
// directly, so openStoreForWrite's hand-edit guard doesn't refuse the
// very first write on a title/banner difference that was never really a
// hand edit. No devboard task file is pre-created: worklogID has no
// board entry yet, and task_store.go's create-on-first-use path (the
// same one a genuinely new ticket goes through) is what the first
// `task waiting-on add` exercises. Migrates into a fresh, real store —
// every write verb, appendAnswerToWorklog included, is unconditionally
// store-backed, so the task-file mutation must agree with it on one
// system of record or the two writes would silently diverge.
func waitingOnStoreFixture(t *testing.T, workMD, archiveMD string) (devboardDir, worklogDir string) {
	t.Helper()
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "WORK.md"), []byte(workMD), 0o644); err != nil {
		t.Fatal(err)
	}
	if archiveMD != "" {
		if err := os.MkdirAll(filepath.Join(src, "archive"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, "archive", "2026-09.md"), []byte(archiveMD), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := memstore.New()
	c, err := convert.ReadCorpusDir(src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := convert.Load(s, c); err != nil {
		t.Fatal(err)
	}

	worklogDir = t.TempDir()
	if err := projection.RenderAll(s, worklogDir); err != nil {
		t.Fatal(err)
	}
	devboardDir = filepath.Join(worklogDir, "devboard")
	if err := os.MkdirAll(devboardDir, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("DEVBOARD_DATA", devboardDir)
	t.Setenv("WORKLOG_DIR", worklogDir)
	dataDir := filepath.Join(t.TempDir(), "migration")
	t.Setenv("WORKLOG_MIGRATION_DATA", dataDir)

	if _, stderr := runCLI(t, "migrate", "--dir", worklogDir, "--out", dataDir); strings.Contains(stderr, "error") {
		t.Fatalf("migrate: %s", stderr)
	}
	return devboardDir, worklogDir
}

// TestWaitingOnResolveRefusesCrossRepoID is deliberately gone (adb-cutover
// M4 legacy retirement): it pinned the cross-repo-group refusal on the
// direct resolveTaskPath call runWaitingOnResolve used to make. That call
// is gone — waiting-on resolve now resolves via storeMutateTaskOrChild
// (task_waiting.go), same as every other task<sub> mutation, which has no
// repo-group concept at all. See task_test.go's
// TestTaskUntrackRefusesCrossRepoID and its neighboring comment for the
// fuller version of this point; untrack is the one command that still
// resolves via resolveTaskPath and keeps its own cross-repo coverage.

func TestWaitingOnAddRequiresWho(t *testing.T) {
	dir := taskStoreFixture(t, false)

	_, _, err := runTask(t, "waiting-on", "add", "question?", "--id", "tkt")
	ec, ok := err.(exitCoder)
	if !ok || ec.ExitCode() != 64 || !strings.Contains(err.Error(), "--who is required") {
		t.Fatalf("expected exit 64 who-required, got %v", err)
	}

	if _, _, err := runTask(t, "waiting-on", "add", "question?", "--who", "platform", "--id", "tkt"); err != nil {
		t.Fatal(err)
	}
	task := loadTask(t, taskFilePath(dir))
	w := task.WaitingOn[0]
	if w.Who != "platform" || w.Asked == "" || w.Text != "question?" {
		t.Fatalf("entry = %+v", w)
	}
}

func TestWaitingOnResolveWithAnswerLiveTicket(t *testing.T) {
	_, wl := waitingOnStoreFixture(t, `## Now
- [~] **TKT-1** — Live ticket
  - **ID**: tkt-1
  - **Started**: 2026-09-01

## Next

## Someday
`, "")
	p := filepath.Join(devboard.DataDir(), "unknown", "tkt-1.yaml")

	mustRunT := func(args ...string) {
		t.Helper()
		if _, _, err := runTask(t, append(args, "--id", "tkt-1")...); err != nil {
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

// TestWaitingOnResolveAnswerArchivedTicket: an archived ticket (present
// only in archive/, not WORK.md) is still resolvable by slug through the
// store — no separate "notes file already exists" fallback needed, unlike
// the retired legacy path, since write-through creates notes/<id>.md the
// first time any ticket gets note content, archived or not.
func TestWaitingOnResolveAnswerArchivedTicket(t *testing.T) {
	_, wl := waitingOnStoreFixture(t, "## Now\n\n## Next\n\n## Someday\n", `# Archive — 2026-09

## 2026-09-01

### tkt-2 — Archived ticket
- **Completed**: 2026-09-01
`)

	if _, _, err := runTask(t, "waiting-on", "add", "q", "--who", "sec-team", "--id", "tkt-2"); err != nil {
		t.Fatal(err)
	}
	stdout, _, err := runTask(t, "waiting-on", "resolve", "1", "--answer", "approved", "--id", "tkt-2")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "answer recorded") {
		t.Fatalf("expected a recorded-answer status, got %q", stdout)
	}
	notes, err := os.ReadFile(filepath.Join(wl, "notes", "tkt-2.md"))
	if err != nil || !strings.Contains(string(notes), "sec-team answered") {
		t.Fatalf("notes not appended: %v\n%s", err, notes)
	}
	p := filepath.Join(devboard.DataDir(), "unknown", "tkt-2.yaml")
	if task := loadTask(t, p); len(task.Decision) == 0 {
		t.Fatal("decision missing")
	}
}

func TestWaitingOnResolveWithoutAnswer(t *testing.T) {
	dir := taskStoreFixture(t, false)
	p := taskFilePath(dir)

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
	dir := taskStoreFixture(t, false)
	p := taskFilePath(dir)

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
