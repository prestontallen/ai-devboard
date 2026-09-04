package store_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/store"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/memstore"
)

// canonTickets runs Canonical and returns the ticket objects as raw maps,
// keyed by slug.
func canonTickets(t *testing.T, s store.Store) map[string]map[string]json.RawMessage {
	t.Helper()
	data, err := store.Canonical(s)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Tickets []map[string]json.RawMessage `json:"tickets"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("Canonical produced unparseable JSON: %v", err)
	}
	out := map[string]map[string]json.RawMessage{}
	for _, tk := range doc.Tickets {
		var slug string
		if err := json.Unmarshal(tk["Slug"], &slug); err != nil {
			t.Fatalf("ticket without a Slug key: %v", err)
		}
		out[slug] = tk
	}
	return out
}

// TestCanonicalCoversEveryTicketField is the guarantee the oracle rests on:
// every exported field of store.Ticket participates in the comparison.
//
// internal/verify's comparator is an explicit nine-field list against a
// nineteen-field struct, so Section, Status, Plan, Source, Links,
// WaitingSince, Files, ActiveChildren and ExtraFields differ silently. This
// test fails the moment a field stops being covered, whether by someone
// adding one or by Canonical narrowing to a hand-maintained list.
func TestCanonicalCoversEveryTicketField(t *testing.T) {
	s := memstore.New()
	if err := s.PutTicket(&store.Ticket{ID: store.NewID(), Slug: "solo", Title: "t", Type: store.TypeTicket, State: store.StatePending}); err != nil {
		t.Fatal(err)
	}

	got := canonTickets(t, s)["solo"]
	if got == nil {
		t.Fatal("Canonical did not emit the ticket")
	}

	rt := reflect.TypeOf(store.Ticket{})
	var missing []string
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if f.PkgPath != "" {
			continue // unexported
		}
		if _, ok := got[f.Name]; !ok {
			missing = append(missing, f.Name)
		}
	}
	if len(missing) > 0 {
		t.Errorf("Canonical omits store.Ticket fields %v — a difference in any of them would compare clean", missing)
	}
}

// TestCanonicalKeepsChildExtraFields pins the bug carried by the test-only
// comparator this was promoted from: it replaced the whole ExtraFields map
// with {"parent": <slug>} for every child, so unmodeled `- **Field**:`
// bullets on a child were excluded from the comparison entirely.
func TestCanonicalKeepsChildExtraFields(t *testing.T) {
	s := memstore.New()
	epicID := store.NewID()
	if err := s.PutTicket(&store.Ticket{ID: epicID, Slug: "an-epic", Title: "e", Type: store.TypeEpic, State: store.StatePending}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutTicket(&store.Ticket{
		ID: store.NewID(), Slug: "a-kid", Title: "k", Type: store.TypeTicket, State: store.StatePending, ParentID: epicID,
		ExtraFields: map[string]string{"Rollout": "staged"},
	}); err != nil {
		t.Fatal(err)
	}

	kid := canonTickets(t, s)["a-kid"]
	if kid == nil {
		t.Fatal("Canonical did not emit the child")
	}

	var extras map[string]string
	if err := json.Unmarshal(kid["ExtraFields"], &extras); err != nil {
		t.Fatalf("ExtraFields unparseable: %v", err)
	}
	if extras["Rollout"] != "staged" {
		t.Errorf("child ExtraFields = %v, want the Rollout bullet preserved", extras)
	}
}

// TestCanonicalIsIdentityFree checks the other half: minted ULIDs cannot
// participate, since two stores holding identical facts mint different
// ones. The parent relation survives as a slug instead.
func TestCanonicalIsIdentityFree(t *testing.T) {
	s := memstore.New()
	epicID := store.NewID()
	if err := s.PutTicket(&store.Ticket{ID: epicID, Slug: "an-epic", Title: "e", Type: store.TypeEpic, State: store.StatePending}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutTicket(&store.Ticket{ID: store.NewID(), Slug: "a-kid", Title: "k", Type: store.TypeTicket, State: store.StatePending, ParentID: epicID}); err != nil {
		t.Fatal(err)
	}

	kid := canonTickets(t, s)["a-kid"]
	for _, key := range []string{"ID", "ParentID"} {
		var v string
		if err := json.Unmarshal(kid[key], &v); err != nil {
			t.Fatalf("%s unparseable: %v", key, err)
		}
		if v != "" {
			t.Errorf("%s = %q, want zeroed; a minted ULID cannot be compared across conversions", key, v)
		}
	}
	var parent string
	if err := json.Unmarshal(kid["ParentSlug"], &parent); err != nil {
		t.Fatalf("ParentSlug unparseable: %v", err)
	}
	if parent != "an-epic" {
		t.Errorf("ParentSlug = %q, want %q", parent, "an-epic")
	}
}
