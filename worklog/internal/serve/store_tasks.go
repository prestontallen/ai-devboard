package serve

// The store-fed /api/tasks payload (adb-serve-store-direct). Every value
// the board shows comes from a ticket, not from a file: no directory is
// read, no rendered path is parsed, and the devboard tree is never
// touched. That is what lets adb-retire-devboard-dir-2 delete it.
//
// The wire shape is unchanged and stays frozen (devboard/API.md). Two
// entry fields have no store column and are composed here instead:
// `file`, which the frontend uses as a card-title fallback, and the repo
// group name, which falls back to "unknown" exactly as the renderer's
// grouping does.

import (
	"sort"
	"time"

	"github.com/prestontallen/ai-devboard/worklog/internal/feedback"
	"github.com/prestontallen/ai-devboard/worklog/internal/model"
)

// StoreTask is one board-tracked ticket, in the shape the payload needs.
// Task is the board body with unknown keys inline, already converted out
// of YAML by the loader; MTime is the ticket's BoardRenderedAt in unix
// nanoseconds, which is the same instant the file mtime reported.
type StoreTask struct {
	Repo     string
	Slug     string
	Archived bool
	MTime    int64
	Task     map[string]any
}

// StoreSnapshot is everything one /api/tasks request needs, read from the
// store in a single open by the injected loader.
//
// Notes is keyed by ticket slug rather than by path, so attaching a
// ticket's notes never involves composing a filename from an id — the
// traversal guard the file path needed has no equivalent here because
// there is no path.
//
// Backlog is keyed by WORK.md section name. Feedback stays as rendered
// FEEDBACK.md bytes: the board already parses that shape, and a second
// store→view mapping would buy nothing (contract decision 11).
type StoreSnapshot struct {
	Tasks    []StoreTask
	Notes    map[string]string
	Backlog  map[model.SectionName][]model.Block
	Feedback []byte
}

// allTasksFromStore builds the payload. Repos sort by name; within a repo
// the live entries come first, then the archived ones, each by slug —
// the order the directory walk produced when these were files named
// <slug>.yaml.
func (s *Server) allTasksFromStore(snap *StoreSnapshot) map[string]any {
	byRepo := map[string][]StoreTask{}
	for _, t := range snap.Tasks {
		byRepo[t.Repo] = append(byRepo[t.Repo], t)
	}
	names := make([]string, 0, len(byRepo))
	for name := range byRepo {
		names = append(names, name)
	}
	sort.Strings(names)

	repos := make([]any, 0)
	for _, name := range names {
		in := byRepo[name]
		sort.Slice(in, func(i, j int) bool {
			if in[i].Archived != in[j].Archived {
				return !in[i].Archived
			}
			return in[i].Slug < in[j].Slug
		})
		tasks := make([]any, 0, len(in))
		for _, t := range in {
			tasks = append(tasks, s.storeEntry(snap, t))
		}
		if len(tasks) > 0 {
			repos = append(repos, map[string]any{"repo": name, "tasks": tasks})
		}
	}
	return map[string]any{
		"version":   s.currentVersion(),
		"generated": float64(time.Now().UnixNano()) / 1e9,
		"repos":     repos,
		"feedback":  parseFeedbackBytes(snap.Feedback),
		"backlog":   storeBacklogSections(snap),
	}
}

// storeEntry is one card. `archived` is present only when true and
// `notes` only when the ticket has a notes file, matching what the file
// walk emitted; adding either unconditionally would change the frozen
// shape.
func (s *Server) storeEntry(snap *StoreSnapshot, t StoreTask) map[string]any {
	rel := t.Repo + "/" + t.Slug + ".yaml"
	if t.Archived {
		rel = t.Repo + "/" + archiveDir + "/" + t.Slug + ".yaml"
	}
	entry := map[string]any{"file": rel, "id": t.Slug}
	if t.Archived {
		entry["archived"] = true
	}
	entry["task"] = t.Task
	entry["mtime"] = float64(t.MTime) / 1e9
	if notes, ok := notesFor(snap, t.Task["worklog"]); ok {
		entry["notes"] = notes
	}
	attachChildNotes(snap, t.Task)
	return entry
}

// notesFor looks a ticket's notes up by slug. The board body carries the
// slug under `worklog`, and a child carries its own under `id`.
func notesFor(snap *StoreSnapshot, v any) (string, bool) {
	slug, ok := v.(string)
	if !ok || slug == "" {
		return "", false
	}
	notes, ok := snap.Notes[slug]
	return notes, ok
}

func attachChildNotes(snap *StoreSnapshot, task map[string]any) {
	children, ok := task["children"].([]any)
	if !ok {
		return
	}
	for _, c := range children {
		child, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if notes, ok := notesFor(snap, child["id"]); ok {
			child["notes"] = notes
		}
	}
}

// storeBacklogSections emits the lens's two sections in bar order, always
// both, empty rather than absent: the lens distinguishes "no items" from
// "no backlog reported by the server".
func storeBacklogSections(snap *StoreSnapshot) []backlogSection {
	out := make([]backlogSection, 0, len(backlogSections))
	for _, name := range backlogSections {
		items := make([]*model.Block, 0, len(snap.Backlog[name]))
		blocks := snap.Backlog[name]
		for i := range blocks {
			items = append(items, &blocks[i])
		}
		out = append(out, backlogSection{Name: string(name), Items: items})
	}
	return out
}

// parseFeedbackBytes reads the rendered FEEDBACK.md the store produces,
// reusing the CLI's own parser so there is one parser for the surface.
// Any problem yields no entries rather than an error: the friction log is
// one lens among seven and must not take the board down.
func parseFeedbackBytes(b []byte) []feedbackView {
	out := make([]feedbackView, 0)
	entries, err := feedback.ParseBytes(b)
	if err != nil {
		return out
	}
	for _, e := range entries {
		out = append(out, feedbackView{
			Timestamp: e.Timestamp,
			Signal:    string(e.Signal),
			Trigger:   e.Trigger,
			Excerpt:   e.Excerpt,
			Context:   e.Context,
			Resolved:  e.Resolved,
		})
	}
	return out
}

// tasksPayload answers /api/tasks. With no loader wired, or on a machine
// that has never adopted a store, the board is empty rather than absent:
// the full envelope with no repos, so every lens draws its own "nothing
// here" instead of the frontend iterating a null.
func (s *Server) tasksPayload() (map[string]any, error) {
	if s.LoadStoreSnapshot == nil {
		return s.allTasksFromStore(&StoreSnapshot{}), nil
	}
	snap, err := s.LoadStoreSnapshot()
	if err != nil {
		return nil, err
	}
	if snap == nil {
		return s.allTasksFromStore(&StoreSnapshot{}), nil
	}
	return s.allTasksFromStore(snap), nil
}
