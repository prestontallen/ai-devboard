package projection

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

var sectionOrder = []struct{ key, heading string }{
	{store.SectionNow, "Now"},
	{store.SectionWaiting, "Waiting"},
	{store.SectionNext, "Next"},
	{store.SectionSomeday, "Someday"},
	{store.SectionBlocked, "Blocked"},
}

var stateChar = map[string]string{
	store.StatePending: " ", store.StateActive: "~", store.StateDone: "x",
}

// WorkMD renders the front page. Field order follows the CLI's
// FormatTicketBlock convention, extended with the fields it omits (Link,
// Plan, unknown extras) so nothing the full-fidelity parser captured is
// dropped. The grammar stays exactly what parse.File accepts.
func WorkMD(tickets []*store.Ticket) []byte {
	byID := map[store.ID]*store.Ticket{}
	for _, t := range tickets {
		byID[t.ID] = t
	}
	var b bytes.Buffer
	banner(&b)
	b.WriteString("# Worklog — active\n")

	for _, sec := range sectionOrder {
		var in []*store.Ticket
		for _, t := range tickets {
			if !t.Archived && t.Section == sec.key {
				in = append(in, t)
			}
		}
		if len(in) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n## %s\n", sec.heading)
		for _, t := range in {
			// Blank line before every block. This is a deliberate format
			// choice, not splice residue: WORK.md is read by a human every
			// session, and ~20 backlog entries run together without it.
			// Matches ArchiveMonth's spacing of its own entries.
			b.WriteString("\n")
			renderBlock(&b, t, byID, tickets)
		}
	}
	return b.Bytes()
}

func renderBlock(b *bytes.Buffer, t *store.Ticket, byID map[store.ID]*store.Ticket, all []*store.Ticket) {
	ch := stateChar[t.State]
	if t.Slug == "" {
		fmt.Fprintf(b, "- [%s] %s\n", ch, t.Title)
		return
	}
	fmt.Fprintf(b, "- [%s] **%s** — %s\n", ch, strings.ToUpper(t.Slug), t.Title)
	fmt.Fprintf(b, "  - **ID**: %s\n", t.Slug)

	add := func(field, value string) {
		if value != "" {
			fmt.Fprintf(b, "  - **%s**: %s\n", field, value)
		}
	}
	if t.Type != store.TypeTicket {
		add("Type", t.Type)
	}
	if t.ParentID != "" {
		if p := byID[t.ParentID]; p != nil {
			add("Parent", p.Slug)
		}
	}
	add("Repo", t.Repo)
	add("Tags", strings.Join(t.Tags, ", "))
	// The PR line is always rendered — even empty — matching the CLI
	// renderer's deliberate "visibly available to fill in" behavior; an
	// absent PR (nil) renders no line at all.
	if t.PR != nil {
		fmt.Fprintf(b, "  - **PR**: %s\n", *t.PR)
	}
	add("Source", t.Source)
	for _, l := range t.Links {
		if l.Kind == store.LinkRef && l.Label != "" {
			add("Link", l.Label+" — "+l.URL)
		}
	}
	if t.NotesPreamble != "" || len(t.NoteEntries) > 0 {
		add("Notes", "notes/"+t.Slug+".md")
	}
	add("Started", t.Started)
	add("Waiting since", t.WaitingSince)
	add("Files", strings.Join(t.Files, ", "))
	add("Acceptance", t.Acceptance)
	add("Status", t.Status)
	add("Plan", t.PlanText)
	if t.Type == store.TypeEpic {
		var active []string
		for _, k := range all {
			if k.ParentID == t.ID && k.State == store.StateActive {
				active = append(active, k.Slug)
			}
		}
		sort.Strings(active)
		if len(active) == 0 {
			add("Active children", "<none>")
		} else {
			add("Active children", strings.Join(active, ", "))
		}
	}
	for _, k := range sortedKeys(t.ExtraFields) {
		add(k, t.ExtraFields[k])
	}
}
