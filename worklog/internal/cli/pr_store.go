package cli

import (
	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/pr"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

// runStorePR is pr.SetPR's store-backed twin (adb-cutover M3d). The
// read path (pr.Get, "worklog pr <id>" with no value) stays on the
// legacy parser unconditionally — write-through keeps WORK.md current
// after every store-backed write, so parsing it is correct under either
// backend and porting a read-only path would add code without changing
// behavior.
func runStorePR(wd model.Workdir, id, value string) (pr.Result, error) {
	ss, err := openStoreForWrite(wd)
	if err != nil {
		return pr.Result{}, err
	}
	defer ss.close()

	t, err := ss.ticketBySlugOrErr(id, pr.ErrIDNotFound)
	if err != nil {
		return pr.Result{}, err
	}

	previous := ""
	if t.PR != nil {
		previous = *t.PR
	}
	t.PR = &value
	setPRLink(t, value)

	if err := ss.commit(t); err != nil {
		return pr.Result{}, err
	}

	var parentSlug string
	if t.ParentID != "" {
		if p, err := ss.s.Ticket(t.ParentID); err == nil {
			parentSlug = p.Slug
		}
	}

	return pr.Result{ID: t.Slug, PR: value, Previous: previous, Parent: parentSlug}, nil
}

// setPRLink mirrors t.PR onto t.Links so the devboard board card — which
// renders its PR link from the Links relation, not the WORK.md-only PR
// field (projection.BoardTask/fillBoard) — stays in sync. Matches
// legacy's devboardOnPR(Child): replace-not-append, and a cleared PR
// drops the link entirely rather than leaving an empty one (ValidateTicket
// allows at most one pr-kind link).
func setPRLink(t *store.Ticket, url string) {
	kept := t.Links[:0]
	for _, l := range t.Links {
		if l.Kind != store.LinkPR {
			kept = append(kept, l)
		}
	}
	t.Links = kept
	if url != "" {
		t.Links = append(t.Links, store.Link{Kind: store.LinkPR, URL: url})
	}
}
