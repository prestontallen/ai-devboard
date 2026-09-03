package convert

import (
	"fmt"
	"sort"
	"strings"

	"github.com/prestontallen/ai-devboard/worklog/internal/feedback"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

// Corpus is an in-memory snapshot of the seven representations. The
// caller reads the files (the converter itself never touches the live
// dir — rehearsals convert a copy).
type Corpus struct {
	WorkMD   []byte
	Archives map[string][]byte // "2026-09" -> archive file bytes
	Notes    map[string][]byte // slug -> notes file bytes
	Board    []BoardInput
	Feedback []byte // FEEDBACK.md, parsed via the CLI's own package
}

type BoardInput struct {
	Repo     string // group directory name (projection detail, not authority)
	Name     string // filename, for error messages
	Archived bool   // sat in _archive/
	Data     []byte
}

// Report is what a conversion did — and every reason it refused or warned.
type Report struct {
	Tickets  int
	Feedback int
	Skipped  []string // bare producer files left alone
	Warnings []string // lint-grade oddities preserved verbatim (e.g. space-separated tags)
}

// Load converts a corpus into the store. Deterministic across re-runs
// into the same store: ticket IDs resolve by slug before minting (D4).
// Refusals (criterion 12): duplicate slugs, board joins to unknown
// tickets, notes for unknown tickets, epic-link inconsistency, unmodeled
// content (surfaced by the parsers).
func Load(s store.Store, c Corpus) (*Report, error) {
	rep := &Report{}
	frags := make(map[string]*store.Ticket)
	var order []string

	bare := 0
	add := func(t *store.Ticket, origin string) error {
		key := t.Slug
		if key == "" {
			// Slug-less quick-capture blocks: legal, keyed synthetically
			// for this pass only (identity is the minted ULID).
			bare++
			key = fmt.Sprintf("\x00bare-%d", bare)
		} else if _, dup := frags[key]; dup {
			return fmt.Errorf("%s: duplicate slug %q — ids are unique across live and archive", origin, t.Slug)
		}
		frags[key] = t
		order = append(order, key)
		return nil
	}

	live, err := WorkMD(c.WorkMD)
	if err != nil {
		return nil, err
	}
	for _, t := range live {
		if err := add(t, "WORK.md"); err != nil {
			return nil, err
		}
	}
	months := make([]string, 0, len(c.Archives))
	for m := range c.Archives {
		months = append(months, m)
	}
	sort.Strings(months)
	for _, m := range months {
		archived, err := ArchiveMonth(m, c.Archives[m])
		if err != nil {
			return nil, err
		}
		for _, t := range archived {
			if err := add(t, "archive/"+m+".md"); err != nil {
				return nil, err
			}
		}
	}

	// Devboard in-flight detail merges onto existing fragments; epic
	// children may introduce fragments of their own only if the child is
	// already known from WORK.md/notes/archive (otherwise the board is
	// claiming a ticket that doesn't exist — refuse).
	for _, in := range c.Board {
		bf, err := DevboardYAML(in.Name, in.Data, in.Archived)
		if err != nil {
			return nil, err
		}
		if bf.Join == "" {
			rep.Skipped = append(rep.Skipped, in.Repo+"/"+in.Name)
			continue
		}
		target, ok := frags[bf.Join]
		if !ok {
			return nil, fmt.Errorf("%s/%s: worklog join %q matches no ticket", in.Repo, in.Name, bf.Join)
		}
		mergeBoard(target, bf.Fragment)
		for _, kid := range bf.Children {
			kt, ok := frags[kid.Slug]
			if !ok {
				// A pending child may exist only on the epic's notes
				// roster; that fragment is created in the notes pass.
				// Board children beyond roster+WORK.md are refused later
				// by the consistency check; stash for merge now.
				kt = &store.Ticket{
					Slug: kid.Slug, Title: kid.Title, Type: store.TypeTicket,
					State: firstNonEmpty(kid.State, store.StatePending),
				}
				if err := add(kt, in.Repo+"/"+in.Name+" children"); err != nil {
					return nil, err
				}
			}
			mergeBoard(kt, kid)
			kt.ExtraFields = addField(kt.ExtraFields, "__parent_slug", bf.Join)
		}
	}

	// Notes: preamble + entries attach to their ticket; epic rosters
	// introduce pending children not seen anywhere else (add --parent
	// writes only the roster line).
	noteSlugs := make([]string, 0, len(c.Notes))
	for slug := range c.Notes {
		noteSlugs = append(noteSlugs, slug)
	}
	sort.Strings(noteSlugs)
	for _, slug := range noteSlugs {
		t, ok := frags[slug]
		if !ok {
			return nil, fmt.Errorf("notes/%s.md: no ticket wears this slug", slug)
		}
		nf := Notes(c.Notes[slug])
		t.NotesPreamble = nf.Preamble
		t.NoteEntries = nf.Entries
		for _, r := range nf.Roster {
			kid, ok := frags[r.Slug]
			if !ok {
				kid = &store.Ticket{
					Slug: r.Slug, Title: r.Title, Type: store.TypeTicket, State: r.State,
				}
				if err := add(kid, "notes/"+slug+".md roster"); err != nil {
					return nil, err
				}
			}
			if kid.Title == "" {
				kid.Title = r.Title // pending-child titles live only here
			}
			kid.ExtraFields = addField(kid.ExtraFields, "__parent_slug", slug)
		}
	}

	// Archived epics' Children CSV becomes the relation (the roster is a
	// view of it from then on).
	for _, t := range frags {
		if ac := t.ExtraFields["__archived_children"]; ac != "" {
			for _, kidSlug := range splitCSV(ac) {
				kid, ok := frags[kidSlug]
				if !ok {
					return nil, fmt.Errorf("archived epic %s: Children names %q, which does not exist", t.Slug, kidSlug)
				}
				if kid.ExtraFields["__parent_slug"] == "" {
					kid.ExtraFields = addField(kid.ExtraFields, "__parent_slug", t.Slug)
				}
			}
		}
	}

	if err := checkEpicConsistency(frags); err != nil {
		return nil, err
	}
	for _, t := range frags {
		for _, tag := range t.Tags {
			if strings.Contains(tag, " ") {
				rep.Warnings = append(rep.Warnings,
					fmt.Sprintf("%s: tag %q contains spaces (preserved verbatim; likely a CSV mistake)", t.Slug, tag))
			}
		}
	}

	// Two passes: mint/reuse IDs first, then resolve parent slugs to IDs
	// and write aggregates.
	existing, err := s.Tickets()
	if err != nil {
		return nil, err
	}
	for _, key := range order {
		t := frags[key]
		if t.Slug != "" {
			if prev, err := s.TicketBySlug(t.Slug); err == nil {
				t.ID = prev.ID // D4: re-runs reuse, the store is the id-map
				carrySubItemIDs(prev, t)
			} else if !store.IsNotFound(err) {
				return nil, err
			}
		} else {
			// Slug-less entities match by exact title so re-runs stay
			// deterministic for them too.
			for _, prev := range existing {
				if prev.Slug == "" && prev.Title == t.Title {
					t.ID = prev.ID
					carrySubItemIDs(prev, t)
					break
				}
			}
		}
		if t.ID == "" {
			t.ID = store.NewID()
		}
	}
	for _, key := range order {
		t := frags[key]
		if ps := t.ExtraFields["__parent_slug"]; ps != "" {
			parent, ok := frags[ps]
			if !ok {
				return nil, fmt.Errorf("%s: parent %q does not exist", t.Slug, ps)
			}
			t.ParentID = parent.ID
		}
		stripConverterKeys(t)
	}
	// Parents before children so the FK holds at every insert.
	for _, pass := range []bool{true, false} {
		for _, key := range order {
			t := frags[key]
			if (t.ParentID == "") != pass {
				continue
			}
			if err := s.PutTicket(t); err != nil {
				return nil, fmt.Errorf("put %s: %w", t.Slug, err)
			}
			rep.Tickets++
		}
	}

	if len(c.Feedback) > 0 {
		entries, err := feedback.ParseBytes(c.Feedback)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if err := s.PutFeedback(&store.FeedbackEntry{
				Seconds: e.Timestamp, Signal: string(e.Signal), Trigger: e.Trigger,
				Excerpt: e.Excerpt, Context: e.Context, Resolved: e.Resolved,
			}); err != nil {
				return nil, err
			}
			rep.Feedback++
		}
	}
	return rep, nil
}

// mergeBoard copies devboard in-flight fields onto the ticket fragment.
func mergeBoard(dst, src *store.Ticket) {
	dst.BoardTracked = true
	dst.BoardArchived = src.BoardArchived
	dst.Phase, dst.Tier, dst.Complexity = src.Phase, src.Tier, src.Complexity
	dst.Branch, dst.Session, dst.RepoPath = src.Branch, src.Session, src.RepoPath
	dst.Scout = src.Scout
	dst.PlanSteps, dst.Scorecard, dst.Decisions = src.PlanSteps, src.Scorecard, src.Decisions
	dst.CodeRefs, dst.NeedsYou, dst.WaitingOn = src.CodeRefs, src.NeedsYou, src.WaitingOn
	// Board links merge with WORK.md links; the pr-kind link wins over a
	// ref with the same URL.
	dst.Links = mergeLinks(dst.Links, src.Links)
	if len(src.Extra) > 0 {
		if dst.Extra == nil {
			dst.Extra = make(map[string]any)
		}
		for k, v := range src.Extra {
			dst.Extra[k] = v
		}
	}
}

func mergeLinks(a, b []store.Link) []store.Link {
	out := append([]store.Link(nil), b...)
	for _, l := range a {
		dup := false
		for _, e := range out {
			if e.URL == l.URL {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, l)
		}
	}
	return out
}

// checkEpicConsistency asserts today's three(+)-place agreement as a
// conversion precondition: every __active_children entry names a child
// that exists, is active, and points back.
func checkEpicConsistency(frags map[string]*store.Ticket) error {
	for slug, t := range frags {
		ac := t.ExtraFields["__active_children"]
		if ac == "" || strings.EqualFold(ac, "<none>") {
			continue
		}
		for _, kidSlug := range splitCSV(ac) {
			kid, ok := frags[kidSlug]
			if !ok {
				return fmt.Errorf("epic %s: Active-children names %q, which does not exist", slug, kidSlug)
			}
			if kid.ExtraFields["__parent_slug"] != slug {
				return fmt.Errorf("epic %s: Active-children names %q, whose parent is %q", slug, kidSlug, kid.ExtraFields["__parent_slug"])
			}
			if kid.State != store.StateActive {
				return fmt.Errorf("epic %s: Active-children names %q, which is not active", slug, kidSlug)
			}
		}
	}
	return nil
}

// carrySubItemIDs preserves sub-item ULIDs across converter re-runs by
// exact content match against the previously stored aggregate.
func carrySubItemIDs(prev, next *store.Ticket) {
	for i := range next.PlanSteps {
		for _, p := range prev.PlanSteps {
			if p.Text == next.PlanSteps[i].Text {
				next.PlanSteps[i].ID, next.PlanSteps[i].Rank = p.ID, p.Rank
				break
			}
		}
	}
	for i := range next.Scorecard {
		for _, p := range prev.Scorecard {
			if p.Text == next.Scorecard[i].Text {
				next.Scorecard[i].ID, next.Scorecard[i].Rank = p.ID, p.Rank
				break
			}
		}
	}
	for i := range next.Decisions {
		for _, p := range prev.Decisions {
			if p.What == next.Decisions[i].What && p.Why == next.Decisions[i].Why {
				next.Decisions[i].ID, next.Decisions[i].Rank = p.ID, p.Rank
				break
			}
		}
	}
	for i := range next.NoteEntries {
		for _, p := range prev.NoteEntries {
			if p.Stamp == next.NoteEntries[i].Stamp && p.Body == next.NoteEntries[i].Body {
				next.NoteEntries[i].ID, next.NoteEntries[i].Rank = p.ID, p.Rank
				break
			}
		}
	}
}

// stripConverterKeys removes the __-prefixed scratch fields the passes
// used; they are relations now, not data.
func stripConverterKeys(t *store.Ticket) {
	for k := range t.ExtraFields {
		if strings.HasPrefix(k, "__") {
			delete(t.ExtraFields, k)
		}
	}
	if len(t.ExtraFields) == 0 {
		t.ExtraFields = nil
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
