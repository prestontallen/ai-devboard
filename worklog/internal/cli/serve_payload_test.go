package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/convert"
	"github.com/prestontallen/ai-devboard/worklog/internal/serve"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/memstore"
)

const payloadCorpus = "../convert/testdata/corpus"

// seedFromCorpus loads the shared convert corpus into a store: epics with
// children, notes, an archived board entry, unknown keys at both the top
// level and inside a plan item.
func seedFromCorpus(t *testing.T) store.Store {
	t.Helper()
	s := memstore.New()
	c, err := convert.ReadCorpusDir(payloadCorpus)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := convert.Load(s, c); err != nil {
		t.Fatal(err)
	}
	// The shared corpus holds exactly one board-tracked top-level ticket,
	// in one repo, with no notes on any child. That leaves four things the
	// payload does unexercised: the _archive/ path segment, the live-
	// before-archived order within a repo, the "unknown" group an
	// unattributed ticket falls into, and child-notes attachment. They are
	// added here rather than to the corpus, which other packages depend on.
	tickets, err := s.Tickets()
	if err != nil {
		t.Fatal(err)
	}
	var archivedOne bool
	for _, tk := range tickets {
		if tk.BoardTracked && tk.ParentID == "" && !archivedOne {
			tk.BoardArchived = true
			if err := s.PutTicket(tk); err != nil {
				t.Fatal(err)
			}
			archivedOne = true
		}
		// A child with notes of its own, which the payload joins onto the
		// child entry inside its epic.
		if tk.ParentID != "" && tk.Slug == "kid-live" {
			tk.NotesPreamble = "# kid-live\n\nchild notes reach the payload\n"
			if err := s.PutTicket(tk); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !archivedOne {
		t.Fatal("corpus has no board-tracked top-level ticket to archive")
	}
	// Sorts after the archived epic by slug, so a payload that ordered by
	// slug alone, or put archived entries first, would order them differently.
	if err := s.PutTicket(&store.Ticket{
		Slug: "zz-live-ticket", Title: "Live alongside an archived one",
		Type: store.TypeTicket, State: store.StateActive, Section: store.SectionNow,
		Repo: "ai-devboard", BoardTracked: true, Phase: "implementing",
	}); err != nil {
		t.Fatal(err)
	}
	// A second live entry in the same repo, so the slug tiebreak is
	// consulted at all: with one live and one archived card the archived
	// test decides every comparison and slug ordering goes unexercised.
	if err := s.PutTicket(&store.Ticket{
		Slug: "aa-live-ticket", Title: "Sorts first among the live",
		Type: store.TypeTicket, State: store.StateActive, Section: store.SectionNow,
		Repo: "ai-devboard", BoardTracked: true, Phase: "verify",
	}); err != nil {
		t.Fatal(err)
	}
	// A third repo group. Repo ordering is asserted by the whole-payload
	// comparison, but with only two groups a builder that failed to sort
	// them would still land in the right order half the time.
	if err := s.PutTicket(&store.Ticket{
		Slug: "other-repo-ticket", Title: "Elsewhere", Type: store.TypeTicket,
		State: store.StateActive, Section: store.SectionNow,
		Repo: "nole", BoardTracked: true,
	}); err != nil {
		t.Fatal(err)
	}
	// No repo at all: groups under "unknown", the same fallback the
	// renderer's grouping uses.
	if err := s.PutTicket(&store.Ticket{
		Slug: "unattributed", Title: "No repo", Type: store.TypeTicket,
		State: store.StateActive, Section: store.SectionNow, BoardTracked: true,
	}); err != nil {
		t.Fatal(err)
	}
	return s
}

// payloadFrom drives a real request, so what is compared is the bytes the
// board receives rather than an intermediate map.
func payloadFrom(t *testing.T, srv *serve.Server) map[string]any {
	t.Helper()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/api/tasks")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	// Both are legitimately request-scoped, so normalizing exactly these
	// two is what makes the rest an equality claim rather than a rounding
	// of one.
	delete(out, "version")
	delete(out, "generated")
	return out
}

// The payload the board receives, pinned to a fixture captured from the
// file-walking builder this replaced. That builder is gone, so the golden
// is what carries its answer forward: it was captured by rendering this
// same corpus to disk and serving it the old way, and the two agreed
// field for field before the old path was deleted.
//
// It compares whole payloads rather than sampled fields on purpose. A
// field nobody thought to assert is exactly the kind that goes missing.
//
// mtime is dropped on both sides: it is a render stamp that moves every
// run. TestMTimeIsTheRenderStampInSeconds covers it in serve.
func TestStorePayloadMatchesGolden(t *testing.T) {
	snap, err := storeSnapshot(seedFromCorpus(t))
	if err != nil {
		t.Fatal(err)
	}
	srv := serve.New(serve.Config{})
	srv.LoadStoreSnapshot = func() (*serve.StoreSnapshot, error) { return snap, nil }
	got := payloadFrom(t, srv)
	for _, r := range got["repos"].([]any) {
		for _, e := range r.(map[string]any)["tasks"].([]any) {
			delete(e.(map[string]any), "mtime")
		}
	}

	raw, err := os.ReadFile(filepath.Join("..", "serve", "testdata", "payload_golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]any
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}

	if reflect.DeepEqual(got, want) {
		return
	}
	for _, d := range diffLeaves(want, got, "") {
		t.Error(d)
	}
	t.Fatal("store-built payload differs from the golden captured off the file builder")
}

// The corpus must actually exercise the shapes, or equality above is a
// claim about nothing. Asserted separately so a thinned corpus fails here
// with a clear reason instead of silently weakening the gate.
func TestPayloadCorpusCoversTheShapes(t *testing.T) {
	s := seedFromCorpus(t)
	snap, err := storeSnapshot(s)
	if err != nil {
		t.Fatal(err)
	}
	var epics, archived, withNotes, withChildren, unknownTop, unknownInItem int
	for _, task := range snap.Tasks {
		if task.Archived {
			archived++
		}
		if _, ok := snap.Notes[task.Slug]; ok {
			withNotes++
		}
		body := task.Task
		if body["type"] == "epic" {
			epics++
		}
		kids, _ := body["children"].([]any)
		if len(kids) > 0 {
			withChildren++
		}
		for k := range body {
			switch k {
			case "schema", "worklog", "title", "type", "phase", "tier", "complexity",
				"branch", "session", "repo_path", "scout", "plan", "scorecard",
				"decisions", "code", "needs_you", "waiting_on", "links", "children":
			default:
				unknownTop++
			}
		}
		for _, kid := range kids {
			m, _ := kid.(map[string]any)
			steps, _ := m["plan"].([]any)
			for _, step := range steps {
				sm, _ := step.(map[string]any)
				for k := range sm {
					if k != "step" && k != "state" && k != "note" && k != "reason" {
						unknownInItem++
					}
				}
			}
		}
	}
	for _, c := range []struct {
		name string
		got  int
	}{
		{"epics", epics}, {"board-archived entries", archived},
		{"tickets with notes", withNotes}, {"epics with children", withChildren},
		{"unknown top-level keys", unknownTop}, {"unknown keys inside a plan item", unknownInItem},
		{"backlog Next items", len(snap.Backlog["Next"])},
		{"repos holding both a live and an archived entry", mixedRepos(snap)},
		{"tickets grouped under unknown", groupedUnknown(snap)},
		{"children carrying notes", childrenWithNotes(snap)},
		{"repos holding two entries of the same archived state", sameStatePairs(snap)},
	} {
		if c.got == 0 {
			t.Errorf("corpus carries no %s, so the equality gate does not cover them", c.name)
		}
	}
}

// diffLeaves walks two decoded payloads together and names each leaf that
// disagrees, with its path.
func diffLeaves(a, b any, path string) []string {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok {
			return []string{path + ": map vs " + typeName(b)}
		}
		seen := map[string]bool{}
		var out []string
		for k := range av {
			seen[k] = true
			if _, ok := bv[k]; !ok {
				out = append(out, path+"."+k+": only in the file payload")
				continue
			}
			out = append(out, diffLeaves(av[k], bv[k], path+"."+k)...)
		}
		for k := range bv {
			if !seen[k] {
				out = append(out, path+"."+k+": only in the store payload")
			}
		}
		return out
	case []any:
		bv, ok := b.([]any)
		if !ok {
			return []string{path + ": list vs " + typeName(b)}
		}
		if len(av) != len(bv) {
			return []string{fmt.Sprintf("%s: %d items vs %d", path, len(av), len(bv))}
		}
		var out []string
		for i := range av {
			out = append(out, diffLeaves(av[i], bv[i], fmt.Sprintf("%s[%d]", path, i))...)
		}
		return out
	default:
		if !reflect.DeepEqual(a, b) {
			return []string{fmt.Sprintf("%s: %#v vs %#v", path, a, b)}
		}
		return nil
	}
}

func typeName(v any) string { return fmt.Sprintf("%T", v) }

func mixedRepos(snap *serve.StoreSnapshot) int {
	live, arch := map[string]bool{}, map[string]bool{}
	for _, t := range snap.Tasks {
		if t.Archived {
			arch[t.Repo] = true
		} else {
			live[t.Repo] = true
		}
	}
	n := 0
	for r := range live {
		if arch[r] {
			n++
		}
	}
	return n
}

func groupedUnknown(snap *serve.StoreSnapshot) int {
	n := 0
	for _, t := range snap.Tasks {
		if t.Repo == "unknown" {
			n++
		}
	}
	return n
}

func childrenWithNotes(snap *serve.StoreSnapshot) int {
	n := 0
	for _, t := range snap.Tasks {
		kids, _ := t.Task["children"].([]any)
		for _, kid := range kids {
			m, _ := kid.(map[string]any)
			id, _ := m["id"].(string)
			if _, ok := snap.Notes[id]; ok {
				n++
			}
		}
	}
	return n
}

func sameStatePairs(snap *serve.StoreSnapshot) int {
	seen, n := map[string]bool{}, 0
	for _, t := range snap.Tasks {
		key := t.Repo + "/"
		if t.Archived {
			key += "archived"
		}
		if seen[key] {
			n++
		}
		seen[key] = true
	}
	return n
}
