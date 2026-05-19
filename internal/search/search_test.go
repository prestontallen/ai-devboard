package search

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prestontallen/day2day/internal/model"
)

func fixtureWorkdir(t *testing.T, files map[string]string) model.Workdir {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	wd, err := model.NewWorkdir(root)
	if err != nil {
		t.Fatal(err)
	}
	return wd
}

func TestSearchEmptyTerm(t *testing.T) {
	wd := fixtureWorkdir(t, map[string]string{"WORK.md": "## Now\n## Next\n## Someday\n"})
	_, err := Run(wd, Inputs{Term: ""})
	if !errors.Is(err, ErrEmptyTerm) {
		t.Errorf("expected ErrEmptyTerm, got %v", err)
	}
	_, err = Run(wd, Inputs{Term: "   "})
	if !errors.Is(err, ErrEmptyTerm) {
		t.Errorf("expected ErrEmptyTerm for whitespace, got %v", err)
	}
}

func TestSearchIndexHit(t *testing.T) {
	wd := fixtureWorkdir(t, map[string]string{
		"WORK.md": `## Now
- [~] **AUTH-1** — Refactor auth middleware
  - **ID**: auth-1
  - **Repo**: api
  - **Started**: 2026-05-15

## Next

## Someday
`,
		"INDEX.md": `# Worklog Index

## By ticket

- auth-1 → WORK.md (Now)

## By tag

## By repo

## By archive month
`,
	})
	out, err := Run(wd, Inputs{Term: "auth-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !out.IndexUsed || out.FellBackToFullText {
		t.Errorf("IndexUsed/Fallback wrong: %+v", out)
	}
	if len(out.Hits) != 1 {
		t.Fatalf("expected 1 hit, got %d: %+v", len(out.Hits), out.Hits)
	}
	h := out.Hits[0]
	if h.Source != "index" || h.ID != "auth-1" || h.File != "WORK.md" {
		t.Errorf("hit shape wrong: %+v", h)
	}
	if !strings.Contains(h.Snippet, "AUTH-1") {
		t.Errorf("snippet missing block content:\n%s", h.Snippet)
	}
}

func TestSearchFullTextFallback(t *testing.T) {
	// INDEX.md exists but has no matching lines. Term hits an archive
	// entry's Summary; full-text fallback should find it.
	wd := fixtureWorkdir(t, map[string]string{
		"WORK.md": "## Now\n## Next\n## Someday\n",
		"INDEX.md": `# Worklog Index

## By ticket

- unrelated → archive/2026-04.md (archived 2026-04-25)

## By tag

## By repo

## By archive month

- 2026-04 → archive/2026-04.md (1 entry)
`,
		"archive/2026-04.md": `# Archive — 2026-04

## 2026-04-25

### old-auth — Old auth work
- **Repo**: api
- **Tags**: cleanup
- **Started → Completed**: 2026-04-20 → 2026-04-25
- **Summary**: cleaned up old authentication helpers
`,
	})
	out, err := Run(wd, Inputs{Term: "authentication"})
	if err != nil {
		t.Fatal(err)
	}
	if !out.FellBackToFullText {
		t.Errorf("expected FellBackToFullText=true")
	}
	if len(out.Hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(out.Hits))
	}
	if out.Hits[0].Source != "fulltext" {
		t.Errorf("source should be fulltext, got %q", out.Hits[0].Source)
	}
	if !strings.Contains(out.Hits[0].Snippet, "old-auth") {
		t.Errorf("snippet missing entry:\n%s", out.Hits[0].Snippet)
	}
}

func TestSearchInWorkMD(t *testing.T) {
	wd := fixtureWorkdir(t, map[string]string{
		"WORK.md": `## Now
- [~] **AUTH-1** — Refactor auth middleware
  - **ID**: auth-1
  - **Started**: 2026-05-15

## Next

## Someday
`,
		// No INDEX.md → all hits via fulltext fallback.
	})
	out, err := Run(wd, Inputs{Term: "middleware"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(out.Hits))
	}
	if out.Hits[0].File != "WORK.md" || out.Hits[0].ID != "auth-1" {
		t.Errorf("WORK.md hit wrong: %+v", out.Hits[0])
	}
}

func TestSearchInNotesFile(t *testing.T) {
	wd := fixtureWorkdir(t, map[string]string{
		"WORK.md": "## Now\n## Next\n## Someday\n",
		"notes/epic-a.md": `# Epic A

## Children

- [ ] child-1: authentication subsystem rewrite
- [ ] child-2: unrelated work
`,
	})
	out, err := Run(wd, Inputs{Term: "authentication"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(out.Hits))
	}
	h := out.Hits[0]
	if h.File != "notes/epic-a.md" || h.ID != "child-1" {
		t.Errorf("notes hit wrong: %+v", h)
	}
	if !strings.Contains(h.Snippet, "authentication") {
		t.Errorf("snippet missing match content:\n%s", h.Snippet)
	}
}

func TestSearchDeepFlag(t *testing.T) {
	// INDEX.md has a hit, but --deep forces full-text path.
	wd := fixtureWorkdir(t, map[string]string{
		"WORK.md": `## Now
- [~] **AUTH-1** — Refactor auth middleware
  - **ID**: auth-1
  - **Started**: 2026-05-15

## Next
## Someday
`,
		"INDEX.md": "# Index\n## By ticket\n- auth-1 → WORK.md (Now)\n",
	})
	out, err := Run(wd, Inputs{Term: "auth-1", Deep: true})
	if err != nil {
		t.Fatal(err)
	}
	if out.IndexUsed {
		t.Errorf("--deep should set IndexUsed=false")
	}
	if len(out.Hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(out.Hits))
	}
	if out.Hits[0].Source != "fulltext" {
		t.Errorf("source should be fulltext under --deep, got %q", out.Hits[0].Source)
	}
}

func TestSearchEmptyResult(t *testing.T) {
	wd := fixtureWorkdir(t, map[string]string{
		"WORK.md":  "## Now\n## Next\n## Someday\n",
		"INDEX.md": "# Index\n",
	})
	out, err := Run(wd, Inputs{Term: "totally-missing-term"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Hits) != 0 {
		t.Errorf("expected 0 hits, got %d", len(out.Hits))
	}
	if !out.IndexUsed || !out.FellBackToFullText {
		t.Errorf("on empty result both should be true: %+v", out)
	}
}

func TestSearchLimitCaps(t *testing.T) {
	// Build a WORK.md with several blocks all matching the same term.
	wd := fixtureWorkdir(t, map[string]string{
		"WORK.md": `## Now
- [ ] **TICKET-1** — auth thing one
  - **ID**: ticket-1

- [ ] **TICKET-2** — auth thing two
  - **ID**: ticket-2

- [ ] **TICKET-3** — auth thing three
  - **ID**: ticket-3

## Next
## Someday
`,
	})
	out, err := Run(wd, Inputs{Term: "auth", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Hits) != 2 {
		t.Fatalf("expected 2 hits (Limit=2), got %d", len(out.Hits))
	}
	if !out.Truncated {
		t.Errorf("expected Truncated=true when limit applied")
	}
}

func TestSearchNotesFileHitDoesNotLeakSentinelID(t *testing.T) {
	// When a notes file matches outside any checkbox line, the internal
	// anchor is "$file" — but the JSON wire shape must surface a meaningful
	// id (the epic ID from the filename), not the sentinel.
	wd := fixtureWorkdir(t, map[string]string{
		"WORK.md": "## Now\n## Next\n## Someday\n",
		"notes/epic-foo.md": `# Epic foo

## Background

Reference: JIRA-9999
`,
	})
	out, err := Run(wd, Inputs{Term: "JIRA-9999"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(out.Hits))
	}
	h := out.Hits[0]
	if h.ID != "epic-foo" {
		t.Errorf("id should be derived from filename, got %q", h.ID)
	}
	if strings.Contains(h.ID, "$") {
		t.Errorf("sentinel string leaked into id: %q", h.ID)
	}
}

func TestSearchTagSection(t *testing.T) {
	// "By tag" line lists IDs that point at WORK.md. Searching for the tag
	// name should surface those IDs via the index path.
	wd := fixtureWorkdir(t, map[string]string{
		"WORK.md": `## Now
- [~] **AUTH-1** — Refactor auth
  - **ID**: auth-1
  - **Tags**: refactor, auth
  - **Started**: 2026-05-15

## Next
## Someday
`,
		"INDEX.md": `# Worklog Index

## By ticket

- auth-1 → WORK.md (Now)

## By tag

- refactor: auth-1
- auth: auth-1

## By repo

## By archive month
`,
	})
	out, err := Run(wd, Inputs{Term: "refactor"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Hits) != 1 {
		t.Fatalf("expected 1 hit via By-tag, got %d: %+v", len(out.Hits), out.Hits)
	}
	if out.Hits[0].Source != "index" {
		t.Errorf("source should be index, got %q", out.Hits[0].Source)
	}
	if out.Hits[0].ID != "auth-1" {
		t.Errorf("expected auth-1, got %q", out.Hits[0].ID)
	}
}
