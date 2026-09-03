package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/prestontallen/ai-devboard/worklog/internal/convert"
	"github.com/prestontallen/ai-devboard/worklog/internal/note"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

// runStoreNoteAppend is note.Append's store-backed twin (adb-cutover
// M3d). Under the store model there is no separate "link the WORK.md
// block to its notes file" step: WorkMD's renderer adds the **Notes**:
// line automatically whenever a ticket has any notes content, so
// LinkedInWorkMD is unconditionally true after a successful append.
func runStoreNoteAppend(ss *storeSession, id, body string, now time.Time) (note.AppendResult, error) {
	t, err := ss.ticketBySlugOrErr(id, note.ErrUnknownID)
	if err != nil {
		return note.AppendResult{}, err
	}

	createdFile := t.NotesPreamble == "" && len(t.NoteEntries) == 0
	timestamp := now.Local().Format("2006-01-02 15:04")
	t.NoteEntries = append(t.NoteEntries, store.NoteEntry{Stamp: timestamp, Body: body})

	if err := ss.commit(t); err != nil {
		return note.AppendResult{}, err
	}

	return note.AppendResult{
		ID:             t.Slug,
		File:           ss.wd.NotesFile(t.Slug),
		Appended:       note.Entry{Timestamp: timestamp, Body: body},
		TotalEntries:   len(t.NoteEntries),
		CreatedFile:    createdFile,
		LinkedInWorkMD: true,
	}, nil
}

// runStoreNoteEditor is note --editor's store-backed twin. Per M3b's
// ratified resolution, --editor is the single sanctioned ingest path
// once every projection is output-only: it seeds a file with the
// ticket's current notes content (scaffolded if empty), opens $EDITOR
// on it, and on a clean exit parses whatever was saved back into the
// store — the only place raw notes text re-enters canon.
//
// editFn runs the editor process against seedPath and reports success;
// the caller supplies it so this stays testable without spawning a real
// editor.
func runStoreNoteEditor(ss *storeSession, id string, editFn func(seedPath string) error) (path string, created bool, err error) {
	t, err := ss.ticketBySlugOrErr(id, note.ErrUnknownID)
	if err != nil {
		return "", false, err
	}

	seedPath := ss.wd.NotesFile(t.Slug)
	created = t.NotesPreamble == "" && len(t.NoteEntries) == 0
	if err := os.MkdirAll(ss.wd.NotesDir(), 0o755); err != nil {
		return "", false, fmt.Errorf("mkdir notes: %w", err)
	}
	if err := os.WriteFile(seedPath, noteEditorSeed(t), 0o644); err != nil {
		return "", false, fmt.Errorf("seeding notes file: %w", err)
	}

	if err := editFn(seedPath); err != nil {
		return seedPath, created, err
	}

	edited, err := os.ReadFile(seedPath)
	if err != nil {
		return seedPath, created, fmt.Errorf("reading edited notes: %w", err)
	}
	nf := convert.Notes(edited)
	carryNoteEntryIDs(t.NoteEntries, nf.Entries)
	t.NotesPreamble = nf.Preamble
	t.NoteEntries = nf.Entries

	if err := ss.commit(t); err != nil {
		return seedPath, created, err
	}
	return seedPath, created, nil
}

// noteEditorSeed renders a ticket's notes content for a --editor session:
// its current preamble/entries with no generated-file banner (this file
// is about to be hand-edited by design), or a friendly scaffold when the
// ticket has no notes yet.
func noteEditorSeed(t *store.Ticket) []byte {
	if t.NotesPreamble == "" && len(t.NoteEntries) == 0 {
		return []byte("# Notes — " + t.Slug + "\n\n")
	}
	var b strings.Builder
	if t.NotesPreamble != "" {
		b.WriteString(t.NotesPreamble)
		b.WriteString("\n")
	}
	for _, e := range t.NoteEntries {
		fmt.Fprintf(&b, "\n## %s\n", e.Stamp)
		if e.Body != "" {
			b.WriteString(e.Body)
			b.WriteString("\n")
		}
	}
	return []byte(b.String())
}

// carryNoteEntryIDs preserves a note entry's ULID across an --editor save
// when its (Stamp, Body) content is unchanged — a lighter-weight, local
// version of the converter's carrySubItemIDs, since that helper is
// unexported and re-run identity for note entries has no functional
// consumer today (nothing indexes into NoteEntries by ID). Content that
// changed or is new mints a fresh ID via PutTicket's MintSubItemIDs, same
// as any other sub-item.
func carryNoteEntryIDs(before, after []store.NoteEntry) {
	byContent := make(map[[2]string]store.ID, len(before))
	for _, e := range before {
		byContent[[2]string{e.Stamp, e.Body}] = e.ID
	}
	for i, e := range after {
		if id, ok := byContent[[2]string{e.Stamp, e.Body}]; ok {
			after[i].ID = id
		}
	}
}
