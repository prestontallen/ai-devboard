// Package devboard writes devboard task files — the live-telemetry YAML
// rendered by the devboard dashboard (see devboard/schema.md at the repo
// root). Worklog is the privileged writer of these files, never a required
// one: everything here is a silent no-op when the devboard data dir does
// not exist, and hand-written schema-valid files remain fully supported.
//
// Ownership rule (schema.md): every shared field has exactly one author;
// mirroring flows worklog→devboard only.
package devboard

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Schema mirror of devboard/schema.md v1. Extra preserves unknown
// top-level fields across read-modify-write cycles (comments are not
// preserved — documented limitation).
type Task struct {
	Schema   int          `yaml:"schema"`
	Title    string       `yaml:"title,omitempty"`
	Branch   string       `yaml:"branch,omitempty"`
	Session  string       `yaml:"session,omitempty"`
	Worklog  string       `yaml:"worklog,omitempty"`
	Tier     *int         `yaml:"tier,omitempty"`
	Phase    string       `yaml:"phase,omitempty"`
	Plan     []PlanItem   `yaml:"plan,omitempty"`
	Score    []ScoreItem  `yaml:"scorecard,omitempty"`
	Decision []Decision   `yaml:"decisions,omitempty"`
	Code     []CodeRef    `yaml:"code,omitempty"`
	NeedsYou []NeedsItem  `yaml:"needs_you,omitempty"`
	Links    []Link       `yaml:"links,omitempty"`
	Extra    map[string]any `yaml:",inline"`
}

type PlanItem struct {
	Text  string `yaml:"text"`
	State string `yaml:"state"` // pending|in_progress|done|blocked
}

type ScoreItem struct {
	Text   string `yaml:"text"`
	Verify string `yaml:"verify,omitempty"`
	Status string `yaml:"status"` // pending|pass|fail
}

type Decision struct {
	What string `yaml:"what"`
	Why  string `yaml:"why,omitempty"`
	When string `yaml:"when,omitempty"`
}

type CodeRef struct {
	File    string `yaml:"file"`
	Lines   string `yaml:"lines,omitempty"`
	Lang    string `yaml:"lang,omitempty"`
	Note    string `yaml:"note,omitempty"`
	Snippet string `yaml:"snippet,omitempty"`
}

type NeedsItem struct {
	Type   string `yaml:"type,omitempty"` // question|checkpoint
	Text   string `yaml:"text"`
	Detail string `yaml:"detail,omitempty"`
}

type Link struct {
	Label string `yaml:"label,omitempty"`
	URL   string `yaml:"url"`
}

// DataDir returns the devboard data directory: $DEVBOARD_DATA, defaulting
// to ~/.local/share/devboard.
func DataDir() string {
	if d := os.Getenv("DEVBOARD_DATA"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "devboard")
}

// Enabled reports whether the data dir exists. Callers must treat a false
// result as "do nothing, succeed" — devboard is opt-in by dir presence.
func Enabled() bool {
	d := DataDir()
	if d == "" {
		return false
	}
	fi, err := os.Stat(d)
	return err == nil && fi.IsDir()
}

// Find locates an existing task file <data>/<repo>/<slug>.{yaml,yml} across
// all repo groups. Returns "" (no error) when absent.
func Find(slug string) (string, error) {
	root := DataDir()
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		for _, ext := range []string{".yaml", ".yml"} {
			p := filepath.Join(root, e.Name(), slug+ext)
			if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
				return p, nil
			}
		}
	}
	return "", nil
}

// List returns every task file path grouped under the data dir.
func List() ([]string, error) {
	root := DataDir()
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		files, err := os.ReadDir(filepath.Join(root, e.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			name := strings.ToLower(f.Name())
			if f.Type().IsRegular() && (strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")) {
				out = append(out, filepath.Join(root, e.Name(), f.Name()))
			}
		}
	}
	return out, nil
}

// RepoName derives the grouping directory for new task files: basename of
// the enclosing git worktree, falling back to the cwd basename.
func RepoName() string {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err == nil {
		if top := strings.TrimSpace(string(out)); top != "" {
			return filepath.Base(top)
		}
	}
	wd, err := os.Getwd()
	if err != nil {
		return "unknown"
	}
	return filepath.Base(wd)
}

// gitBranch returns the current branch name, or "" outside a repo.
func gitBranch() string {
	out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Mutate performs a locked, atomic read-modify-write of the task file at
// path. When the file is absent it starts from an empty schema-1 task.
// A file that exists but fails to parse aborts before fn runs, leaving the
// file byte-identical.
func Mutate(path string, fn func(*Task) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	unlock, err := lock(path + ".lock")
	if err != nil {
		return err
	}
	defer unlock()

	t := &Task{Schema: 1}
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		if uerr := yaml.Unmarshal(raw, t); uerr != nil {
			return fmt.Errorf("devboard: %s is not valid YAML (file left untouched): %w", path, uerr)
		}
		if t.Schema == 0 {
			t.Schema = 1
		}
	case errors.Is(err, os.ErrNotExist):
		// fresh task
	default:
		return err
	}

	if err := fn(t); err != nil {
		return err
	}

	out, err := yaml.Marshal(t)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".devboard-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// pathFor returns the existing file for slug, or the creation path in the
// current repo group when none exists.
func pathFor(slug string) (string, error) {
	if p, err := Find(slug); err != nil || p != "" {
		return p, err
	}
	return filepath.Join(DataDir(), RepoName(), slug+".yaml"), nil
}

// today is stubbed in tests.
var today = func() string { return time.Now().Format("2006-01-02") }

// --- worklog lifecycle side effects -----------------------------------
// All three are silent no-ops when devboard is not Enabled. Errors are
// returned for the caller to WARN on — a devboard failure must never fail
// the worklog operation itself.

// OnStart guarantees a task file exists for the started ticket, carrying
// identity fields. It does NOT set phase — phases are agent-driven.
func OnStart(id, title string) error {
	if !Enabled() {
		return nil
	}
	path, err := pathFor(id)
	if err != nil {
		return err
	}
	return Mutate(path, func(t *Task) error {
		created := t.Title == "" && t.Worklog == ""
		if title != "" {
			t.Title = title
		}
		t.Worklog = id
		if s := os.Getenv("CLAUDE_CODE_SESSION_ID"); s != "" {
			t.Session = s
		}
		if created {
			if b := gitBranch(); b != "" {
				t.Branch = b
			}
		}
		return nil
	})
}

// OnDone marks the task done and clears the attention queue. No-op when no
// task file exists for the ticket.
func OnDone(id string) error {
	if !Enabled() {
		return nil
	}
	path, err := Find(id)
	if err != nil || path == "" {
		return err
	}
	return Mutate(path, func(t *Task) error {
		t.Phase = "done"
		t.NeedsYou = nil
		return nil
	})
}

// OnPR sets (or clears, url=="") the PR link on the ticket's task file.
// No-op when no task file exists.
func OnPR(id, url string) error {
	if !Enabled() {
		return nil
	}
	path, err := Find(id)
	if err != nil || path == "" {
		return err
	}
	return Mutate(path, func(t *Task) error {
		kept := t.Links[:0]
		for _, l := range t.Links {
			if l.Label != "PR" {
				kept = append(kept, l)
			}
		}
		t.Links = kept
		if url != "" {
			t.Links = append(t.Links, Link{Label: "PR", URL: url})
		}
		return nil
	})
}
