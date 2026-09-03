package cli

import (
	"fmt"
	"strings"

	"github.com/prestontallen/ai-devboard/worklog/internal/devboard"
	"github.com/prestontallen/ai-devboard/worklog/internal/migrate"
	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/projection"
	"github.com/prestontallen/ai-devboard/worklog/internal/reindex"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/sqlitestore"
)

// storeSession is one write verb's open store handle plus the layout it
// renders into — the shared plumbing every ticket-shaped store-backed
// write verb (start/done/edit/pr/link/note/wait/add/import) opens once,
// mutates a *store.Ticket (or several) against, and commits. The
// task<sub> family (task_store.go) predates this helper and keeps its
// own inline version; not worth touching already-shipped, tested code
// to unify the ~15 lines of overlap.
type storeSession struct {
	s      store.Store
	layout projection.Layout
	wd     model.Workdir
}

// openStoreForWrite opens the store and refuses up front if any
// projection under wd/devboard has been hand-edited since the last
// render — the M3b policy: refuse before mutating, never after, since a
// render destroys whatever a human typed by hand.
func openStoreForWrite(wd model.Workdir) (*storeSession, error) {
	dataDir, err := storeDataDir()
	if err != nil {
		return nil, err
	}
	s, err := sqlitestore.Open(migrate.OutputPath(dataDir))
	if err != nil {
		return nil, fmt.Errorf("opening store: %w", err)
	}
	layout := projection.Layout{WorklogDir: wd.Root, DevboardDir: devboard.DataDir()}

	edited, err := projection.EditedIn(s, layout)
	if err != nil {
		s.Close()
		return nil, fmt.Errorf("checking projections: %w", err)
	}
	if len(edited) > 0 {
		s.Close()
		return nil, errWithExit(1,
			"refusing to write — these projections were edited by hand and a re-render would discard the changes:\n  %s\nreconcile them first (they are build outputs; the store is the source)",
			strings.Join(edited, "\n  "))
	}
	return &storeSession{s: s, layout: layout, wd: wd}, nil
}

func (ss *storeSession) close() { ss.s.Close() }

// commit persists t and synchronously re-renders every projection plus
// INDEX.md, so the dashboard and session-start hook see the write before
// this process exits (contract criterion 3). Unlike the task<sub> family,
// which never touches an indexed field, these verbs can change
// title/section/repo/tags, so INDEX.md is regenerated every time.
func (ss *storeSession) commit(t *store.Ticket) error {
	if err := ss.s.PutTicket(t); err != nil {
		return err
	}
	return ss.render()
}

// commitFeedback persists e and re-renders — FEEDBACK.md isn't one of
// INDEX.md's four sections (ticket/tag/repo/archive-month), so no
// reindex is needed here.
func (ss *storeSession) commitFeedback(e *store.FeedbackEntry) error {
	if err := ss.s.PutFeedback(e); err != nil {
		return err
	}
	return projection.RenderTo(ss.s, ss.layout)
}

func (ss *storeSession) render() error {
	if err := projection.RenderTo(ss.s, ss.layout); err != nil {
		return fmt.Errorf("rendering projections: %w", err)
	}
	if _, err := reindex.Run(ss.wd, reindex.Inputs{}); err != nil {
		return fmt.Errorf("regenerating INDEX.md: %w", err)
	}
	return nil
}

// ticketBySlugOrErr resolves slug to a *store.Ticket, mapping a
// not-found lookup to notFoundErr (typically a package's own
// ErrIDNotFound sentinel) so callers keep their existing error-mapping
// switch statements unchanged between the legacy and store paths.
func (ss *storeSession) ticketBySlugOrErr(slug string, notFoundErr error) (*store.Ticket, error) {
	t, err := ss.s.TicketBySlug(slug)
	if store.IsNotFound(err) {
		return nil, fmt.Errorf("%w: %q", notFoundErr, slug)
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}
