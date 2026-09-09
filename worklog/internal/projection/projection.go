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

	"github.com/prestontallen/ai-devboard/worklog/internal/boardmap"
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
	files, _, err := render(s)
	return files, err
}

// RenderSnapshot returns every projection of s plus, for each rendered
// board file, the BoardRenderedAt stamp of the ticket it renders — the
// store's own answer to "what would be on disk, and how fresh is it".
// Read-only: it writes neither files nor stamps. This is what the serve
// layer's store shadow consumes (adb-store-serve-shadow): one renderer
// produces both the files and the store-served payload, so the shadow
// diff measures desync, never renderer disagreement.
func RenderSnapshot(s store.Store) (files map[string][]byte, boardStamps map[string]int64, err error) {
	fs, owners, err := render(s)
	if err != nil {
		return nil, nil, err
	}
	stamps := make(map[string]int64, len(owners))
	for rel, o := range owners {
		stamps[rel] = o.at
	}
	return fs, stamps, nil
}

// boardOwner ties a rendered board file to the ticket whose render it is,
// carrying the stamp read at render time so stampBoardRendered can skip
// the Touch when the file's mtime already matches.
type boardOwner struct {
	id store.ID
	at int64
}

func render(s store.Store) (map[string][]byte, map[string]boardOwner, error) {
	tickets, err := s.Tickets()
	if err != nil {
		return nil, nil, err
	}
	out := map[string][]byte{"WORK.md": WorkMD(tickets)}
	owners := map[string]boardOwner{}

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
		return nil, nil, err
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
			return nil, nil, err
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
		rel := dir + "/" + t.Slug + ".yaml"
		out[rel] = boardmap.BoardYAML(t, kids)
		owners[rel] = boardOwner{id: t.ID, at: t.BoardRenderedAt}
	}
	return out, owners, nil
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

// RenderTo writes every projection of s into the two directories l names,
// then syncs each board-tracked ticket's BoardRenderedAt to its rendered
// file's mtime. The stamp mirrors the file: writeIfChanged skips a
// byte-identical render, so an untouched file keeps its mtime and the
// stamp does not move; a changed one gets a new mtime and the stamp
// follows. Syncing to the mtime rather than time.Now is what makes a
// store-served /api/tasks mtime equal the file-served one exactly, and it
// backfills tickets from before the column existed on their next write
// (adb-store-serve-shadow). RenderAll is the pure variant.
func RenderTo(s store.Store, l Layout) error {
	files, owners, err := render(s)
	if err != nil {
		return err
	}
	if err := writeFiles(files, l); err != nil {
		return err
	}
	for rel, owner := range owners {
		st, err := os.Stat(l.path(rel))
		if err != nil {
			return fmt.Errorf("stamping %s: %w", rel, err)
		}
		if mt := st.ModTime().UnixNano(); mt != owner.at {
			if err := s.TouchBoardRendered(owner.id, mt); err != nil {
				return fmt.Errorf("stamping %s: %w", rel, err)
			}
		}
	}
	return nil
}

// writeFiles is RenderTo's write loop, shared with RenderAll.
func writeFiles(files map[string][]byte, l Layout) error {
	for rel, content := range files {
		if err := writeIfChanged(l.path(rel), content); err != nil {
			return err
		}
		// The store owns where a board task file lives, so archiving is a
		// re-render rather than a rename (adb-archive-store-desync). Clearing
		// the path the ticket just left is the other half of that: without it
		// the board shows one task twice, once per path.
		if sib := boardSibling(rel); sib != "" {
			if err := os.Remove(l.path(sib)); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("clearing %s: %w", sib, err)
			}
		}
	}
	return nil
}

// archiveSegment is the directory an archived board task file lives in,
// relative to its repo dir.
const archiveSegment = "_archive"

/*
boardSibling returns the other place THIS rendered board task file could
legitimately live — live <-> _archive — or "" for anything that is not one.

The narrowness is the whole safety argument. Render only removes the sibling
of a path it just wrote, and it only writes files for board-tracked tickets
in the store, so a file the store has no ticket for can never be named here.
The narrowness outlived the case that motivated it. It existed to keep
hand-dropped producer files out of reach, and those are gone: bare files
stopped being a supported input in adb-retire-devboard-dir. It is kept
because the argument does not depend on them — the broad rule ("delete
anything I did not render") is unsafe against any file the store has no
ticket for, however it got there.
*/
func boardSibling(rel string) string {
	if !strings.HasPrefix(rel, "devboard/") || !strings.HasSuffix(rel, ".yaml") {
		return ""
	}
	dir, file := filepath.Split(rel)
	dir = strings.TrimSuffix(dir, "/")
	if filepath.Base(dir) == archiveSegment {
		return filepath.Dir(dir) + "/" + file
	}
	return dir + "/" + archiveSegment + "/" + file
}

// RenderAll writes every projection of s under a single root, WITHOUT
// stamping BoardRenderedAt — it is the scratch-dir variant: verify
// renders into a temp dir for comparison and must not write the store,
// nor adopt real stamps from scratch-file mtimes.
func RenderAll(s store.Store, root string) error {
	files, _, err := render(s)
	if err != nil {
		return err
	}
	return writeFiles(files, SingleRoot(root))
}

// EditedFiles reports the projections under root whose bytes differ from
// what s renders right now, newline-sorted — the files someone hand-edited
// since the last write, whose content a re-render would destroy.
//
// It only inspects paths the store actually renders, so files it does not
// own are never flagged: INDEX.md and anything else living alongside. A
// rendered file missing from disk counts as edited; it was deleted.
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
			// Absent is usually a deletion, and a deletion is an edit. The one
			// exception is a board task file that is byte-identical at its
			// sibling path: that is a move the store did not record, not
			// something someone wrote (adb-archive-store-desync). Refusing it
			// meant a single board click blocked every later write, with a
			// message about hand-editing that named a state the board itself
			// created. Anything whose bytes differ is still an edit.
			if !movedIntact(l, rel, want) {
				edited = append(edited, rel)
			}
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

// movedIntact reports whether a rendered board file is missing from its own
// path only because it sits, byte for byte, at the other one. The next render
// puts it back where the store says it belongs and clears the sibling, so
// there is nothing to lose by proceeding — which is exactly what makes this
// safe to wave through and a differing copy not.
func movedIntact(l Layout, rel string, want []byte) bool {
	sib := boardSibling(rel)
	if sib == "" {
		return false
	}
	got, err := os.ReadFile(l.path(sib))
	return err == nil && bytes.Equal(got, want)
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
