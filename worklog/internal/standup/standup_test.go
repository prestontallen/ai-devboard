package standup

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prestontallen/day2day/internal/model"
)

func makeWD(t *testing.T, workMD string, archiveFiles map[string]string) model.Workdir {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "WORK.md"), []byte(workMD), 0o644); err != nil {
		t.Fatal(err)
	}
	if len(archiveFiles) > 0 {
		if err := os.MkdirAll(filepath.Join(root, "archive"), 0o755); err != nil {
			t.Fatal(err)
		}
		for name, content := range archiveFiles {
			if err := os.WriteFile(filepath.Join(root, "archive", name), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return model.Workdir{Root: root}
}

func date(s string) time.Time {
	t, _ := time.ParseInLocation("2006-01-02", s, time.Local)
	return t
}

func TestBuildEmpty(t *testing.T) {
	wd := makeWD(t, "## Now\n\n## Waiting\n\n## Next\n\n## Someday\n", nil)
	today := date("2026-05-20")
	report, err := Build(wd, Options{Today: today})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if report.Today != "2026-05-20" {
		t.Errorf("Today = %q, want 2026-05-20", report.Today)
	}
	if report.Since != "2026-05-19" {
		t.Errorf("Since = %q, want 2026-05-19", report.Since)
	}
	if report.Until != "2026-05-20" {
		t.Errorf("Until = %q, want 2026-05-20", report.Until)
	}
	if len(report.Completed) != 0 {
		t.Errorf("Completed = %d, want 0", len(report.Completed))
	}
	if len(report.Active) != 0 {
		t.Errorf("Active = %d, want 0", len(report.Active))
	}
	if len(report.Waiting) != 0 {
		t.Errorf("Waiting = %d, want 0", len(report.Waiting))
	}
}

func TestBuildYesterdayWindow(t *testing.T) {
	archiveMD := `## Archive

### DOCS-3 — Clean up README
- **Repo**: docs
- **Started → Completed**: 2026-05-18 → 2026-05-19
- **Summary**: Updated docs.

### AUTH-OLD — Old auth fix
- **Repo**: api
- **Started → Completed**: 2026-05-08 → 2026-05-09
- **Summary**: Fixed auth.

### LAST-WEEK — Last week task
- **Repo**: api
- **Started → Completed**: 2026-05-11 → 2026-05-12
- **Summary**: Done last week.
`
	wd := makeWD(t, "## Now\n\n## Waiting\n\n## Next\n\n## Someday\n",
		map[string]string{"2026-05.md": archiveMD})
	today := date("2026-05-20")

	// Default window: yesterday only (2026-05-19).
	report, err := Build(wd, Options{Today: today})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(report.Completed) != 1 {
		t.Fatalf("Completed = %d, want 1", len(report.Completed))
	}
	if report.Completed[0].ID != "docs-3" {
		t.Errorf("Completed[0].ID = %q, want docs-3", report.Completed[0].ID)
	}

	// Widen window to last week.
	report2, err := Build(wd, Options{
		Today: today,
		Since: date("2026-05-08"),
		Until: date("2026-05-20"),
	})
	if err != nil {
		t.Fatalf("Build (wide): %v", err)
	}
	if len(report2.Completed) != 3 {
		t.Errorf("Completed (wide) = %d, want 3", len(report2.Completed))
	}
}

func TestBuildTodayActive(t *testing.T) {
	workMD := `## Now
- [~] **AUTH-1** — Refactor auth middleware
  - **ID**: auth-1
  - **Repo**: api
  - **Started**: 2026-05-15

- [ ] **PAY-PLAN** — Payment planning
  - **ID**: pay-plan
  - **Started**: 2026-05-18

- [~] **PAY-5** — Stripe webhook fixes
  - **ID**: pay-5
  - **Repo**: api
  - **Started**: 2026-05-18

## Waiting

## Next

## Someday
`
	wd := makeWD(t, workMD, nil)
	today := date("2026-05-20")
	report, err := Build(wd, Options{Today: today})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(report.Active) != 2 {
		t.Fatalf("Active = %d, want 2 (only [~] blocks)", len(report.Active))
	}
	// Sorted by Started asc: auth-1 (05-15) before pay-5 (05-18).
	if report.Active[0].ID != "auth-1" {
		t.Errorf("Active[0].ID = %q, want auth-1", report.Active[0].ID)
	}
	if report.Active[1].ID != "pay-5" {
		t.Errorf("Active[1].ID = %q, want pay-5", report.Active[1].ID)
	}
}

func TestBuildBlockersWithAge(t *testing.T) {
	workMD := `## Now

## Waiting
- [ ] **PAY-7** — Provider docs review
  - **ID**: pay-7
  - **Waiting since**: 2026-05-15

- [ ] **AUTH-BLOCKED** — Auth vendor blocker
  - **ID**: auth-blocked
  - **Waiting since**: 2026-05-19

## Next

## Someday
`
	wd := makeWD(t, workMD, nil)
	today := date("2026-05-20")
	report, err := Build(wd, Options{Today: today})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(report.Waiting) != 2 {
		t.Fatalf("Waiting = %d, want 2", len(report.Waiting))
	}
	// Sorted oldest first: pay-7 (05-15, 5 days) before auth-blocked (05-19, 1 day).
	if report.Waiting[0].ID != "pay-7" {
		t.Errorf("Waiting[0].ID = %q, want pay-7", report.Waiting[0].ID)
	}
	if report.Waiting[0].WaitingDays != 5 {
		t.Errorf("Waiting[0].WaitingDays = %d, want 5", report.Waiting[0].WaitingDays)
	}
	if report.Waiting[1].ID != "auth-blocked" {
		t.Errorf("Waiting[1].ID = %q, want auth-blocked", report.Waiting[1].ID)
	}
	if report.Waiting[1].WaitingDays != 1 {
		t.Errorf("Waiting[1].WaitingDays = %d, want 1", report.Waiting[1].WaitingDays)
	}
}

func TestBuildBlockersUnparseableDate(t *testing.T) {
	workMD := `## Now

## Waiting
- [ ] **BLOCKED-1** — Something stuck
  - **ID**: blocked-1
  - **Waiting since**: not-a-date

## Next

## Someday
`
	wd := makeWD(t, workMD, nil)
	today := date("2026-05-20")
	report, err := Build(wd, Options{Today: today})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(report.Waiting) != 1 {
		t.Fatalf("Waiting = %d, want 1", len(report.Waiting))
	}
	if report.Waiting[0].WaitingDays != -1 {
		t.Errorf("WaitingDays = %d, want -1 for unparseable date", report.Waiting[0].WaitingDays)
	}
}

func TestBuildSortStability(t *testing.T) {
	archiveMD := `## Archive

### BETA-2 — Beta task
- **Repo**: api
- **Started → Completed**: 2026-05-18 → 2026-05-19
- **Summary**: Beta done.

### ALPHA-1 — Alpha task
- **Repo**: api
- **Started → Completed**: 2026-05-17 → 2026-05-19
- **Summary**: Alpha done.
`
	wd := makeWD(t, "## Now\n\n## Waiting\n\n## Next\n\n## Someday\n",
		map[string]string{"2026-05.md": archiveMD})
	today := date("2026-05-20")
	report, err := Build(wd, Options{Today: today})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(report.Completed) != 2 {
		t.Fatalf("Completed = %d, want 2", len(report.Completed))
	}
	// Same Completed date → sorted by ID asc: alpha-1 before beta-2.
	if report.Completed[0].ID != "alpha-1" {
		t.Errorf("Completed[0].ID = %q, want alpha-1", report.Completed[0].ID)
	}
	if report.Completed[1].ID != "beta-2" {
		t.Errorf("Completed[1].ID = %q, want beta-2", report.Completed[1].ID)
	}
}

func TestBuildArchiveDirMissing(t *testing.T) {
	// No archive/ subdirectory created.
	wd := makeWD(t, "## Now\n\n## Waiting\n\n## Next\n\n## Someday\n", nil)
	today := date("2026-05-20")
	report, err := Build(wd, Options{Today: today})
	if err != nil {
		t.Fatalf("Build: unexpected error: %v", err)
	}
	if len(report.Completed) != 0 {
		t.Errorf("Completed = %d, want 0 when archive dir missing", len(report.Completed))
	}
}
