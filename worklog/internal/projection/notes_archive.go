package projection

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

// NotesFile renders a ticket's notes projection: verbatim preamble
// (human-owned, including the Children roster region the CLI maintains),
// then each entry under its timestamp heading — the same segmentation
// grammar the converter parses (D6), so render∘parse is the identity on
// entries.
func NotesFile(t *store.Ticket) []byte {
	var b bytes.Buffer
	banner(&b)
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
	return b.Bytes()
}

// ArchiveMonth renders one archive file. Entries group under day headings
// by Completed date, newest day first. Parent and Children render from
// the single relation, never from stored fields.
func ArchiveMonth(month string, tickets, all []*store.Ticket) []byte {
	byID := map[store.ID]*store.Ticket{}
	for _, t := range all {
		byID[t.ID] = t
	}
	byDay := map[string][]*store.Ticket{}
	for _, t := range tickets {
		byDay[t.Completed] = append(byDay[t.Completed], t)
	}
	days := make([]string, 0, len(byDay))
	for d := range byDay {
		days = append(days, d)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(days)))

	var b bytes.Buffer
	banner(&b)
	fmt.Fprintf(&b, "# Archive — %s\n", month)
	for _, day := range days {
		fmt.Fprintf(&b, "\n## %s\n", day)
		ts := byDay[day]
		// Rank is the entry's position in the month file it came from, so
		// entries keep the order they were archived in. Slug is the
		// tiebreak for anything never ranked (archived by a store-backed
		// write, which assigns no document position).
		sort.Slice(ts, func(i, j int) bool {
			if ts[i].Rank != ts[j].Rank {
				return ts[i].Rank < ts[j].Rank
			}
			return ts[i].Slug < ts[j].Slug
		})
		for _, t := range ts {
			b.WriteString("\n")
			renderArchiveEntry(&b, t, byID, all)
		}
	}
	return b.Bytes()
}

func renderArchiveEntry(b *bytes.Buffer, t *store.Ticket, byID map[store.ID]*store.Ticket, all []*store.Ticket) {
	fmt.Fprintf(b, "### %s — %s\n", t.Slug, t.Title)
	add := func(field, value string) {
		if value != "" {
			fmt.Fprintf(b, "- **%s**: %s\n", field, value)
		}
	}
	add("Repo", t.Repo)
	add("Tags", joinCSV(t.Tags))
	if t.PR != nil && *t.PR != "" {
		add("PR", *t.PR)
	}
	add("Files", joinCSV(t.Files))
	// Parent renders from the relation; the field is a view of it.
	if t.ParentID != "" {
		if p := byID[t.ParentID]; p != nil {
			add("Parent", p.Slug)
		}
	}
	if t.Type != store.TypeTicket {
		add("Type", t.Type)
	}
	switch {
	case t.Started != "":
		add("Started → Completed", t.Started+" → "+t.Completed)
	case t.Completed != "":
		add("Completed", t.Completed)
	}
	if t.NotesPreamble != "" || len(t.NoteEntries) > 0 {
		add("Notes", "notes/"+t.Slug+".md")
	}
	add("Plan", t.PlanText)
	if t.Type == store.TypeEpic {
		var kids []*store.Ticket
		for _, k := range all {
			if k.ParentID == t.ID {
				kids = append(kids, k)
			}
		}
		// Roster order, not alphabetical: this CSV is the archived epic's
		// only remaining record of the order its children were added.
		sort.Slice(kids, func(i, j int) bool {
			if kids[i].RosterRank != kids[j].RosterRank {
				return kids[i].RosterRank < kids[j].RosterRank
			}
			return kids[i].Slug < kids[j].Slug
		})
		slugs := make([]string, 0, len(kids))
		for _, k := range kids {
			slugs = append(slugs, k.Slug)
		}
		add("Children", joinCSV(slugs))
	}
	add("Summary", t.Summary)
	if len(t.ArchiveFeedback) > 0 {
		b.WriteString("- **Feedback / Notes**:\n")
		for _, fb := range t.ArchiveFeedback {
			fmt.Fprintf(b, "  - %s\n", fb)
		}
	}
	add("Time", t.TimeSpent)
	for _, k := range sortedKeys(t.ExtraFields) {
		add(k, t.ExtraFields[k])
	}
}

func joinCSV(vals []string) string {
	out := ""
	for i, v := range vals {
		if i > 0 {
			out += ", "
		}
		out += v
	}
	return out
}
