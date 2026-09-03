package convert

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

// TestRerunCarriesEverySubItemKind converts the fixture corpus twice into
// one store and asserts every sub-item ULID is identical across the
// re-run, for every slice field on store.Ticket whose element type
// carries an ID — enumerated by reflection rather than hand-listed, since
// a hand-listed helper (snapshotIDs, used by TestDeterministicRerun) is
// exactly what let carrySubItemIDs's gap on links/code refs/needs-you/
// waiting-on survive undetected (adb-worklog2-migrate).
func TestRerunCarriesEverySubItemKind(t *testing.T) {
	for name, s := range stores(t) {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(s, corpus()); err != nil {
				t.Fatal(err)
			}
			first := allSubItemIDs(t, s)
			if _, err := Load(s, corpus()); err != nil {
				t.Fatal(err)
			}
			second := allSubItemIDs(t, s)

			if len(first) != len(second) {
				t.Fatalf("sub-item count changed: %d vs %d", len(first), len(second))
			}
			for k, v := range first {
				if second[k] != v {
					t.Errorf("%s: id changed across re-run: %s vs %s", k, v, second[k])
				}
			}

			// Every kind the fixture actually populates must appear at
			// least once, so this test cannot pass by finding nothing.
			want := []string{"PlanSteps", "Scorecard", "NoteEntries", "Links", "CodeRefs", "NeedsYou", "WaitingOn"}
			seen := make(map[string]bool)
			for k := range first {
				for _, w := range want {
					if strings.Contains(k, "/"+w+"/") {
						seen[w] = true
					}
				}
			}
			for _, w := range want {
				if !seen[w] {
					t.Errorf("fixture never exercised sub-item kind %q — test cannot prove it carries", w)
				}
			}
		})
	}
}

// allSubItemIDs walks every ticket's slice fields via reflection, and for
// every element type carrying a store.ID-typed "ID" field, records that
// ID keyed by ticket slug + field name + element position. Position is a
// safe key here (not an identity claim): both runs convert the identical
// fixture corpus in the same order, so a real ID swap shows up as a
// mismatch at the same position.
func allSubItemIDs(t *testing.T, s store.Store) map[string]store.ID {
	t.Helper()
	out := make(map[string]store.ID)
	tickets, err := s.Tickets()
	if err != nil {
		t.Fatal(err)
	}
	idType := reflect.TypeOf(store.ID(""))
	for _, tk := range tickets {
		v := reflect.ValueOf(tk).Elem()
		ty := v.Type()
		for i := 0; i < ty.NumField(); i++ {
			field := ty.Field(i)
			fv := v.Field(i)
			if fv.Kind() != reflect.Slice || fv.Type().Elem().Kind() != reflect.Struct {
				continue
			}
			idField, ok := fv.Type().Elem().FieldByName("ID")
			if !ok || idField.Type != idType {
				continue
			}
			for j := 0; j < fv.Len(); j++ {
				id := fv.Index(j).FieldByName("ID").Interface().(store.ID)
				key := fmt.Sprintf("%s/%s/%d", tk.Slug, field.Name, j)
				out[key] = id
			}
		}
	}
	return out
}
