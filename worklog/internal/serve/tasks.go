package serve

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/prestontallen/ai-devboard/worklog/internal/feedback"
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
	}
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

	if wl, ok := task["worklog"].(string); ok && wl != "" &&
		!strings.Contains(wl, "/") && !strings.Contains(wl, "..") {
		np := filepath.Join(s.cfg.WorklogDir, "notes", wl+".md")
		if notes, err := os.ReadFile(np); err == nil {
			entry["notes"] = string(notes)
		}
	}
	return entry
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
