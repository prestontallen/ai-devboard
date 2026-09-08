package serve

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/prestontallen/ai-devboard/worklog/internal/feedback"
	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/parse"
	"github.com/prestontallen/ai-devboard/worklog/internal/yamlx"
)

const archiveDir = "_archive"

var taskExts = []string{".yaml", ".yml", ".json"}

func hasTaskExt(name string) bool {
	lower := strings.ToLower(name)
	for _, ext := range taskExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// feedbackView is the frontend's feedback entry shape. It marshals
// independently of feedback.Entry so `resolved` is always present
// (Entry's omitempty would drop resolved:0, which the old server emitted).
type feedbackView struct {
	Timestamp int64  `json:"timestamp"`
	Signal    string `json:"signal"`
	Trigger   string `json:"trigger"`
	Excerpt   string `json:"excerpt"`
	Context   string `json:"context"`
	Resolved  int64  `json:"resolved"`
}

// allTasks builds the /api/tasks payload: every task file grouped by repo
// directory (live first, then _archive), plus the parsed friction log.
func (s *Server) allTasks() map[string]any {
	repos := make([]any, 0)
	names := make([]string, 0)
	if entries, err := os.ReadDir(s.cfg.DataDir); err == nil {
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				names = append(names, e.Name())
			}
		}
	}
	sort.Strings(names)
	for _, name := range names {
		rdir := filepath.Join(s.cfg.DataDir, name)
		tasks := make([]any, 0)
		for _, f := range sortedTaskFiles(rdir) {
			tasks = append(tasks, s.parseTask(filepath.Join(rdir, f), false))
		}
		arcdir := filepath.Join(rdir, archiveDir)
		for _, f := range sortedTaskFiles(arcdir) {
			tasks = append(tasks, s.parseTask(filepath.Join(arcdir, f), true))
		}
		if len(tasks) > 0 {
			repos = append(repos, map[string]any{"repo": name, "tasks": tasks})
		}
	}
	return map[string]any{
		"version":   s.currentVersion(),
		"generated": float64(time.Now().UnixNano()) / 1e9,
		"repos":     repos,
		"feedback":  s.parseFeedback(),
		"backlog":   s.parseBacklog(),
	}
}

// backlogSection is one WORK.md section as the backlog lens draws it.
type backlogSection struct {
	Name  string         `json:"name"`
	Items []*model.Block `json:"items"`
}

// backlogSections are the two the lens shows, in bar order. `Now` is
// deliberately absent: started work already reaches the board as task
// files, and listing it twice would double it against the Board chip.
// `Waiting` is absent for the same reason — it has its own lens.
var backlogSections = []model.SectionName{model.SectionNext, model.SectionSomeday}

// parseBacklog reads WORK.md's not-yet-started sections.
//
// This is the one place the server opens WORK.md. It reads the projection
// rather than the store because the projection is what the human reads,
// and because reaching for the store would put a second reader (and its
// locking) inside a process whose whole job is to render.
//
// That decision was knowingly revisited — not contradicted — by
// adb-store-serve-shadow: shadow.go's store reader exists to measure
// whether serving from the store is safe, behind a flag, per request,
// with the file path still serving every byte. If the epic's flip lands,
// this comment's rationale retires with the file read; until then it
// stands.
//
// Any problem yields empty sections, never an error: the backlog is one
// lens among seven, and a malformed or absent WORK.md must not take the
// whole board down — the same rule parseFeedback follows.
func (s *Server) parseBacklog() []backlogSection {
	out := make([]backlogSection, 0, len(backlogSections))
	doc, err := parse.File(filepath.Join(s.cfg.WorklogDir, "WORK.md"))
	for _, name := range backlogSections {
		section := backlogSection{Name: string(name), Items: []*model.Block{}}
		if err == nil {
			if found := doc.Section(name); found != nil {
				for i := range found.Blocks {
					section.Items = append(section.Items, &found.Blocks[i])
				}
			}
		}
		out = append(out, section)
	}
	return out
}

func sortedTaskFiles(dir string) []string {
	out := make([]string, 0)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.Type().IsRegular() && hasTaskExt(e.Name()) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// parseTask mirrors the old server's per-file behavior: any bad file
// becomes an error card, never a failed response.
func (s *Server) parseTask(path string, archived bool) map[string]any {
	rel, err := filepath.Rel(s.cfg.DataDir, path)
	if err != nil {
		rel = path
	}
	base := filepath.Base(path)
	entry := map[string]any{
		"file": rel,
		"id":   strings.TrimSuffix(base, filepath.Ext(base)),
	}
	if archived {
		entry["archived"] = true
	}

	fail := func(err error) map[string]any {
		entry["error"] = err.Error()
		return entry
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return fail(err)
	}
	var data any
	if strings.HasSuffix(strings.ToLower(path), ".json") {
		data, err = yamlx.JSONToAny(raw)
	} else {
		data, err = yamlx.YAMLToAny(raw)
	}
	if err != nil {
		return fail(err)
	}
	task, ok := data.(map[string]any)
	if !ok {
		return fail(fmt.Errorf("top level must be a mapping"))
	}
	entry["task"] = task
	st, err := os.Stat(path)
	if err != nil {
		return fail(err)
	}
	entry["mtime"] = float64(st.ModTime().UnixNano()) / 1e9

	if notes, ok := s.notesFor(task["worklog"]); ok {
		entry["notes"] = notes
	}
	s.attachChildNotes(task)
	return entry
}

// notesFor reads notes/<name>.md for a worklog id, or reports that there is
// nothing to attach.
//
// The name reaches a filesystem path, and every task file on the board is
// hand-editable, so it must be a plain name: no separator and no parent
// reference. This is the whole guard — there is no second one downstream.
func (s *Server) notesFor(v any) (string, bool) {
	name, ok := v.(string)
	if !ok || name == "" || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return "", false
	}
	notes, err := os.ReadFile(filepath.Join(s.cfg.WorklogDir, "notes", name+".md"))
	if err != nil {
		return "", false
	}
	return string(notes), true
}

// attachChildNotes gives each of an epic's children its own notes.
//
// A child of an epic has no task file, so it has no top-level `worklog` key to
// resolve — but it is a worklog ticket in its own right, and its `id` IS the
// notes filename (devboard/schema.md, "Epic files"). Without this a child's
// detail page is the only one on the board with no notes, for a file sitting
// on disk beside every other ticket's.
//
// The id runs through the same guard as `worklog`: it is read from the same
// hand-editable file and reaches the same path join.
func (s *Server) attachChildNotes(task map[string]any) {
	children, ok := task["children"].([]any)
	if !ok {
		return
	}
	for _, c := range children {
		child, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if notes, ok := s.notesFor(child["id"]); ok {
			child["notes"] = notes
		}
	}
}

// parseFeedback reuses the CLI's own FEEDBACK.md parser — one parser, so the
// board can never drift from the writer. Any problem yields an empty list:
// friction is a side panel and must never take down the page.
func (s *Server) parseFeedback() []feedbackView {
	out := make([]feedbackView, 0)
	entries, err := feedback.Parse(filepath.Join(s.cfg.WorklogDir, "FEEDBACK.md"))
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
