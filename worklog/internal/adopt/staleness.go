package adopt

import (
	"sort"

	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

// StaleRows returns ticket slugs present in the store but not among the
// slugs this run's conversion actually touched — leftovers from an earlier
// generation that no longer exist in live data.
//
// It lives here because adoption is its only caller and the package it came
// from is being deleted. The check exists because convert.Load only upserts
// (the Store has no delete), so an id-set diff alone reports these tickets
// as "unchanged" rather than as gone. Without it, a row with no corpus
// counterpart would be rendered back onto disk: a ticket the human deleted,
// resurrected by the next write.
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
