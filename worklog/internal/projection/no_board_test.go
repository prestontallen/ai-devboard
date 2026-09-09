package projection

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/store"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/memstore"
)

// The renderer was the only producer of `devboard/` keys, and the layout
// was the only thing that redirected them to a second root. Both are gone
// (adb-retire-devboard-dir-2).

func boardless(t *testing.T) store.Store {
	t.Helper()
	s := memstore.New()
	epic := &store.Ticket{
		ID: store.NewID(), Slug: "an-epic", Title: "E", Type: store.TypeEpic,
		State: store.StateActive, Section: store.SectionNext,
		Repo: "ai-devboard", BoardTracked: true, Phase: "implementing",
		PlanSteps: []store.PlanStep{{Text: "a step", State: "done", Rank: 1}},
	}
	if err := s.PutTicket(epic); err != nil {
		t.Fatal(err)
	}
	for _, tk := range []*store.Ticket{
		{Slug: "a-kid", Title: "K", Type: store.TypeTicket, State: store.StateActive,
			ParentID: epic.ID, BoardTracked: true},
		{Slug: "archived-card", Title: "A", Type: store.TypeTicket, State: store.StateActive,
			Section: store.SectionNow, Repo: "ai-devboard", BoardTracked: true, BoardArchived: true},
		{Slug: "no-repo", Title: "U", Type: store.TypeTicket, State: store.StateActive,
			Section: store.SectionNow, BoardTracked: true},
	} {
		if err := s.PutTicket(tk); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

// Nothing the renderer emits is addressed under a board root any more,
// including for the shapes that used to produce the trickiest paths: an
// archived card, a ticket with no repo, and an epic with a child.
func TestRenderEmitsNoBoardKeys(t *testing.T) {
	files, err := Render(boardless(t))
	if err != nil {
		t.Fatal(err)
	}
	for rel := range files {
		if strings.HasPrefix(rel, "devboard/") || strings.HasSuffix(rel, ".yaml") {
			t.Errorf("render emitted %q", rel)
		}
	}
	if _, ok := files["WORK.md"]; !ok {
		t.Fatal("render produced no WORK.md, so this proves nothing")
	}
}

// The ordering trap, as a test. writeIfChanged calls MkdirAll on a path's
// parent, so a layout that resolved a key to a relative path would create
// a tree wherever the process happened to be standing. Deleting the board
// directory without first dropping the root from render was self-undoing
// for exactly this reason.
func TestNoWriteCreatesARelativeTree(t *testing.T) {
	elsewhere := t.TempDir()
	t.Chdir(elsewhere)

	root := t.TempDir()
	if err := RenderTo(boardless(t), Layout{WorklogDir: root}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(elsewhere)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("a render created %v relative to the working directory", names)
	}
	if _, err := os.Stat(filepath.Join(root, "WORK.md")); err != nil {
		t.Fatalf("the render did not land in the root it was given: %v", err)
	}
}

// A layout has one root. This is the guard on the shape rather than on a
// behaviour: blanking a second root used to yield relative paths, so the
// fix was to remove it, not to leave it empty.
func TestLayoutHasOneRoot(t *testing.T) {
	root := t.TempDir()
	l := SingleRoot(root)
	if l.WorklogDir != root {
		t.Errorf("SingleRoot(%q).WorklogDir = %q", root, l.WorklogDir)
	}
	if got := l.path("notes/a.md"); got != filepath.Join(root, "notes", "a.md") {
		t.Errorf("path = %q, want it under the root", got)
	}
}
