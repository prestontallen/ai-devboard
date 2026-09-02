package render

import (
	"errors"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/parse"
)

// twoBlockDoc has a ticket followed by a second block, so tests can prove an
// appended field lands inside the first block rather than after the blank line
// that separates them.
const twoBlockDoc = `## Now

- [~] **AUTH-1** — Refactor auth
  - **ID**: auth-1
  - **Repo**: acme/api
  - **PR**:
  - **Started**: 2026-05-10
  - **Acceptance**: login works

- [~] **AUTH-2** — Second ticket
  - **ID**: auth-2
  - **PR**:

## Next

## Someday
`

func mustParse(t *testing.T, src string) (lines []string) {
	t.Helper()
	return strings.Split(strings.TrimSuffix(src, "\n"), "\n")
}

func setField(t *testing.T, src, id, field, value string) string {
	t.Helper()
	doc, err := parse.Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := SetBlockField(doc, id, field, value)
	if err != nil {
		t.Fatalf("SetBlockField(%q, %q): %v", field, value, err)
	}
	return strings.Join(out, "\n")
}

// assertOnlyChange checks that out differs from src by exactly the expected
// added and removed lines, which is how the "every untouched line is preserved
// byte-for-byte" promise gets enforced.
func assertOnlyChange(t *testing.T, src, out string, added, removed []string) {
	t.Helper()
	before := mustParse(t, src)
	after := strings.Split(out, "\n")

	gotAdded := diffLines(after, before)
	gotRemoved := diffLines(before, after)

	if !equalStrings(gotAdded, added) {
		t.Errorf("added lines = %q, want %q", gotAdded, added)
	}
	if !equalStrings(gotRemoved, removed) {
		t.Errorf("removed lines = %q, want %q", gotRemoved, removed)
	}
}

// diffLines returns the lines of a that are not present in b, counting
// duplicates.
func diffLines(a, b []string) []string {
	remaining := make(map[string]int, len(b))
	for _, l := range b {
		remaining[l]++
	}
	var out []string
	for _, l := range a {
		if remaining[l] > 0 {
			remaining[l]--
			continue
		}
		out = append(out, l)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// lineIndex returns the 0-based index of the first line equal to want, or -1.
func lineIndex(lines []string, want string) int {
	for i, l := range lines {
		if l == want {
			return i
		}
	}
	return -1
}

func TestSetBlockFieldInsertsInCanonicalPosition(t *testing.T) {
	// Status ranks after Acceptance, so it appends at the end of the block —
	// the case that would land after the blank separator without metaBounds
	// trimming.
	out := setField(t, twoBlockDoc, "auth-1", "Status", "in review")
	assertOnlyChange(t, twoBlockDoc, out, []string{"  - **Status**: in review"}, nil)

	lines := strings.Split(out, "\n")
	iStatus := lineIndex(lines, "  - **Status**: in review")
	iAcceptance := lineIndex(lines, "  - **Acceptance**: login works")
	iNext := lineIndex(lines, "- [~] **AUTH-2** — Second ticket")
	if iStatus != iAcceptance+1 {
		t.Errorf("Status at %d, want directly after Acceptance at %d:\n%s", iStatus, iAcceptance, out)
	}
	if iStatus > iNext {
		t.Errorf("Status leaked past the next block:\n%s", out)
	}
}

func TestSetBlockFieldInsertsBeforeHigherRankedField(t *testing.T) {
	// Tags ranks between Repo and PR.
	out := setField(t, twoBlockDoc, "auth-1", "Tags", "auth, api")
	lines := strings.Split(out, "\n")

	iRepo := lineIndex(lines, "  - **Repo**: acme/api")
	iTags := lineIndex(lines, "  - **Tags**: auth, api")
	iPR := lineIndex(lines, "  - **PR**:")
	if !(iRepo < iTags && iTags < iPR) {
		t.Errorf("want Repo < Tags < PR, got %d < %d < %d:\n%s", iRepo, iTags, iPR, out)
	}
}

func TestSetBlockFieldRewrite(t *testing.T) {
	out := setField(t, twoBlockDoc, "auth-1", "Acceptance", "logout works too")
	assertOnlyChange(t, twoBlockDoc, out,
		[]string{"  - **Acceptance**: logout works too"},
		[]string{"  - **Acceptance**: login works"})

	if n := strings.Count(out, "**Acceptance**"); n != 1 {
		t.Errorf("Acceptance appears %d times, want 1:\n%s", n, out)
	}
	lines := strings.Split(out, "\n")
	if got, want := lineIndex(lines, "  - **Acceptance**: logout works too"), 7; got != want {
		t.Errorf("rewritten line moved to index %d, want %d:\n%s", got, want, out)
	}
}

func TestSetBlockFieldEmptyValueRemovesLine(t *testing.T) {
	out := setField(t, twoBlockDoc, "auth-1", "Acceptance", "")
	assertOnlyChange(t, twoBlockDoc, out, nil, []string{"  - **Acceptance**: login works"})
}

func TestSetBlockFieldEmptyValueOnAbsentFieldIsNoOp(t *testing.T) {
	out := setField(t, twoBlockDoc, "auth-1", "Status", "")
	if out != strings.TrimSuffix(twoBlockDoc, "\n") {
		t.Errorf("clearing an absent field changed the document:\n%s", out)
	}
}

func TestSetBlockFieldIsCaseInsensitive(t *testing.T) {
	out := setField(t, twoBlockDoc, "auth-1", "acceptance", "lowercase name")
	if !strings.Contains(out, "  - **Acceptance**: lowercase name") {
		t.Errorf("field name not canonicalized:\n%s", out)
	}
}

func TestSetBlockFieldStepsOverUnknownFields(t *testing.T) {
	src := `## Now

- [~] **AUTH-1** — Refactor auth
  - **ID**: auth-1
  - **Whatever**: hand written

## Next
`
	out := setField(t, src, "auth-1", "Status", "in review")
	lines := strings.Split(out, "\n")
	iUnknown := lineIndex(lines, "  - **Whatever**: hand written")
	iStatus := lineIndex(lines, "  - **Status**: in review")
	if iUnknown < 0 {
		t.Fatalf("unknown field was dropped:\n%s", out)
	}
	if iStatus < iUnknown {
		t.Errorf("unknown field displaced Status; want Status after it:\n%s", out)
	}
}

func TestSetBlockFieldUnknownField(t *testing.T) {
	doc, err := parse.Bytes("WORK.md", []byte(twoBlockDoc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := SetBlockField(doc, "auth-1", "Nonsense", "x"); !errors.Is(err, ErrUnknownField) {
		t.Errorf("err = %v, want ErrUnknownField", err)
	}
}

func TestSetBlockFieldUnknownBlock(t *testing.T) {
	doc, err := parse.Bytes("WORK.md", []byte(twoBlockDoc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := SetBlockField(doc, "nope", "Status", "x"); !errors.Is(err, ErrBlockNotFound) {
		t.Errorf("err = %v, want ErrBlockNotFound", err)
	}
}

func TestSetBlockTitle(t *testing.T) {
	doc, err := parse.Bytes("WORK.md", []byte(twoBlockDoc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := SetBlockTitle(doc, "auth-1", "Rework auth end to end")
	if err != nil {
		t.Fatalf("SetBlockTitle: %v", err)
	}
	joined := strings.Join(out, "\n")
	assertOnlyChange(t, twoBlockDoc, joined,
		[]string{"- [~] **AUTH-1** — Rework auth end to end"},
		[]string{"- [~] **AUTH-1** — Refactor auth"})
}

func TestSetBlockTitleAcceptsDoubleDashSeparator(t *testing.T) {
	src := "## Now\n\n- [ ] **AUTH-1** -- Old title\n  - **ID**: auth-1\n\n## Next\n"
	doc, err := parse.Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := SetBlockTitle(doc, "auth-1", "New title")
	if err != nil {
		t.Fatalf("SetBlockTitle: %v", err)
	}
	if got := out[2]; got != "- [ ] **AUTH-1** -- New title" {
		t.Errorf("bullet = %q, want the -- separator preserved", got)
	}
}

func TestSetBlockTitleWithoutSeparator(t *testing.T) {
	src := "## Now\n\n- [ ] bare title\n  - **ID**: auth-1\n\n## Next\n"
	doc, err := parse.Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := SetBlockTitle(doc, "auth-1", "New"); !errors.Is(err, ErrNoTitleSeparator) {
		t.Errorf("err = %v, want ErrNoTitleSeparator", err)
	}
}

func TestFieldOrderCoversFormatters(t *testing.T) {
	// Every field either formatter emits must be rankable, or SetBlockField
	// would treat a freshly rendered line as unknown.
	ticket := FormatTicketBlock(BlockOptions{
		Title: "T", ID: "t-1", Type: "ticket", Parent: "e-1", Repo: "r",
		Tags: []string{"a"}, Started: "2026-01-01", PR: "u", Source: "s",
		Files: []string{"f"}, Acceptance: "a", NotesRef: "n", Status: "st",
		WaitingSince: "2026-01-02",
	})
	epic := FormatEpicBlock(EpicBlockOptions{
		Title: "E", ID: "e-1", Repo: "r", Tags: []string{"a"},
		NotesRef: "n", Plan: "p", ActiveChildren: []string{"t-1"}, Status: "st",
	})

	for _, lines := range [][]string{ticket, epic} {
		last := -1
		for _, l := range lines {
			m := metaFieldRe.FindStringSubmatch(l)
			if m == nil {
				continue
			}
			r := fieldRank(m[1])
			if r < 0 {
				t.Errorf("formatter emits %q, which fieldOrder doesn't rank", m[1])
				continue
			}
			if r <= last {
				t.Errorf("field %q ranks %d, not after the previous field's %d — fieldOrder disagrees with the formatter", m[1], r, last)
			}
			last = r
		}
	}
}
