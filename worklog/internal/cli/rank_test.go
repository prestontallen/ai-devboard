package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sectionEntries returns the bold IDs of the ticket blocks under one
// WORK.md section heading, in rendered document order.
func sectionEntries(t *testing.T, live, heading string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(live, "WORK.md"))
	if err != nil {
		t.Fatalf("reading WORK.md: %v", err)
	}
	var out []string
	in := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "## ") {
			in = strings.TrimSpace(strings.TrimPrefix(line, "## ")) == heading
			continue
		}
		if !in || !strings.HasPrefix(line, "- [") {
			continue
		}
		if a := strings.Index(line, "**"); a >= 0 {
			if b := strings.Index(line[a+2:], "**"); b >= 0 {
				out = append(out, line[a+2:a+2+b])
				continue
			}
		}
		out = append(out, strings.TrimSpace(line)) // bare title-only block
	}
	return out
}

// TestAddRanksToEndOfSection pins the Rank collision fix.
//
// Rank was assigned only by convert (convert/workmd.go:73), which numbers
// document position from ZERO — so a ticket created through the CLI kept
// the zero value and thereby *tied* whichever ticket sits first in the
// document. Tickets() breaks that tie with `ORDER BY rank, slug`, so the
// new ticket's placement was decided by alphabetical luck rather than by
// where the human put it.
//
// "aaa-new" sorts before the fixture's existing "solo", so under the old
// behavior it wins the tiebreak and lands at the TOP of ## Next. The
// assertion is that it lands last.
func TestAddRanksToEndOfSection(t *testing.T) {
	live, _, _ := storeWriteFixture(t)

	if _, stderr := runCLI(t, "add", "--dir", live,
		"--id", "aaa-new", "--title", "Added last, sorts first by slug"); strings.Contains(stderr, "error") {
		t.Fatalf("add: %s", stderr)
	}

	got := sectionEntries(t, live, "Next")
	want := []string{"SOLO", "AAA-NEW"}
	if len(got) != len(want) {
		t.Fatalf("## Next = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("## Next = %v, want %v (a rank-0 tie sorts AAA-NEW to the top)", got, want)
		}
	}

	// The other half of the criterion: the corpus is still a fixed point
	// of the renderer afterwards, so the next write is not refused. This
	// is what makes the fix a prerequisite for adoption rather than a
	// cosmetic ordering nicety.
	stdout, stderr := runCLI(t, "verify", "--dir", live)
	if !strings.Contains(stdout+stderr, "clean") {
		t.Errorf("verify after add: %s%s", stdout, stderr)
	}
}

// TestAddKeepsInsertionOrderAcrossAdds checks the rule is "one past the
// highest", not merely "non-zero": two successive adds must stay in the
// order they were created even though their slugs sort the other way.
func TestAddKeepsInsertionOrderAcrossAdds(t *testing.T) {
	live, _, _ := storeWriteFixture(t)

	for _, id := range []string{"zzz-first", "aaa-second"} {
		if _, stderr := runCLI(t, "add", "--dir", live, "--id", id, "--title", "x"); strings.Contains(stderr, "error") {
			t.Fatalf("add %s: %s", id, stderr)
		}
	}

	got := sectionEntries(t, live, "Next")
	want := []string{"SOLO", "ZZZ-FIRST", "AAA-SECOND"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("## Next = %v, want %v", got, want)
	}
}

// TestAddChildRanksToEndOfRoster is the same collision one level down:
// RosterRank is assigned only by convert (convert.go:161,181), so a child
// added through the CLI tied the epic's first child at 0 and was ordered
// against it by slug. "aaa-kid" sorts before the fixture's "kid-live".
func TestAddChildRanksToEndOfRoster(t *testing.T) {
	live, board, _ := storeWriteFixture(t)

	if _, stderr := runCLI(t, "add", "--dir", live,
		"--parent", "an-epic", "--id", "aaa-kid", "--title", "Newest child"); strings.Contains(stderr, "error") {
		t.Fatalf("add --parent: %s", stderr)
	}

	matches, err := filepath.Glob(filepath.Join(board, "*", "an-epic.yaml"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("locating the epic's board file: %v (matches %v)", err, matches)
	}
	repo := filepath.Base(filepath.Dir(matches[0]))

	var got []string
	for _, c := range readBoard(t, board, repo, "an-epic").Children {
		got = append(got, c.ID)
	}
	if len(got) == 0 || got[len(got)-1] != "aaa-kid" {
		t.Errorf("roster = %v; aaa-kid must land last, not win a RosterRank-0 tie", got)
	}
}
