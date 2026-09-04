package adopt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/convert"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/memstore"
)

// TestAdoptRealPreCutoverCorpus is criterion 15: adopt a COPY of the real
// pre-cutover backup and check the outcome against what the hand
// canonicalisation produced.
//
// It asserts three things, in increasing strength:
//   - adoption succeeds and the post-condition holds, so the corpus is a
//     render fixpoint and the next write verb would not refuse;
//   - re-running plans no writes, so adoption converges;
//   - nothing was lost: the store converted from the ADOPTED corpus is
//     canonically identical to the store converted from the ORIGINAL.
func TestAdoptRealPreCutoverCorpus(t *testing.T) {
	src := os.Getenv("WORKLOG_SNAPSHOT")
	if src == "" {
		t.Skip("set WORKLOG_SNAPSHOT to run the acceptance gate")
	}
	board := os.Getenv("DEVBOARD_SNAPSHOT")

	root := t.TempDir()
	r := Roots{Worklog: filepath.Join(root, "worklog")}
	if err := copyTree(src, r.Worklog, "", map[string]string{}); err != nil {
		t.Fatal(err)
	}
	if board != "" {
		r.Devboard = filepath.Join(root, "devboard")
		if err := copyTree(board, r.Devboard, "", map[string]string{}); err != nil {
			t.Fatal(err)
		}
	}

	// The real pre-cutover corpus carries one hazard: csk-integration's
	// board title lacks the suffix its ticket has, and boardFragment never
	// reads a top-level title, so a render would silently replace it.
	// Adoption must REFUSE rather than rewrite it. That refusal is the
	// first assertion.
	if _, err := Run(memstore.New(), Options{Roots: r, SnapshotDir: filepath.Join(root, "refuse")}); err == nil {
		t.Error("adoption accepted a corpus carrying a silent-alteration construct")
	} else if !strings.Contains(err.Error(), "csk-integration") {
		t.Errorf("refusal did not name the offending file: %v", err)
	}

	// Resolve it the way an operator would, then adopt for real.
	fixTitle(t, r, "csk-integration")

	// The store the ORIGINAL corpus converts to, before anything is written.
	orig := memstore.New()
	before, err := Run(orig, Options{Roots: r, SnapshotDir: filepath.Join(root, "dry")})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	t.Logf("plan: %v", before.Plan.Counts())
	wantCanon, err := store.Canonical(orig)
	if err != nil {
		t.Fatal(err)
	}

	res, err := Run(memstore.New(), Options{Roots: r, SnapshotDir: filepath.Join(root, "snap"), Apply: true})
	if err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if !res.Applied {
		t.Fatal("Applied = false")
	}
	t.Logf("snapshot: %s", res.Snapshot.Describe())

	// Converges: a second run plans no writes.
	again, err := Run(memstore.New(), Options{Roots: r, SnapshotDir: filepath.Join(root, "dry2")})
	if err != nil {
		t.Fatalf("second dry run: %v", err)
	}
	if again.Plan.Writes() {
		for _, c := range again.Plan.Changes {
			if c.Op != OpKeep && c.Op != OpProduce && c.Op != OpDerived {
				t.Errorf("adoption did not converge: still plans %s", c)
			}
		}
	}

	// Nothing lost: the adopted corpus converts to the same store.
	staged, cleanup, err := stage(r)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	after := memstore.New()
	c, err := convert.ReadCorpusDir(staged)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := convert.Load(after, c); err != nil {
		t.Fatal(err)
	}
	gotCanon, err := store.Canonical(after)
	if err != nil {
		t.Fatal(err)
	}
	// Adoption legitimately HEALS one thing beyond formatting and file
	// placement: an epic's board file gains any children a stale roster
	// omitted, so re-reading marks those children BoardTracked. That is the
	// epic-roster-children fix adb-cutover recorded, not a loss. So the
	// assertion is precise rather than blanket: everything apart from
	// BoardTracked must be identical, and BoardTracked may only go false to
	// true — losing board presence silently would be real damage.
	if a, b := stripTracked(string(wantCanon)), stripTracked(string(gotCanon)); a != b {
		wl, gl := strings.Split(a, "\n"), strings.Split(b, "\n")
		shown := 0
		for i := 0; i < len(wl) && i < len(gl) && shown < 6; i++ {
			if wl[i] != gl[i] {
				t.Errorf("adoption changed the corpus semantically at line %d:\n  before: %s\n   after: %s",
					i+1, strings.TrimSpace(wl[i]), strings.TrimSpace(gl[i]))
				shown++
			}
		}
		if shown == 0 {
			t.Errorf("canonical documents differ in length: %d vs %d lines", len(wl), len(gl))
		}
	}
	gained, lost := trackedDelta(string(wantCanon), string(gotCanon))
	t.Logf("board tracking healed on %d ticket(s)", gained)
	if lost > 0 {
		t.Errorf("adoption silently removed board tracking from %d ticket(s)", lost)
	}
}

// fixTitle drops the unread top-level title from a board file, which is how
// an operator resolves a devboard-title-mismatch: the ticket is the source
// of a title, and the board copy is never read.
func fixTitle(t *testing.T, r Roots, slug string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(r.Devboard, "*", slug+".yaml"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("locating %s: %v (matches %v)", slug, err, matches)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	var kept []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "title:") {
			continue
		}
		kept = append(kept, line)
	}
	if err := os.WriteFile(matches[0], []byte(strings.Join(kept, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
}

// stripTracked blanks the BoardTracked value so the rest of the document
// can be compared exactly.
func stripTracked(doc string) string {
	lines := strings.Split(doc, "\n")
	for i, l := range lines {
		if strings.Contains(l, `"BoardTracked"`) {
			lines[i] = `  "BoardTracked": _,`
		}
	}
	return strings.Join(lines, "\n")
}

// trackedDelta counts BoardTracked transitions between two canonical docs.
func trackedDelta(before, after string) (gained, lost int) {
	bl, al := strings.Split(before, "\n"), strings.Split(after, "\n")
	for i := 0; i < len(bl) && i < len(al); i++ {
		if !strings.Contains(bl[i], `"BoardTracked"`) || bl[i] == al[i] {
			continue
		}
		if strings.Contains(bl[i], "false") && strings.Contains(al[i], "true") {
			gained++
		}
		if strings.Contains(bl[i], "true") && strings.Contains(al[i], "false") {
			lost++
		}
	}
	return gained, lost
}
