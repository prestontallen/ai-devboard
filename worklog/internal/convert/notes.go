package convert

import (
	"regexp"
	"strings"

	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

// The ratified segmentation rule (contract D6): ONLY a heading of exactly
// this shape starts a new journal entry. Content headings — including
// non-timestamp `## ` lines inside a body — are swallowed by the entry
// they appear in. Duplicate stamps are legal; order is identity's tie-break.
var noteStampRe = regexp.MustCompile(`^## (\d{4}-\d{2}-\d{2} \d{2}:\d{2})\s*$`)

// Child roster line inside the `## Children` preamble region:
// `- [ |x|~] <child-id>: <title>`.
var rosterRe = regexp.MustCompile(`^- \[([ x~])\] ([a-z0-9-]+): (.*)$`)

// NotesFile is a segmented notes file: verbatim preamble (everything
// before the first timestamp heading — title, scaffold comment, Children
// roster, Background prose), then entries.
type NotesFile struct {
	Preamble string
	Entries  []store.NoteEntry
	// Roster is the parsed `- [ ] child: title` lines found in the
	// preamble — a converter cross-check input, not canon (the child
	// relation is; these lines are projections of it).
	Roster []RosterLine
}

type RosterLine struct {
	State string // pending|active|done (checkbox char mapped)
	Slug  string
	Title string
}

// Notes segments a notes file per D6. It cannot fail: any byte layout is
// representable as preamble + verbatim bodies.
func Notes(data []byte) *NotesFile {
	lines := splitNorm(data)
	nf := &NotesFile{}
	var pre []string
	var cur *store.NoteEntry

	flush := func() {
		if cur != nil {
			cur.Body = strings.TrimRight(cur.Body, "\n")
			nf.Entries = append(nf.Entries, *cur)
			cur = nil
		}
	}

	for _, line := range lines {
		if m := noteStampRe.FindStringSubmatch(line); m != nil {
			flush()
			cur = &store.NoteEntry{Stamp: m[1]}
			continue
		}
		if cur != nil {
			cur.Body += line + "\n"
			continue
		}
		pre = append(pre, line)
		if m := rosterRe.FindStringSubmatch(line); m != nil {
			nf.Roster = append(nf.Roster, RosterLine{
				State: stateChars[m[1]],
				Slug:  store.NormalizeSlug(m[2]),
				Title: strings.TrimSpace(m[3]),
			})
		}
	}
	flush()
	nf.Preamble = strings.TrimRight(strings.Join(pre, "\n"), "\n")
	return nf
}
