package cli

import (
	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/pr"
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
