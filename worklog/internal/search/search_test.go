package search

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
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

func singleQ(term string) Query {
	return Query{Terms: []string{strings.ToLower(strings.TrimSpace(term))}, Mode: ModeSingle}
}

func TestSearchEmptyTerm(t *testing.T) {
	wd := fixtureWorkdir(t, map[string]string{"WORK.md": "## Now\n## Next\n## Someday\n"})
	_, err := Run(wd, Inputs{Query: Query{}})
	if !errors.Is(err, ErrEmptyTerm) {
		t.Errorf("expected ErrEmptyTerm, got %v", err)
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
	out, err := Run(wd, Inputs{Query: singleQ("auth-1")})
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
	out, err := Run(wd, Inputs{Query: singleQ("authentication")})
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
	out, err := Run(wd, Inputs{Query: singleQ("middleware")})
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
	out, err := Run(wd, Inputs{Query: singleQ("authentication")})
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
	out, err := Run(wd, Inputs{Query: singleQ("auth-1"), Deep: true})
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
	out, err := Run(wd, Inputs{Query: singleQ("totally-missing-term")})
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
	out, err := Run(wd, Inputs{Query: singleQ("auth"), Limit: 2})
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
	out, err := Run(wd, Inputs{Query: singleQ("JIRA-9999")})
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

func TestQueryMatchesAllOf(t *testing.T) {
	q := Query{Terms: []string{"auth", "refactor"}, Mode: ModeAllOf}
	if !q.Matches("auth refactor middleware") {
		t.Error("expected match for haystack containing both terms")
	}
	if q.Matches("auth only no match") {
		t.Error("expected no match when only one term present")
	}
	if q.Matches("neither here") {
		t.Error("expected no match when no terms present")
	}
}

func TestQueryMatchesAnyOf(t *testing.T) {
	q := Query{Terms: []string{"auth", "security"}, Mode: ModeAnyOf}
	if !q.Matches("auth middleware") {
		t.Error("expected match for haystack containing first term")
	}
	if !q.Matches("security audit") {
		t.Error("expected match for haystack containing second term")
	}
	if q.Matches("neither here") {
		t.Error("expected no match when no terms present")
	}
}

func TestSearchAllOfIndexHit(t *testing.T) {
	// INDEX has a "By tag" line containing both terms on a single line.
	wd := fixtureWorkdir(t, map[string]string{
		"WORK.md": `## Now
- [~] **AUTH-1** — Refactor auth middleware
  - **ID**: auth-1
  - **Tags**: auth, refactor
  - **Started**: 2026-05-15

## Next
## Someday
`,
		"INDEX.md": `# Worklog Index

## By ticket

- auth-1 → WORK.md (Now)

## By tag

- auth, refactor: auth-1

## By repo

## By archive month
`,
	})
	q := Query{Terms: []string{"auth", "refactor"}, Mode: ModeAllOf}
	out, err := Run(wd, Inputs{Query: q})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Hits) != 1 {
		t.Fatalf("expected 1 hit, got %d: %+v", len(out.Hits), out.Hits)
	}
	if out.Hits[0].ID != "auth-1" {
		t.Errorf("expected auth-1, got %q", out.Hits[0].ID)
	}
}

func TestSearchAllOfFullTextFallback(t *testing.T) {
	// INDEX has no single line containing both terms → falls back to full-text
	// where the archive entry body contains both.
	wd := fixtureWorkdir(t, map[string]string{
		"WORK.md": "## Now\n## Next\n## Someday\n",
		"INDEX.md": `# Worklog Index

## By ticket

- sec-cleanup → archive/2026-04.md (archived 2026-04-25)

## By tag

- security: sec-cleanup
- refactor: sec-cleanup

## By repo

## By archive month

- 2026-04 → archive/2026-04.md (1 entry)
`,
		"archive/2026-04.md": `# Archive — 2026-04

## 2026-04-25

### sec-cleanup — Security audit cleanup
- **Tags**: security, refactor
- **Started → Completed**: 2026-04-20 → 2026-04-25
- **Summary**: Tightened auth for security and refactor pass.
`,
	})
	q := Query{Terms: []string{"security", "refactor"}, Mode: ModeAllOf}
	out, err := Run(wd, Inputs{Query: q})
	if err != nil {
		t.Fatal(err)
	}
	if !out.FellBackToFullText {
		t.Errorf("expected full-text fallback")
	}
	if len(out.Hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(out.Hits))
	}
	if out.Hits[0].Source != "fulltext" {
		t.Errorf("source should be fulltext, got %q", out.Hits[0].Source)
	}
}

func TestSearchAnyOfMultipleHits(t *testing.T) {
	// Three blocks, each matching a different any-of term.
	wd := fixtureWorkdir(t, map[string]string{
		"WORK.md": `## Now
- [ ] **T-1** — auth work
  - **ID**: t-1

- [ ] **T-2** — security audit
  - **ID**: t-2

- [ ] **T-3** — middleware cleanup
  - **ID**: t-3

## Next
## Someday
`,
	})
	q := Query{Terms: []string{"auth", "security", "middleware"}, Mode: ModeAnyOf}
	out, err := Run(wd, Inputs{Query: q})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Hits) != 3 {
		t.Fatalf("expected 3 hits, got %d: %+v", len(out.Hits), out.Hits)
	}
}

func TestSearchEmptyTermsRejected(t *testing.T) {
	wd := fixtureWorkdir(t, map[string]string{"WORK.md": "## Now\n## Next\n## Someday\n"})
	_, err := Run(wd, Inputs{Query: Query{Terms: nil, Mode: ModeSingle}})
	if !errors.Is(err, ErrEmptyTerm) {
		t.Errorf("expected ErrEmptyTerm, got %v", err)
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
	out, err := Run(wd, Inputs{Query: singleQ("refactor")})
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
