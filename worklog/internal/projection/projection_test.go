package projection

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/prestontallen/ai-devboard/worklog/internal/convert"
	"github.com/prestontallen/ai-devboard/worklog/internal/feedback"
	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/parse"
	"github.com/prestontallen/ai-devboard/worklog/internal/reindex"
	"github.com/prestontallen/ai-devboard/worklog/internal/serve"
	"github.com/prestontallen/ai-devboard/worklog/internal/standup"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/memstore"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/sqlitestore"
)

const corpusDir = "../convert/testdata/corpus"

func loadCorpus(t *testing.T, s store.Store, dir string) {
	t.Helper()
	c, err := convert.ReadCorpusDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := convert.Load(s, c); err != nil {
		t.Fatal(err)
	}
}

func impls(t *testing.T) map[string]store.Store {
	t.Helper()
	sq, err := sqlitestore.Open(filepath.Join(t.TempDir(), "w.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sq.Close() })
	return map[string]store.Store{"memstore": memstore.New(), "sqlite": sq}
}

// normalized strips minted identity so aggregates from two independent
// conversions compare: IDs zeroed, ParentID replaced by the parent slug.
func normalized(t *testing.T, s store.Store) []byte {
	t.Helper()
	tickets, err := s.Tickets()
	if err != nil {
		t.Fatal(err)
	}
	bySlugOf := map[store.ID]string{}
	for _, tk := range tickets {
		bySlugOf[tk.ID] = tk.Slug
	}
	for _, tk := range tickets {
		tk.ID = ""
		if tk.ParentID != "" {
			parent := bySlugOf[tk.ParentID]
			tk.ParentID = ""
			tk.ExtraFields = map[string]string{"parent": parent}
		}
		zeroIDs(tk)
	}
	sort.Slice(tickets, func(i, j int) bool {
		if tickets[i].Slug != tickets[j].Slug {
			return tickets[i].Slug < tickets[j].Slug
		}
		return tickets[i].Title < tickets[j].Title
	})
	fb, err := s.Feedback()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range fb {
		e.ID = ""
	}
	out, err := json.MarshalIndent(map[string]any{"tickets": tickets, "feedback": fb}, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func zeroIDs(tk *store.Ticket) {
	for i := range tk.PlanSteps {
		tk.PlanSteps[i].ID = ""
	}
	for i := range tk.Scorecard {
		tk.Scorecard[i].ID = ""
	}
	for i := range tk.Decisions {
		tk.Decisions[i].ID = ""
	}
	for i := range tk.CodeRefs {
		tk.CodeRefs[i].ID = ""
	}
	for i := range tk.NeedsYou {
		tk.NeedsYou[i].ID = ""
	}
	for i := range tk.WaitingOn {
		tk.WaitingOn[i].ID = ""
	}
	for i := range tk.Links {
		tk.Links[i].ID = ""
	}
	for i := range tk.Transitions {
		tk.Transitions[i].ID = ""
	}
	for i := range tk.NoteEntries {
		tk.NoteEntries[i].ID = ""
	}
}

// TestSemanticRoundTrip is criterion 1: corpus → store → render → re-parse
// → second store, and the two stores hold the same facts. Runs against
// both implementations (criterion 15).
func TestSemanticRoundTrip(t *testing.T) {
	for name, s1 := range impls(t) {
		t.Run(name, func(t *testing.T) {
			loadCorpus(t, s1, corpusDir)
			dir := t.TempDir()
			if err := RenderAll(s1, dir); err != nil {
				t.Fatal(err)
			}
			s2 := memstore.New()
			loadCorpus(t, s2, dir)

			want, got := normalized(t, s1), normalized(t, s2)
			if !bytes.Equal(want, got) {
				t.Errorf("round-trip drift:\n--- direct conversion\n%s\n--- via projections\n%s", want, got)
			}
		})
	}
}

// TestRenderFixpoint is criterion 2's fixpoint half: render∘parse∘render
// is byte-stable.
func TestRenderFixpoint(t *testing.T) {
	s1 := memstore.New()
	loadCorpus(t, s1, corpusDir)
	dir1 := t.TempDir()
	if err := RenderAll(s1, dir1); err != nil {
		t.Fatal(err)
	}
	s2 := memstore.New()
	loadCorpus(t, s2, dir1)
	dir2 := t.TempDir()
	if err := RenderAll(s2, dir2); err != nil {
		t.Fatal(err)
	}
	compareTrees(t, dir1, dir2)
}

func compareTrees(t *testing.T, a, b string) {
	t.Helper()
	list := func(root string) map[string][]byte {
		out := map[string][]byte{}
		filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			out[rel] = data
			return nil
		})
		return out
	}
	am, bm := list(a), list(b)
	for rel, data := range am {
		if !bytes.Equal(bm[rel], data) {
			t.Errorf("fixpoint drift in %s:\n--- first\n%s\n--- second\n%s", rel, data, bm[rel])
		}
		delete(bm, rel)
	}
	for rel := range bm {
		t.Errorf("second render created extra file %s", rel)
	}
}

// TestFreshness is criterion 13: re-rendering unchanged content must not
// touch any file's mtime, or the frozen SSE behavior fires on no-ops.
func TestFreshness(t *testing.T) {
	s := memstore.New()
	loadCorpus(t, s, corpusDir)
	dir := t.TempDir()
	if err := RenderAll(s, dir); err != nil {
		t.Fatal(err)
	}
	mtimes := map[string]time.Time{}
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, _ := d.Info()
		mtimes[path] = info.ModTime()
		return nil
	})
	time.Sleep(10 * time.Millisecond)
	if err := RenderAll(s, dir); err != nil {
		t.Fatal(err)
	}
	for path, was := range mtimes {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if !info.ModTime().Equal(was) {
			t.Errorf("%s rewritten despite identical content", path)
		}
	}
}

// TestOracles is criterion 2: the real readers accept the projections and
// see the same facts they see in the originals.
func TestOracles(t *testing.T) {
	s := memstore.New()
	loadCorpus(t, s, corpusDir)
	dir := t.TempDir()
	if err := RenderAll(s, dir); err != nil {
		t.Fatal(err)
	}

	t.Run("parse.File", func(t *testing.T) {
		orig, err := parse.File(filepath.Join(corpusDir, "WORK.md"))
		if err != nil {
			t.Fatal(err)
		}
		rendered, err := parse.File(filepath.Join(dir, "WORK.md"))
		if err != nil {
			t.Fatal(err)
		}
		type view struct {
			State, Type, Parent, Repo, Started, PR, Acceptance, Title string
			Tags                                                      []string
			Section                                                   model.SectionName
		}
		collect := func(doc *model.WorkDoc) map[string]view {
			out := map[string]view{}
			for _, sec := range doc.Sections {
				for _, b := range sec.Blocks {
					key := b.ID
					if key == "" {
						key = "\x00" + b.Title
					}
					out[key] = view{
						State: string(b.State), Type: string(b.Type), Parent: b.Parent,
						Repo: b.Repo, Started: b.Started, PR: b.PR,
						Acceptance: b.Acceptance, Title: b.Title, Tags: b.Tags,
						Section: sec.Name,
					}
				}
			}
			return out
		}
		o, r := collect(orig), collect(rendered)
		if len(o) != len(r) {
			t.Fatalf("block counts differ: %d vs %d", len(o), len(r))
		}
		for id, ov := range o {
			rv, ok := r[id]
			if !ok {
				t.Errorf("block %q missing from rendered WORK.md", id)
				continue
			}
			oj, _ := json.Marshal(ov)
			rj, _ := json.Marshal(rv)
			if !bytes.Equal(oj, rj) {
				t.Errorf("block %q drifted: %s vs %s", id, oj, rj)
			}
		}
	})

	t.Run("standup.ParseFile", func(t *testing.T) {
		orig, err := standup.ParseFile(filepath.Join(corpusDir, "archive", "2026-09.md"), "archive/2026-09.md")
		if err != nil {
			t.Fatal(err)
		}
		rendered, err := standup.ParseFile(filepath.Join(dir, "archive", "2026-09.md"), "archive/2026-09.md")
		if err != nil {
			t.Fatal(err)
		}
		if len(orig) != len(rendered) {
			t.Fatalf("archive entry counts differ: %d vs %d", len(orig), len(rendered))
		}
		byID := map[string]standup.Entry{}
		for _, e := range rendered {
			byID[e.ID] = e
		}
		for _, e := range orig {
			if _, ok := byID[e.ID]; !ok {
				t.Errorf("archive entry %s missing from projection", e.ID)
			}
		}
	})

	t.Run("reindex.Run", func(t *testing.T) {
		wd, err := model.NewWorkdir(dir)
		if err != nil {
			t.Fatal(err)
		}
		out, err := reindex.Run(wd, reindex.Inputs{})
		if err != nil {
			t.Fatal(err)
		}
		// 6 sluged tickets total (bare one has no slug): 4 live + 2 archived.
		if out.Entries.ByTicket == 0 || out.Entries.ByArchiveMonth != 1 {
			t.Errorf("reindex counts off: %+v", out.Entries)
		}
	})

	t.Run("feedback.Parse", func(t *testing.T) {
		orig, err := feedback.Parse(filepath.Join(corpusDir, "FEEDBACK.md"))
		if err != nil {
			t.Fatal(err)
		}
		rendered, err := feedback.Parse(filepath.Join(dir, "FEEDBACK.md"))
		if err != nil {
			t.Fatal(err)
		}
		oj, _ := json.Marshal(orig)
		rj, _ := json.Marshal(rendered)
		if !bytes.Equal(oj, rj) {
			t.Errorf("feedback drifted:\n%s\nvs\n%s", oj, rj)
		}
	})

	t.Run("serve payload", func(t *testing.T) {
		srv := serve.New(serve.Config{
			DataDir:    filepath.Join(dir, "devboard"),
			WorklogDir: dir,
		})
		ts := httptest.NewServer(srv.Handler())
		defer ts.Close()
		resp, err := http.Get(ts.URL + "/api/tasks")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var payload struct {
			Repos []struct {
				Repo  string `json:"repo"`
				Tasks []struct {
					ID    string         `json:"id"`
					Notes string         `json:"notes"`
					Task  map[string]any `json:"task"`
				} `json:"tasks"`
			} `json:"repos"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if len(payload.Repos) != 1 || payload.Repos[0].Repo != "ai-devboard" {
			t.Fatalf("groups: %+v", payload.Repos)
		}
		task := payload.Repos[0].Tasks[0]
		if task.ID != "an-epic" || task.Task["custom_top"] != "survives" {
			t.Errorf("unknown-key passthrough failed: %+v", task.Task)
		}
		if !strings.Contains(task.Notes, "scaffold comment stays verbatim") {
			t.Error("notes join missing from payload")
		}
		kids, _ := task.Task["children"].([]any)
		if len(kids) != 2 {
			t.Fatalf("children: %v", task.Task["children"])
		}
		kid := kids[0].(map[string]any)
		if kid["phase"] != "implementing" {
			t.Errorf("child phase: %v", kid["phase"])
		}
		plan := kid["plan"].([]any)[0].(map[string]any)
		if plan["surprise_key"] != "kept" {
			t.Errorf("per-item unknown key lost in feed: %v", plan)
		}
	})
}

// TestBannerOnMarkdownSurfaces: the generated-file marker lands on the
// markdown surfaces whose parsers tolerate it, and stays off the two that
// don't (FEEDBACK.md is parsed by the legacy feedback package, devboard
// files are YAML, not markdown).
func TestBannerOnMarkdownSurfaces(t *testing.T) {
	s := memstore.New()
	loadCorpus(t, s, corpusDir)
	files, err := Render(s)
	if err != nil {
		t.Fatal(err)
	}

	var sawNotes, sawArchive, sawBoard bool
	for rel, content := range files {
		has := bytes.HasPrefix(content, []byte(Banner+"\n"))
		switch {
		case rel == "WORK.md", strings.HasPrefix(rel, "notes/"), strings.HasPrefix(rel, "archive/"):
			if !has {
				t.Errorf("%s: want a banner, got none", rel)
			}
			sawNotes = sawNotes || strings.HasPrefix(rel, "notes/")
			sawArchive = sawArchive || strings.HasPrefix(rel, "archive/")
		case rel == "FEEDBACK.md", strings.HasPrefix(rel, "devboard/"):
			if has {
				t.Errorf("%s: banner must not be emitted here", rel)
			}
			sawBoard = sawBoard || strings.HasPrefix(rel, "devboard/")
		}
	}
	// Guard against the assertions above passing vacuously.
	if _, ok := files["WORK.md"]; !ok {
		t.Fatal("corpus rendered no WORK.md")
	}
	if !sawNotes || !sawArchive || !sawBoard {
		t.Fatalf("corpus did not exercise every surface: notes=%v archive=%v board=%v",
			sawNotes, sawArchive, sawBoard)
	}
}

// TestEditedFilesNamesHandEdits is the hand-edit guard: a write must be
// able to tell that a projection changed under it, so it can refuse
// instead of silently overwriting the edit.
func TestEditedFilesNamesHandEdits(t *testing.T) {
	s := memstore.New()
	loadCorpus(t, s, corpusDir)
	dir := t.TempDir()
	if err := RenderAll(s, dir); err != nil {
		t.Fatal(err)
	}

	// Freshly rendered: nothing was edited.
	edited, err := EditedFiles(s, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(edited) != 0 {
		t.Fatalf("clean render reported edits: %v", edited)
	}

	// A file the store does not own must never be flagged — this is what
	// keeps hand-written devboard producer files and INDEX.md honored.
	if err := os.WriteFile(filepath.Join(dir, "INDEX.md"), []byte("hand-written\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if edited, err = EditedFiles(s, dir); err != nil || len(edited) != 0 {
		t.Fatalf("unowned file flagged as edited: %v (err %v)", edited, err)
	}

	// Now actually edit a projection.
	work := filepath.Join(dir, "WORK.md")
	data, err := os.ReadFile(work)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(work, append(data, []byte("\n- [ ] **SNUCK-IN** — typed by hand\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	edited, err = EditedFiles(s, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(edited) != 1 || edited[0] != "WORK.md" {
		t.Fatalf("want [WORK.md], got %v", edited)
	}

	// A deleted projection is an edit too: re-rendering would resurrect it
	// without anyone noticing it had been removed.
	if err := os.Remove(work); err != nil {
		t.Fatal(err)
	}
	if edited, err = EditedFiles(s, dir); err != nil || len(edited) != 1 || edited[0] != "WORK.md" {
		t.Fatalf("deleted projection not reported: %v (err %v)", edited, err)
	}
}
