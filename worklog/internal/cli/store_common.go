package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/prestontallen/ai-devboard/worklog/internal/devboard"
	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/projection"
	"github.com/prestontallen/ai-devboard/worklog/internal/reindex"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
	"github.com/prestontallen/ai-devboard/worklog/internal/storepath"
)

// nextRank is the Rank a newly created ticket must carry: one past the
// highest rank among live tickets, so it renders at the END of its
// section.
//
// Leaving it at the zero value is not "unranked" — it is the literal
// rank of whichever ticket sits first in WORK.md, because convert
// numbers document position from zero (convert/workmd.go:73). A new
// ticket defaulting to 0 therefore *ties* the first ticket, and
// Tickets() breaks that tie with `ORDER BY rank, slug` — so the new
// ticket silently sorts by alphabetical luck instead of landing where
// the human put it. Observed live: a ticket added on 2026-09-03 tied
// adb-research-mode at rank 0 and won the slug comparison, jumping to
// the top of ## Now.
//
// Archived tickets are excluded: their Rank is a position within an
// archive month, a different numbering space that says nothing about
// where a live ticket belongs.
func nextRank(s store.Store) (int, error) {
	tickets, err := s.Tickets()
	if err != nil {
		return 0, err
	}
	max := -1
	for _, t := range tickets {
		if !t.Archived && t.Rank > max {
			max = t.Rank
		}
	}
	return max + 1, nil
}

// nextRosterRank is nextRank for an epic's child roster. Same collision,
// one level down: RosterRank is assigned only by convert (convert.go:161,
// :181), so a child added through the CLI defaults to 0, ties the epic's
// first child, and is ordered against it by slug.
func nextRosterRank(s store.Store, parent store.ID) (int, error) {
	kids, err := s.Children(parent)
	if err != nil {
		return 0, err
	}
	return nextRosterRankOf(kids), nil
}

// nextRosterRankOf is nextRosterRank for a caller that already holds the
// roster, so the only copy of the "one past the highest" rule lives here.
func nextRosterRankOf(kids []*store.Ticket) int {
	max := -1
	for _, k := range kids {
		if k.RosterRank > max {
			max = k.RosterRank
		}
	}
	return max + 1
}

// refuseRetiredStoreEnv refuses while $WORKLOG_MIGRATION_DATA is still
// set. Ignoring it silently would be worse than failing: somebody who
// exported it meant to send the store somewhere specific, and quietly
// writing elsewhere is how a machine ends up with two databases.
func refuseRetiredStoreEnv() error {
	if os.Getenv(storepath.LegacyEnv) == "" {
		return nil
	}
	return errWithExit(1,
		"$%s is retired — the store directory is now derived from the worklog directory\n"+
			"unset it; use --dir or $WORKLOG_DIR to move both together",
		storepath.LegacyEnv)
}

// isDefaultCorpus reports whether wd is the corpus a bare `worklog` with
// no --dir and no $WORKLOG_DIR resolves to.
func isDefaultCorpus(wd model.Workdir) bool {
	def, err := model.NewWorkdir("")
	return err == nil && def.Root == wd.Root
}

// requireStore resolves wd's database and refuses when it is not there.
//
// Two absences that need different answers. A machine that never adopted
// has no store anywhere, and the fix is to build one. A machine whose
// database is still at the pre-move location has a perfectly good store
// in the wrong place, and telling THAT machine to run adopt is actively
// destructive: adopt rebuilds the store from markdown, so every field the
// store owns and the markdown does not — phase history, scorecards,
// decisions, the board's in-flight detail — would be silently dropped.
// Look before answering.
//
// Refusing also matters because sqlitestore.Open CREATES the database
// when the path is absent. Without this the verb would proceed against an
// empty store and render a WORK.md the corpus does not match.
func requireStore(wd model.Workdir) (string, error) {
	if err := refuseRetiredStoreEnv(); err != nil {
		return "", err
	}

	path := storepath.DB(wd.Root)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("opening store: %w", err)
	}

	// Only the DEFAULT corpus can have a database at the pre-move default
	// location. Pointing a scratch or secondary corpus at that message
	// would be nonsense — its store was never there — and in tests it
	// would swap the honest "not adopted" answer for a relocation notice
	// about the developer's real database.
	if isDefaultCorpus(wd) {
		if legacy, err := storepath.LegacyDB(); err == nil {
			if _, statErr := os.Stat(legacy); statErr == nil {
				return "", errWithExit(1,
					"the store has a new home and yours is still at the old one\n  old: %s\n  new: %s\nrun `worklog store relocate` to move it — do NOT run `worklog adopt`, which would rebuild the store from markdown and drop everything only the store holds",
					legacy, path)
			}
		}
	}

	return "", errWithExit(1,
		"this machine has not adopted the store yet — no database at %s\nrun `worklog adopt` to preview the adoption, then `worklog adopt --commit`",
		path)
}

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

// openStoreForWrite opens the store and reports, without refusing, any
// projection under wd/devboard that has been hand-edited since the last
// render.
func openStoreForWrite(wd model.Workdir) (*storeSession, error) {
	path, err := requireStore(wd)
	if err != nil {
		return nil, err
	}
	s, _, err := openStore(storeExisting, path)
	if err != nil {
		return nil, fmt.Errorf("opening store: %w", err)
	}
	layout := projection.Layout{WorklogDir: wd.Root}

	warnHandEdits(s, layout)
	return &storeSession{s: s, layout: layout, wd: wd}, nil
}

// warnHandEdits names any projection whose bytes on disk differ from what
// the store would render, then lets the write proceed.
//
// This used to refuse. It no longer does, because refusing treats the file
// as a second source of truth to reconcile against, and the whole point of
// this design is that markdown is render OUTPUT — the store is the source,
// and a re-render is supposed to win.
//
// The warning stays because the overwrite is real and silent otherwise:
// notes files carry human-authored prose, so a hand edit there is genuinely
// lost, and the human deserves to be told which file it was rather than
// discovering it later. Detection has to happen HERE, before the mutation:
// at render time every legitimate write also differs, so there is no signal
// left to distinguish a hand edit from ordinary work.
//
// Never fatal. A failure to check is not a reason to block the user's
// command, and the check itself is advisory now.
func warnHandEdits(s store.Store, layout projection.Layout) {
	edited, err := projection.EditedIn(s, layout)
	if err != nil {
		return
	}
	if len(edited) == 0 {
		return
	}

	// Cap the list. A warning nobody reads is the same as no warning, and
	// the failure mode here is a wall of paths scrolling a real one away.
	const show = 8
	shown := edited
	suffix := ""
	if len(shown) > show {
		shown = shown[:show]
		suffix = fmt.Sprintf("\n  ... and %d more", len(edited)-show)
	}
	fmt.Fprintf(os.Stderr,
		"warning: overwriting %d hand-edited file(s) — the store is the source and this render wins:\n  %s%s\n",
		len(edited), strings.Join(shown, "\n  "), suffix)
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
// all no-op on a missing entry). A
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
	// No opt-in gate. It used to be os.Stat of the devboard data dir, from
	// the era when worklog wrote those YAML files itself and must not
	// create a directory nobody asked for. Every write verb is store-backed
	// now and board-tracking is one column in a database this command
	// already opened, so there is nothing to opt into
	// (adb-retire-devboard-dir-2).
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
