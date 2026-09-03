// Package projection renders the markdown and YAML surfaces from a Store.
// Projections are read-only build outputs: banner-stamped where the format
// allows, regenerated write-through, and compared before writing so a
// byte-identical render never touches mtime (the frozen SSE behavior must
// not fire on no-op rebuilds).
package projection

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

// RenderAll writes every projection of s under root:
// WORK.md, notes/<slug>.md, archive/<month>.md, FEEDBACK.md,
// devboard/<repo>/<slug>.yaml (with _archive/ for board-archived tasks).
// INDEX.md is rendered by running the real reindex over the output —
// its four sections are the de-facto spec (reindex.go), so the projection
// IS that code's output, not a reimplementation.
func RenderAll(s store.Store, root string) error {
	tickets, err := s.Tickets()
	if err != nil {
		return err
	}
	if err := writeIfChanged(filepath.Join(root, "WORK.md"), WorkMD(tickets)); err != nil {
		return err
	}
	for _, t := range tickets {
		if t.NotesPreamble == "" && len(t.NoteEntries) == 0 {
			continue
		}
		name := t.Slug
		if name == "" {
			continue // slug-less quick-capture entities carry no notes file
		}
		if err := writeIfChanged(filepath.Join(root, "notes", name+".md"), NotesFile(t)); err != nil {
			return err
		}
	}
	months := map[string][]*store.Ticket{}
	for _, t := range tickets {
		if t.Archived && t.ArchiveMonth != "" {
			months[t.ArchiveMonth] = append(months[t.ArchiveMonth], t)
		}
	}
	for month, ts := range months {
		if err := writeIfChanged(filepath.Join(root, "archive", month+".md"), ArchiveMonth(month, ts, tickets)); err != nil {
			return err
		}
	}
	fb, err := s.Feedback()
	if err != nil {
		return err
	}
	if len(fb) > 0 {
		if err := writeIfChanged(filepath.Join(root, "FEEDBACK.md"), FeedbackMD(fb)); err != nil {
			return err
		}
	}
	byID := map[store.ID]*store.Ticket{}
	for _, t := range tickets {
		byID[t.ID] = t
	}
	for _, t := range tickets {
		if !t.BoardTracked || t.ParentID != "" {
			continue // children render inside their epic's file
		}
		allKids, err := s.Children(t.ID)
		if err != nil {
			return err
		}
		// Only board-tracked children nest in the feed: children archived
		// before they ever had board entries stay out — the projection
		// states today's facts, it doesn't invent feed entries.
		kids := allKids[:0]
		for _, k := range allKids {
			if k.BoardTracked {
				kids = append(kids, k)
			}
		}
		// Repo attribution heals here (ratified OQ2): the canonical repo
		// field decides the group directory, never the writer's cwd.
		repo := t.Repo
		if repo == "" {
			repo = "unknown"
		}
		dir := filepath.Join(root, "devboard", repo)
		if t.BoardArchived {
			dir = filepath.Join(dir, "_archive")
		}
		if err := writeIfChanged(filepath.Join(dir, t.Slug+".yaml"), BoardYAML(t, kids)); err != nil {
			return err
		}
	}
	return nil
}

// writeIfChanged is the freshness rule (criterion 13): identical content
// never rewrites the file, so watchers see no phantom change.
func writeIfChanged(path string, content []byte) error {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, content) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".proj-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// FeedbackMD renders the friction log in the exact format
// internal/feedback writes and parses.
func FeedbackMD(entries []*store.FeedbackEntry) []byte {
	var b bytes.Buffer
	b.WriteString("# Worklog Feedback Log\n")
	for _, e := range entries {
		fmt.Fprintf(&b, "\n## %d — %s\n", e.Seconds, e.Signal)
		fmt.Fprintf(&b, "**Trigger**: %s\n", e.Trigger)
		if e.Excerpt != "" {
			b.WriteString("**Excerpt**:\n")
			for _, line := range splitLines(e.Excerpt) {
				fmt.Fprintf(&b, "> %s\n", line)
			}
		}
		if e.Context != "" {
			fmt.Fprintf(&b, "**Context**: %s\n", e.Context)
		}
		if e.Resolved != 0 {
			fmt.Fprintf(&b, "**Resolved**: %d\n", e.Resolved)
		}
	}
	return b.Bytes()
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
