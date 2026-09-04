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
	"strings"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

// Banner is the build-output marker emitted at the top of the markdown
// surfaces. It lives in internal/model so the parser can strip it without
// depending on this package (see model.Banner).
const Banner = model.Banner

func banner(b *bytes.Buffer) { b.WriteString(Banner + "\n") }

// Render produces every projection of s as an in-memory map of
// slash-separated relative path to content: WORK.md, notes/<slug>.md,
// archive/<month>.md, FEEDBACK.md, devboard/<repo>/<slug>.yaml (with
// _archive/ for board-archived tasks).
//
// INDEX.md is deliberately absent: it is produced by running the real
// reindex over this output, so the projection IS that code's output
// rather than a reimplementation of it.
func Render(s store.Store) (map[string][]byte, error) {
	tickets, err := s.Tickets()
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{"WORK.md": WorkMD(tickets)}

	for _, t := range tickets {
		if t.NotesPreamble == "" && len(t.NoteEntries) == 0 {
			continue
		}
		if t.Slug == "" {
			continue // slug-less quick-capture entities carry no notes file
		}
		out["notes/"+t.Slug+".md"] = NotesFile(t)
	}

	months := map[string][]*store.Ticket{}
	for _, t := range tickets {
		if t.Archived && t.ArchiveMonth != "" {
			months[t.ArchiveMonth] = append(months[t.ArchiveMonth], t)
		}
	}
	for month, ts := range months {
		out["archive/"+month+".md"] = ArchiveMonth(month, ts, tickets)
	}

	fb, err := s.Feedback()
	if err != nil {
		return nil, err
	}
	// Always emitted, even with zero entries. Gating on len(fb) made
	// FEEDBACK.md invisible to BOTH EditedIn and RenderTo whenever the
	// store happened to hold no feedback — so a hand-edited or
	// merely-unread friction log was neither protected from a re-render
	// nor rewritten by one, until the first entry arrived and replaced it
	// wholesale. A projection's existence is a property of the surface,
	// not of its row count.
	out["FEEDBACK.md"] = FeedbackMD(fb)

	for _, t := range tickets {
		if !t.BoardTracked || t.ParentID != "" {
			continue // children render inside their epic's file
		}
		kids, err := s.Children(t.ID)
		if err != nil {
			return nil, err
		}
		// A child never gets its own top-level file (see the skip above),
		// so its own BoardTracked flag doesn't gate anything here — the
		// epic's roster is the only surface a child ever appears on, and
		// it's the full history: pending, active, and done children alike
		// (a done child leaves ## Now but stays on the epic's card as
		// completed history until the epic itself is archived).
		// Repo attribution heals here (ratified OQ2): the canonical repo
		// field decides the group directory, never the writer's cwd.
		repo := t.Repo
		if repo == "" {
			repo = "unknown"
		}
		dir := "devboard/" + repo
		if t.BoardArchived {
			dir += "/_archive"
		}
		out[dir+"/"+t.Slug+".yaml"] = BoardYAML(t, kids)
	}
	return out, nil
}

// Layout locates the two directories the projections actually split
// across in a live install: the worklog dir (WORK.md, notes/, archive/,
// FEEDBACK.md) and the devboard dir, which is a sibling rather than a
// subdirectory. Render's map is rooted at a single tree, so this is where
// the "devboard/" prefix gets redirected.
type Layout struct {
	WorklogDir  string
	DevboardDir string
}

// SingleRoot is the layout the staged copies and tests use, where the
// devboard tree sits under the worklog root as Render names it.
func SingleRoot(root string) Layout {
	return Layout{WorklogDir: root, DevboardDir: filepath.Join(root, "devboard")}
}

func (l Layout) path(rel string) string {
	if after, ok := strings.CutPrefix(rel, "devboard/"); ok {
		return filepath.Join(l.DevboardDir, filepath.FromSlash(after))
	}
	return filepath.Join(l.WorklogDir, filepath.FromSlash(rel))
}

// RenderTo writes every projection of s into the two directories l names.
func RenderTo(s store.Store, l Layout) error {
	files, err := Render(s)
	if err != nil {
		return err
	}
	for rel, content := range files {
		if err := writeIfChanged(l.path(rel), content); err != nil {
			return err
		}
	}
	return nil
}

// RenderAll writes every projection of s under a single root.
func RenderAll(s store.Store, root string) error { return RenderTo(s, SingleRoot(root)) }

// EditedFiles reports the projections under root whose bytes differ from
// what s renders right now, newline-sorted — the files someone hand-edited
// since the last write, whose content a re-render would destroy.
//
// It only inspects paths the store actually renders, so files it does not
// own are never flagged: bare devboard producer files (which are not
// canon), INDEX.md, and anything else living alongside. A rendered file
// missing from disk counts as edited; it was deleted.
func EditedFiles(s store.Store, root string) ([]string, error) {
	return EditedIn(s, SingleRoot(root))
}

// EditedIn is EditedFiles against a two-directory live layout.
func EditedIn(s store.Store, l Layout) ([]string, error) {
	files, err := Render(s)
	if err != nil {
		return nil, err
	}
	var edited []string
	for rel, want := range files {
		got, err := os.ReadFile(l.path(rel))
		if os.IsNotExist(err) {
			edited = append(edited, rel)
			continue
		}
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(got, want) {
			edited = append(edited, rel)
		}
	}
	sort.Strings(edited)
	return edited, nil
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
