// Package blockmap holds the single store.Ticket → model.Block
// correspondence: the shape WORK.md's blocks parse into, built straight
// from a ticket instead of by rendering markdown and parsing it back.
//
// It exists because the board's backlog lens needs blocks and the server
// must not read WORK.md off disk to get them (adb-serve-store-direct).
// It sits below both projection and serve so either can import the one
// encoding — projection's in-package tests import serve, so serve
// importing projection directly would be a cycle.
//
// The correspondence is defined by projection's WORK.md renderer, not
// invented here: Block(t) must equal what parse.File yields for the block
// renderBlock writes for the same ticket. That is a testable claim rather
// than a convention, and block_test.go's round-trip is what holds the two
// encodings together. Two independent encodings of one correspondence is
// what produced M2's scorecard `verify` bug, where the renderer emitted a
// key the legacy writer omitted.
package blockmap

import (
	"sort"
	"strings"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

// sections maps a store section key to the WORK.md heading it renders
// under. An archived ticket has no section and maps to "".
var sections = map[string]model.SectionName{
	store.SectionNow:     model.SectionNow,
	store.SectionWaiting: model.SectionWaiting,
	store.SectionNext:    model.SectionNext,
	store.SectionSomeday: model.SectionSomeday,
	store.SectionBlocked: model.SectionBlocked,
}

var states = map[string]model.State{
	store.StatePending: model.StatePending,
	store.StateActive:  model.StateActive,
	store.StateDone:    model.StateDone,
}

// Block builds one ticket's block. byID resolves the parent slug and all
// supplies the epic roster; both are the whole live set, exactly as the
// renderer takes them.
//
// StartLine and EndLine are left zero: they are positions in a file this
// never writes, and they carry `json:"-"` so nothing on the wire reads
// them.
func Block(t *store.Ticket, byID map[store.ID]*store.Ticket, all []*store.Ticket) model.Block {
	b := model.Block{
		Section: sections[t.Section],
		State:   states[t.State],
		Title:   t.Title,
		ID:      strings.ToLower(t.Slug),
		// The renderer omits the Type line for a plain ticket and the
		// parser defaults to it, so the round trip yields "ticket" either
		// way. Setting it unconditionally is the same answer without the
		// detour.
		Type:         model.BlockType(strings.ToLower(t.Type)),
		Repo:         t.Repo,
		Tags:         csv(t.Tags),
		Started:      t.Started,
		Source:       t.Source,
		WaitingSince: t.WaitingSince,
		Files:        csv(t.Files),
		Acceptance:   t.Acceptance,
		Status:       t.Status,
		Plan:         t.PlanText,
	}
	if b.Type == "" {
		b.Type = model.TypeTicket
	}
	if t.ParentID != "" {
		if p := byID[t.ParentID]; p != nil {
			b.Parent = strings.ToLower(p.Slug)
		}
	}
	// A nil PR renders no line and parses to ""; a non-nil empty one
	// renders an empty line and also parses to "". The distinction is
	// real in the store and invisible here, which is why Block cannot be
	// inverted back into a ticket.
	if t.PR != nil {
		b.PR = *t.PR
	}
	for _, l := range t.Links {
		if l.Kind == store.LinkRef && l.Label != "" {
			b.Links = append(b.Links, model.LinkEntry{Name: l.Label, URL: l.URL})
		}
	}
	if t.NotesPreamble != "" || len(t.NoteEntries) > 0 {
		b.NotesRef = "notes/" + t.Slug + ".md"
	}
	if t.Type == store.TypeEpic {
		b.ActiveChildren = activeChildren(t, all)
	}
	return b
}

// Blocks builds the blocks of one section, in the human's rank order.
// Archived tickets and children are both absent: a child renders inside
// its epic's roster, never as a block of its own.
func Blocks(tickets []*store.Ticket, section string) []model.Block {
	byID := map[store.ID]*store.Ticket{}
	for _, t := range tickets {
		byID[t.ID] = t
	}
	var in []*store.Ticket
	for _, t := range tickets {
		if !t.Archived && t.Section == section {
			in = append(in, t)
		}
	}
	// Rank then slug, the same total order both store implementations
	// return from Tickets(). Restating it here rather than inheriting the
	// caller's ordering means Blocks answers for its own output: rank
	// carries the human's document order, and slug keeps the result total
	// for rows that share one.
	sort.Slice(in, func(i, j int) bool {
		if in[i].Rank != in[j].Rank {
			return in[i].Rank < in[j].Rank
		}
		return in[i].Slug < in[j].Slug
	})
	out := make([]model.Block, 0, len(in))
	for _, t := range in {
		out = append(out, Block(t, byID, tickets))
	}
	return out
}

// activeChildren is the epic's roster in the order the human added its
// children, matching the renderer. Sorting these by slug silently
// re-ordered every epic on each render once before
// (adb-verify-child-order), so the tiebreak is explicit rather than
// incidental.
func activeChildren(t *store.Ticket, all []*store.Ticket) []string {
	var kids []*store.Ticket
	for _, k := range all {
		if k.ParentID == t.ID && k.State == store.StateActive {
			kids = append(kids, k)
		}
	}
	sort.Slice(kids, func(i, j int) bool {
		if kids[i].RosterRank != kids[j].RosterRank {
			return kids[i].RosterRank < kids[j].RosterRank
		}
		return kids[i].Slug < kids[j].Slug
	})
	if len(kids) == 0 {
		return nil // the renderer writes "<none>", which parses back to nil
	}
	out := make([]string, len(kids))
	for i, k := range kids {
		out[i] = strings.ToLower(k.Slug)
	}
	return out
}

// csv mirrors what a comma-joined field survives on the round trip: the
// renderer omits an empty list entirely, and the parser drops empty
// members, so neither an empty slice nor a blank member can come back.
func csv(in []string) []string {
	var out []string
	for _, v := range in {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
