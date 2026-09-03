package cli

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/prestontallen/ai-devboard/worklog/internal/done"
	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

// doneDateRe mirrors internal/done's unexported dateRe — kept local since
// the store path validates the same YYYY-MM-DD shape before touching the
// store.
var doneDateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// runStoreDone is done's store-backed twin (adb-cutover M3d). Under the
// legacy model, archiving a child costs four non-atomic writes: the
// archive entry, the parent's notes-file checkbox flip, the parent's
// Active-children removal, and the WORK.md block removal. Under the
// store model this collapses to one PutTicket: Section/Active-children
// are derived from the relation and the archived state, so setting
// Archived/State/Completed on the ticket IS the whole write — the
// notes-file checkbox never existed as a separate representation (a
// child is its own store row with ParentID), so there is nothing left to
// flip or remove.
func runStoreDone(wd model.Workdir, in done.Inputs, today string) (done.Output, error) {
	completed := strings.TrimSpace(in.Completed)
	if completed == "" {
		completed = today
	}
	if !doneDateRe.MatchString(completed) {
		return done.Output{}, fmt.Errorf("%w: %q", done.ErrInvalidDate, completed)
	}
	month := completed[:7]

	ss, err := openStoreForWrite(wd)
	if err != nil {
		return done.Output{}, err
	}
	defer ss.close()

	t, err := ss.ticketBySlugOrErr(in.ID, done.ErrIDNotFound)
	if err != nil {
		return done.Output{}, err
	}

	if t.Type == store.TypeEpic {
		return runStoreDoneEpic(ss, t, in, completed, month)
	}
	if strings.TrimSpace(in.Summary) == "" {
		return done.Output{}, done.ErrSummaryRequired
	}

	if pr := strings.TrimSpace(in.PR); pr != "" {
		t.PR = &pr
	}
	t.Archived = true
	t.Section = ""
	t.State = store.StateDone
	t.Completed = completed
	t.ArchiveMonth = month
	t.Summary = strings.TrimSpace(in.Summary)
	t.ArchiveFeedback = trimAll(in.Feedback)
	t.TimeSpent = strings.TrimSpace(in.Time)

	if err := ss.commit(t); err != nil {
		return done.Output{}, err
	}

	var epicCompletable bool
	var parentSlug string
	if t.ParentID != "" {
		if p, err := ss.s.Ticket(t.ParentID); err == nil {
			parentSlug = p.Slug
		}
		if kids, err := ss.s.Children(t.ParentID); err == nil {
			epicCompletable = true
			for _, k := range kids {
				if k.Open() {
					epicCompletable = false
					break
				}
			}
		}
	}

	return done.Output{
		Status:          "archived",
		ID:              t.Slug,
		Title:           t.Title,
		ArchivePath:     wd.ArchiveFile(month),
		Completed:       completed,
		Parent:          parentSlug,
		EpicCompletable: epicCompletable,
	}, nil
}

// runStoreDoneEpic archives an epic: refuse if any child is still open
// (State != done — the store-level equivalent of the legacy notes-file
// checkbox scan and the WORK.md Parent-field scan, unified because every
// child is now one relation regardless of how it originated).
func runStoreDoneEpic(ss *storeSession, t *store.Ticket, in done.Inputs, completed, month string) (done.Output, error) {
	kids, err := ss.s.Children(t.ID)
	if err != nil {
		return done.Output{}, err
	}
	var open []string
	for _, k := range kids {
		if k.Open() {
			open = append(open, k.Slug)
		}
	}
	if len(open) > 0 {
		return done.Output{}, fmt.Errorf("%w: %s", done.ErrEpicHasOpenChildren, strings.Join(open, ", "))
	}
	if strings.TrimSpace(in.Summary) == "" {
		return done.Output{}, done.ErrSummaryRequired
	}

	if pr := strings.TrimSpace(in.PR); pr != "" {
		t.PR = &pr
	}
	t.Archived = true
	t.Section = ""
	t.State = store.StateDone
	t.Completed = completed
	t.ArchiveMonth = month
	t.Summary = strings.TrimSpace(in.Summary)
	t.ArchiveFeedback = trimAll(in.Feedback)
	t.TimeSpent = strings.TrimSpace(in.Time)

	if err := ss.commit(t); err != nil {
		return done.Output{}, err
	}

	return done.Output{
		Status:      "archived",
		Type:        "epic",
		ID:          t.Slug,
		Title:       t.Title,
		ArchivePath: ss.wd.ArchiveFile(month),
		Completed:   completed,
	}, nil
}
