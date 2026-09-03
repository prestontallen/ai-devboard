package cli

import (
	"fmt"
	"os"
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

// ensureBoardTracked replicates the one create-if-missing devboard hook
// the legacy write surface had (devboard.OnStart — done/pr/link's hooks
// all no-op on a missing entry, matching devboard.Find's contract). A
// ticket that has never been on the board gets BoardTracked, its session
// (for the dashboard's resume button — dev-context documents this as
// automatic on start), branch, and repo path; an already-tracked ticket
// only gets its repo path self-healed, same as legacy. declaredRepo is
// the ticket's own canonical Repo, used only to resolve a filesystem
// root when RepoPath is empty or has gone away.
func ensureBoardTracked(t *store.Ticket, declaredRepo string) {
	if t.BoardTracked {
		if t.RepoPath == "" || !dirExists(t.RepoPath) {
			if root := devboard.RepoRootFor(declaredRepo); root != "" {
				t.RepoPath = root
			}
		}
		return
	}
	if !devboard.Enabled() {
		return // devboard is opt-in by dir presence; a first tracking is a no-op, not a create
	}
	t.BoardTracked = true
	if s := os.Getenv("CLAUDE_CODE_SESSION_ID"); s != "" {
		t.Session = s
	}
	if b := devboard.GitBranch(); b != "" {
		t.Branch = b
	}
	if root := devboard.RepoRootFor(declaredRepo); root != "" {
		t.RepoPath = root
	}
}

// ensureParentBoardTracked replicates legacy's devboardSyncEpic, called on
// every child start/resume/done: a child's parent epic never goes through
// its own start (epics can't occupy ## Now), so nothing else ever
// board-tracks it. Without this, an epic's own file is never created and
// children with nowhere to nest never render. No-ops for a standalone
// ticket (t.ParentID == "") or if the parent lookup fails — a dangling
// ParentID is reported by the caller's own checks, not silently patched
// here.
func ensureParentBoardTracked(ss *storeSession, t *store.Ticket) error {
	if t.ParentID == "" {
		return nil
	}
	parent, err := ss.s.Ticket(t.ParentID)
	if err != nil {
		return nil
	}
	ensureBoardTracked(parent, parent.Repo)
	return ss.s.PutTicket(parent)
}

// clearBoardTracked is untrack's store-side half. Render only ever writes
// the files it expects (every BoardTracked, non-child ticket); it never
// prunes a file that fell out of that set, so deleting a task file alone
// would leave it recreated on the next write that happens to re-render.
// No-op (not an error) when id doesn't resolve to a store ticket — a bare
// producer file (no worklog: join key) has nothing in the store to clear,
// and the caller deletes the file directly in that case.
func clearBoardTracked(wd model.Workdir, id string) error {
	ss, err := openStoreForWrite(wd)
	if err != nil {
		return err
	}
	defer ss.close()
	t, err := ss.s.TicketBySlug(id)
	if store.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	t.BoardTracked = false
	return ss.commit(t)
}

func dirExists(p string) bool {
	if p == "" {
		return false
	}
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
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
