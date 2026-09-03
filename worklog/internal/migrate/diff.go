package migrate

import (
	"fmt"
	"reflect"
	"sort"

	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

// IDSetDiff is the pre/post comparison of every ticket and sub-item ULID
// in the output db across one migrate run (criteria 1, 2, 3).
type IDSetDiff struct {
	Added     []string    `json:"added"`
	Removed   []string    `json:"removed"`
	Unchanged int         `json:"unchanged"`
	Changed   []ChangedID `json:"changed"` // same key, different ID — should never happen while D4 holds; reported, never dropped
}

// ChangedID is one entity/sub-item whose ULID differs between runs — the
// failure this whole tool exists to catch.
type ChangedID struct {
	Key string   `json:"key"`
	Old store.ID `json:"old"`
	New store.ID `json:"new"`
}

// snapshotFromTickets records every ticket ULID and every sub-item ULID
// (of a kind carrying one), keyed by ticket identity + field name +
// position. It walks store.Ticket by reflection rather than a hand-listed
// field set — a hand-listed helper (internal/convert's old snapshotIDs)
// is exactly what let carrySubItemIDs's gap on four sub-item kinds go
// undetected; this snapshot can't silently skip a kind the same way.
//
// Position is a safe part of the key here (not an identity claim):
// migrate converts a stable-ordered corpus, so a real identity break
// shows up as the SAME key resolving to a DIFFERENT id (see ChangedID),
// not as spurious adds/removes from reordering.
func snapshotFromTickets(tickets []*store.Ticket) map[string]store.ID {
	out := make(map[string]store.ID)
	idType := reflect.TypeOf(store.ID(""))
	for _, t := range tickets {
		ident := t.Slug
		if ident == "" {
			ident = "title:" + t.Title // slug-less entities match by title (convert.Load's own resolution rule)
		}
		out["ticket/"+ident] = t.ID

		v := reflect.ValueOf(t).Elem()
		ty := v.Type()
		for i := 0; i < ty.NumField(); i++ {
			fv := v.Field(i)
			if fv.Kind() != reflect.Slice || fv.Type().Elem().Kind() != reflect.Struct {
				continue
			}
			idField, ok := fv.Type().Elem().FieldByName("ID")
			if !ok || idField.Type != idType {
				continue
			}
			fieldName := ty.Field(i).Name
			for j := 0; j < fv.Len(); j++ {
				id := fv.Index(j).FieldByName("ID").Interface().(store.ID)
				out[fmt.Sprintf("%s/%s/%d", ident, fieldName, j)] = id
			}
		}
	}
	return out
}

// DiffIDs compares two snapshots of the same store, taken immediately
// before and immediately after one convert.Load call.
func DiffIDs(before, after map[string]store.ID) IDSetDiff {
	var d IDSetDiff
	for k, v := range after {
		old, ok := before[k]
		switch {
		case !ok:
			d.Added = append(d.Added, k)
		case old == v:
			d.Unchanged++
		default:
			d.Changed = append(d.Changed, ChangedID{Key: k, Old: old, New: v})
		}
	}
	for k := range before {
		if _, ok := after[k]; !ok {
			d.Removed = append(d.Removed, k)
		}
	}
	sort.Strings(d.Added)
	sort.Strings(d.Removed)
	sort.Slice(d.Changed, func(i, j int) bool { return d.Changed[i].Key < d.Changed[j].Key })
	return d
}

// StaleRows returns ticket slugs present in the db after this run but not
// among the slugs this run's conversion actually touched — leftovers from
// a previous copy-forward generation that no longer exist in live data.
// convert.Load only upserts (Store has no delete), so the id-set diff
// alone reports these tickets as "unchanged", never as gone; this
// restores the ticket-count/id-set check the ratified workstream brief
// (notes/adb-worklog-rewrite.md, 2026-09-02 20:04) asked for, which a
// db-generation-to-generation diff structurally cannot see on its own.
func StaleRows(afterTickets []*store.Ticket, convertedSlugs map[string]bool) []string {
	var stale []string
	for _, t := range afterTickets {
		if t.Slug == "" {
			continue // slug-less quick-capture entities have no slug to go stale by
		}
		if !convertedSlugs[t.Slug] {
			stale = append(stale, t.Slug)
		}
	}
	sort.Strings(stale)
	return stale
}
