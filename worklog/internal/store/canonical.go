package store

import (
	"encoding/json"
	"sort"
)

// canonTicket is a ticket prepared for comparison: the embedded Ticket
// marshals its fields inline, so EVERY exported field participates by
// construction. That is the point — a field added to Ticket later is
// compared automatically instead of quietly escaping an explicit list,
// which is how internal/verify's nine-field view came to omit Section,
// Status, Plan, Links and the rest.
//
// ParentSlug replaces ParentID because a ULID is minted per install: two
// stores holding identical facts disagree on every ID, so raw IDs cannot
// be compared across a conversion. The slug is the stable name.
type canonTicket struct {
	*Ticket
	ParentSlug string
}

// Canonical renders s as a deterministic, identity-free JSON document
// suitable for proving two stores hold the same facts — the store side of
// the adoption oracle.
//
// What it removes is exactly what is minted rather than observed: ticket
// and sub-item ULIDs, and ParentID (carried as ParentSlug instead).
// Everything else is compared, including ExtraFields, which is the whole
// reason unmodeled `- **Field**: value` bullets are durable.
//
// Callers may pass any Store. The tickets it mutates are the copies
// Tickets() returns (memstore clones, sqlitestore builds from rows), so
// the store itself is untouched.
func Canonical(s Store) ([]byte, error) {
	tickets, err := s.Tickets()
	if err != nil {
		return nil, err
	}

	slugOf := make(map[ID]string, len(tickets))
	for _, t := range tickets {
		slugOf[t.ID] = t.Slug
	}

	out := make([]canonTicket, 0, len(tickets))
	for _, t := range tickets {
		c := canonTicket{Ticket: t}
		if t.ParentID != "" {
			c.ParentSlug = slugOf[t.ParentID]
		}
		t.ID = ""
		t.ParentID = ""
		zeroSubItemIDs(t)
		out = append(out, c)
	}
	// Slug is unique per store, so this is a total order and the output is
	// stable across runs and implementations.
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })

	fb, err := s.Feedback()
	if err != nil {
		return nil, err
	}
	for _, e := range fb {
		e.ID = ""
	}
	sort.Slice(fb, func(i, j int) bool {
		if fb[i].Seconds != fb[j].Seconds {
			return fb[i].Seconds < fb[j].Seconds
		}
		if fb[i].Signal != fb[j].Signal {
			return fb[i].Signal < fb[j].Signal
		}
		return fb[i].Trigger < fb[j].Trigger
	})

	return json.MarshalIndent(map[string]any{"tickets": out, "feedback": fb}, "", " ")
}

// zeroSubItemIDs clears every minted sub-item ULID. Ranks are deliberately
// left alone: a sub-item's rank is observed order, not minted identity, and
// losing it is exactly the class of drift this oracle exists to catch.
func zeroSubItemIDs(t *Ticket) {
	for i := range t.PlanSteps {
		t.PlanSteps[i].ID = ""
	}
	for i := range t.Scorecard {
		t.Scorecard[i].ID = ""
	}
	for i := range t.Decisions {
		t.Decisions[i].ID = ""
	}
	for i := range t.CodeRefs {
		t.CodeRefs[i].ID = ""
	}
	for i := range t.NeedsYou {
		t.NeedsYou[i].ID = ""
	}
	for i := range t.WaitingOn {
		t.WaitingOn[i].ID = ""
	}
	for i := range t.Links {
		t.Links[i].ID = ""
	}
	for i := range t.Transitions {
		t.Transitions[i].ID = ""
	}
	for i := range t.NoteEntries {
		t.NoteEntries[i].ID = ""
	}
}
