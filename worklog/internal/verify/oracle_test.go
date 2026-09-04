package verify

import (
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/store"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/memstore"
)

func seed(t *testing.T, mut func(*store.Ticket)) store.Store {
	t.Helper()
	s := memstore.New()
	tk := &store.Ticket{
		ID: store.NewID(), Slug: "solo", Title: "A ticket",
		Type: store.TypeTicket, State: store.StatePending, Section: "next",
	}
	if mut != nil {
		mut(tk)
	}
	if err := s.PutTicket(tk); err != nil {
		t.Fatal(err)
	}
	return s
}

// TestOracleSeesFieldsTheViewOmits is criterion 18. Each of these fields is
// absent from verify's nine-field workmd view, so a difference in any of
// them compared silently clean before the oracle existed.
func TestOracleSeesFieldsTheViewOmits(t *testing.T) {
	for name, mut := range map[string]func(*store.Ticket){
		"Section":     func(tk *store.Ticket) { tk.Section = "someday" },
		"Status":      func(tk *store.Ticket) { tk.Status = "blocked-on-review" },
		"ExtraFields": func(tk *store.Ticket) { tk.ExtraFields = map[string]string{"Rollout": "staged"} },
		"Rank":        func(tk *store.Ticket) { tk.Rank = 7 },
	} {
		t.Run(name, func(t *testing.T) {
			drift, err := canonicalDrift(seed(t, nil), seed(t, mut))
			if err != nil {
				t.Fatal(err)
			}
			if len(drift) == 0 {
				t.Fatalf("a difference in %s went unreported; the oracle must cover every store.Ticket field", name)
			}
			if drift[0].Class != ClassRenderer {
				t.Errorf("Class = %q, want %q so it is distinguishable from a stale file", drift[0].Class, ClassRenderer)
			}
		})
	}
}

// TestOracleQuietWhenIdentical keeps the check from being trivially noisy.
func TestOracleQuietWhenIdentical(t *testing.T) {
	drift, err := canonicalDrift(seed(t, nil), seed(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(drift) != 0 {
		t.Errorf("identical stores reported drift: %+v", drift)
	}
}
