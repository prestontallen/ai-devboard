// Package memstore is the in-memory Store implementation. It exists to
// prove the Store boundary holds: every behavioral guarantee the SQLite
// implementation makes (validation, slug uniqueness across history,
// journaling, decision dedupe, sub-item ID stability) is honored here
// too, and the round-trip suite runs against both.
package memstore

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

type Mem struct {
	mu       sync.Mutex
	tickets  map[store.ID]*store.Ticket
	bySlug   map[string]store.ID // NormalizeSlug -> id; never released (history reserves slugs)
	feedback []*store.FeedbackEntry
	journal  map[store.ID][]store.FieldChange
}

func New() *Mem {
	return &Mem{
		tickets: make(map[store.ID]*store.Ticket),
		bySlug:  make(map[string]store.ID),
		journal: make(map[store.ID][]store.FieldChange),
	}
}

func clone[T any](v T) T {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("memstore clone: %v", err))
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		panic(fmt.Sprintf("memstore clone: %v", err))
	}
	return out
}

func (m *Mem) PutTicket(t *store.Ticket) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := store.ValidateTicket(t); err != nil {
		return err
	}
	if t.ID == "" {
		t.ID = store.NewID()
	}
	slug := store.NormalizeSlug(t.Slug)
	if slug != "" {
		if owner, ok := m.bySlug[slug]; ok && owner != t.ID {
			return fmt.Errorf("slug %q already used by another ticket (slugs are reserved across history)", slug)
		}
	}

	prev := m.tickets[t.ID]
	if prev != nil && store.NormalizeSlug(prev.Slug) != slug {
		// Slug is mutable, but the old alias stays reserved and keeps
		// resolving to this ticket.
	}

	// Dedupe before minting so ranks stay contiguous on the surviving set.
	t.Decisions = store.DedupeDecisions(t.Decisions)
	store.MintSubItemIDs(t)

	now := time.Now().UTC().Format(time.RFC3339)
	for _, ch := range store.DiffScalars(prev, t) {
		ch.ID = store.NewID()
		ch.Entity = t.ID
		ch.At = now
		m.journal[t.ID] = append(m.journal[t.ID], ch)
	}

	stored := clone(t)
	m.tickets[t.ID] = stored
	if slug != "" {
		m.bySlug[slug] = t.ID
	}
	return nil
}

func (m *Mem) Ticket(id store.ID) (*store.Ticket, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tickets[id]
	if !ok {
		return nil, store.NotFound("ticket " + string(id))
	}
	return clone(t), nil
}

func (m *Mem) TicketBySlug(slug string) (*store.Ticket, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.bySlug[store.NormalizeSlug(slug)]
	if !ok {
		return nil, store.NotFound("slug " + slug)
	}
	return clone(m.tickets[id]), nil
}

func (m *Mem) Tickets() ([]*store.Ticket, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*store.Ticket, 0, len(m.tickets))
	for _, t := range m.tickets {
		out = append(out, clone(t))
	}
	// Matches sqlitestore's "ORDER BY rank, slug": rank carries the human's
	// document order, slug keeps the result total for rows that share one.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Rank != out[j].Rank {
			return out[i].Rank < out[j].Rank
		}
		return out[i].Slug < out[j].Slug
	})
	return out, nil
}

func (m *Mem) Children(parent store.ID) ([]*store.Ticket, error) {
	all, err := m.Tickets()
	if err != nil {
		return nil, err
	}
	out := make([]*store.Ticket, 0)
	for _, t := range all {
		if t.ParentID == parent {
			out = append(out, t)
		}
	}
	// Roster order, matching sqlitestore's ORDER BY on this method. NOT
	// Rank — a child's Rank is its position in WORK.md or an archive
	// month, which says nothing about its place in the parent's roster.
	sort.Slice(out, func(i, j int) bool {
		if out[i].RosterRank != out[j].RosterRank {
			return out[i].RosterRank < out[j].RosterRank
		}
		return out[i].Slug < out[j].Slug
	})
	return out, nil
}

func (m *Mem) PutFeedback(e *store.FeedbackEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e.Signal == "" || e.Trigger == "" {
		return fmt.Errorf("feedback: signal and trigger are required")
	}
	if e.ID == "" {
		e.ID = store.NewID()
	}
	for i, old := range m.feedback {
		if old.ID == e.ID {
			m.feedback[i] = clone(e)
			return nil
		}
	}
	m.feedback = append(m.feedback, clone(e))
	return nil
}

func (m *Mem) Feedback() ([]*store.FeedbackEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*store.FeedbackEntry, 0, len(m.feedback))
	for _, e := range m.feedback {
		out = append(out, clone(e))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Seconds < out[j].Seconds })
	return out, nil
}

func (m *Mem) Journal(entity store.ID) ([]store.FieldChange, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]store.FieldChange(nil), m.journal[entity]...), nil
}

func (m *Mem) Close() error { return nil }

var _ store.Store = (*Mem)(nil)

// String aids test failure output.
func (m *Mem) String() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	slugs := make([]string, 0, len(m.bySlug))
	for s := range m.bySlug {
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)
	return "memstore[" + strings.Join(slugs, " ") + "]"
}
