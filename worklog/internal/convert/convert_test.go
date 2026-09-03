package convert

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/store"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/memstore"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/sqlitestore"
)

// testdata/corpus exercises every hazard the risk scouts found in the
// live data: unknown WORK.md field bullets, empty-vs-absent PR,
// space-containing tags, archive date pairs and bare Completed, literal
// `**PR**:` inside a Summary, archived-epic Children, duplicate note
// stamps, content headings inside note bodies, pending children living
// only on the roster, unknown YAML keys at every level, a bare producer
// file, and the retired phase alias.
func corpus() Corpus {
	c, err := ReadCorpusDir("testdata/corpus")
	if err != nil {
		panic(err)
	}
	return c
}

func stores(t *testing.T) map[string]store.Store {
	t.Helper()
	sq, err := sqlitestore.Open(filepath.Join(t.TempDir(), "w.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sq.Close() })
	return map[string]store.Store{"memstore": memstore.New(), "sqlite": sq}
}

func TestRoundTrip(t *testing.T) {
	for name, s := range stores(t) {
		t.Run(name, func(t *testing.T) {
			rep, err := Load(s, corpus())
			if err != nil {
				t.Fatal(err)
			}
			// 7 tickets: an-epic, kid-live, kid-pending (roster only),
			// solo, the bare title-only one, done-ticket, old-epic.
			if rep.Tickets != 7 {
				t.Errorf("tickets = %d, want 7", rep.Tickets)
			}
			if rep.Feedback != 2 {
				t.Errorf("feedback = %d, want 2", rep.Feedback)
			}
			if len(rep.Skipped) != 1 || !strings.Contains(rep.Skipped[0], "bare.yaml") {
				t.Errorf("skipped = %v, want the bare producer file", rep.Skipped)
			}
			if len(rep.Warnings) != 1 || !strings.Contains(rep.Warnings[0], "worklog cli release") {
				t.Errorf("warnings = %v, want the space-tag lint", rep.Warnings)
			}

			epic, err := s.TicketBySlug("an-epic")
			if err != nil {
				t.Fatal(err)
			}
			if epic.Type != store.TypeEpic || epic.Extra["custom_top"] != "survives" {
				t.Errorf("epic lost data: %+v", epic)
			}
			if !strings.Contains(epic.NotesPreamble, "scaffold comment stays verbatim") {
				t.Error("preamble not verbatim")
			}
			// Segmentation: content heading swallowed, duplicate stamps distinct.
			if len(epic.NoteEntries) != 2 {
				t.Fatalf("note entries = %d, want 2", len(epic.NoteEntries))
			}
			if !strings.Contains(epic.NoteEntries[0].Body, "Why a content heading does not split") {
				t.Error("content heading split the first entry")
			}
			if epic.NoteEntries[0].Stamp != epic.NoteEntries[1].Stamp {
				t.Error("duplicate stamps should both parse")
			}

			kid, err := s.TicketBySlug("kid-live")
			if err != nil {
				t.Fatal(err)
			}
			if kid.ParentID != epic.ID {
				t.Error("child relation not resolved to the epic's ULID")
			}
			if kid.PR == nil || *kid.PR != "" {
				t.Error("present-but-empty PR lost")
			}
			if kid.ExtraFields["Custom Field"] != "hand-added and load-bearing" {
				t.Errorf("unknown WORK.md field lost: %v", kid.ExtraFields)
			}
			if kid.Phase != "implementing" {
				t.Errorf("phase alias not normalized: %q", kid.Phase)
			}
			if len(kid.PlanSteps) != 1 || kid.PlanSteps[0].Extra["surprise_key"] != "kept" {
				t.Errorf("per-item unknown key lost: %+v", kid.PlanSteps)
			}
			var pr, ref int
			for _, l := range kid.Links {
				switch l.Kind {
				case store.LinkPR:
					pr++
				case store.LinkRef:
					ref++
				}
			}
			if pr != 1 || ref != 1 {
				t.Errorf("links not typed: %+v", kid.Links)
			}
			if kid.Scout == nil || kid.Scout.Mode != "ran" {
				t.Errorf("scout lost: %+v", kid.Scout)
			}

			pending, err := s.TicketBySlug("kid-pending")
			if err != nil {
				t.Fatal(err)
			}
			if pending.Title != "Pending child whose title lives only here" || pending.ParentID != epic.ID {
				t.Errorf("roster-only child mangled: %+v", pending)
			}

			done, err := s.TicketBySlug("done-ticket")
			if err != nil {
				t.Fatal(err)
			}
			if done.Started != "2026-09-01" || done.Completed != "2026-09-02" ||
				done.TimeSpent != "~2h" || len(done.ArchiveFeedback) != 2 {
				t.Errorf("archive fields lost: %+v", done)
			}
			if !strings.Contains(done.Summary, "**PR**: still stood") {
				t.Errorf("summary with field-marker text mangled: %q", done.Summary)
			}

			oldEpic, err := s.TicketBySlug("old-epic")
			if err != nil {
				t.Fatal(err)
			}
			if oldEpic.Completed != "2026-09-01" || oldEpic.Started != "" {
				t.Error("bare-Completed entry mangled")
			}

			fb, err := s.Feedback()
			if err != nil || len(fb) != 2 || fb[0].ID == fb[1].ID {
				t.Errorf("feedback: %v %v", fb, err)
			}
		})
	}
}

// TestDeterministicRerun: converting the same corpus into the same store
// twice yields identical ticket ULIDs and identical sub-item ULIDs for
// unchanged content — the property adb-worklog2-migrate's id-set diff
// depends on (D4).
func TestDeterministicRerun(t *testing.T) {
	for name, s := range stores(t) {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(s, corpus()); err != nil {
				t.Fatal(err)
			}
			first := snapshotIDs(t, s)
			if _, err := Load(s, corpus()); err != nil {
				t.Fatal(err)
			}
			second := snapshotIDs(t, s)
			if len(first) != len(second) {
				t.Fatalf("id count changed: %d vs %d", len(first), len(second))
			}
			for k, v := range first {
				if second[k] != v {
					t.Errorf("%s: id changed across re-run: %s vs %s", k, v, second[k])
				}
			}
		})
	}
}

func snapshotIDs(t *testing.T, s store.Store) map[string]store.ID {
	t.Helper()
	out := make(map[string]store.ID)
	all, err := s.Tickets()
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range all {
		out[tk.Slug] = tk.ID
		for _, p := range tk.PlanSteps {
			out[tk.Slug+"/plan/"+p.Text] = p.ID
		}
		for _, c := range tk.Scorecard {
			out[tk.Slug+"/score/"+c.Text] = c.ID
		}
		for _, n := range tk.NoteEntries {
			out[tk.Slug+"/note/"+n.Stamp+"/"+n.Body[:min(8, len(n.Body))]] = n.ID
		}
	}
	return out
}

func TestConverterRefusals(t *testing.T) {
	base := corpus()

	t.Run("unmodeled work line", func(t *testing.T) {
		c := base
		c.WorkMD = []byte("## Now\n- [ ] **X** — t\n  - **ID**: x\nrogue prose line\n")
		if _, err := Load(memstore.New(), c); err == nil || !strings.Contains(err.Error(), "unmodeled") {
			t.Errorf("want unmodeled refusal, got %v", err)
		}
	})
	t.Run("duplicate slug across live and archive", func(t *testing.T) {
		c := base
		c.Archives = map[string][]byte{"2026-08": []byte("# A\n\n## 2026-08-01\n\n### solo — same slug again\n- **Completed**: 2026-08-01\n")}
		if _, err := Load(memstore.New(), c); err == nil || !strings.Contains(err.Error(), "duplicate slug") {
			t.Errorf("want duplicate-slug refusal, got %v", err)
		}
	})
	t.Run("board join to ghost ticket", func(t *testing.T) {
		c := base
		c.Board = append([]BoardInput{}, c.Board...)
		c.Board = append(c.Board, BoardInput{Repo: "r", Name: "ghost.yaml", Data: []byte("worklog: ghost\n")})
		if _, err := Load(memstore.New(), c); err == nil || !strings.Contains(err.Error(), "matches no ticket") {
			t.Errorf("want ghost-join refusal, got %v", err)
		}
	})
	t.Run("active-children inconsistency", func(t *testing.T) {
		c := base
		c.WorkMD = []byte(strings.Replace(string(base.WorkMD), "**Active children**: kid-live", "**Active children**: kid-ghost", 1))
		if _, err := Load(memstore.New(), c); err == nil || !strings.Contains(err.Error(), "does not exist") {
			t.Errorf("want consistency refusal, got %v", err)
		}
	})
	t.Run("notes for unknown slug", func(t *testing.T) {
		c := base
		c.Notes = map[string][]byte{"nobody": []byte("## 2026-01-01 00:00\nx\n")}
		if _, err := Load(memstore.New(), c); err == nil || !strings.Contains(err.Error(), "no ticket wears") {
			t.Errorf("want unknown-notes refusal, got %v", err)
		}
	})
}
