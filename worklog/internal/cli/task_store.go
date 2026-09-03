package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/prestontallen/ai-devboard/worklog/internal/devboard"
	"github.com/prestontallen/ai-devboard/worklog/internal/migrate"
	"github.com/prestontallen/ai-devboard/worklog/internal/projection"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/sqlitestore"
)

// storeMutateTaskOrChild is what every task<sub> subcommand's mutation
// runs against (adb-cutover M4: the legacy YAML-splice dispatch,
// mutateTaskOrChild, is retired). The ticket is read from the store,
// projected into the *devboard.Task shape the eleven subcommand closures
// already expect, mutated by the closure untouched, applied back, and
// committed — after which the affected projections are re-rendered to
// disk so the dashboard and the session-start hook keep reading live
// files.
//
// A child is its own ticket row carrying ParentID, so it resolves by
// slug like any other ticket and its in-flight detail is simply its own
// — no separate scratch-view machinery needed for the epic/child split.
// Returns the absolute path of the board file the render produced, so
// mutateTask's warn hooks and result reporting work unchanged. Because
// write-through renders synchronously before returning, a hook that
// re-reads that file sees the mutation rather than a stale copy — the
// staleness the design panel flagged on the legacy path.
func storeMutateTaskOrChild(id, child string, fn func(*devboard.Task) error) (path, worklogID string, err error) {
	wd, err := resolveWorkdir()
	if err != nil {
		return "", "", err
	}
	dataDir, err := storeDataDir()
	if err != nil {
		return "", "", err
	}
	s, err := sqlitestore.Open(migrate.OutputPath(dataDir))
	if err != nil {
		return "", "", fmt.Errorf("task: opening store: %w", err)
	}
	defer s.Close()

	layout := projection.Layout{WorklogDir: wd.Root, DevboardDir: devboard.DataDir()}

	// Refuse before mutating, never after: once the render runs, whatever
	// someone typed into a projection by hand is gone (adb-cutover M3b).
	edited, err := projection.EditedIn(s, layout)
	if err != nil {
		return "", "", fmt.Errorf("task: checking projections: %w", err)
	}
	if len(edited) > 0 {
		return "", "", errWithExit(1,
			"task: refusing to write — these projections were edited by hand and a re-render would discard the changes:\n  %s\nreconcile them first (they are build outputs; the store is the source)",
			strings.Join(edited, "\n  "))
	}

	target, worklogID, top, err := resolveStoreTarget(s, id, child)
	if err != nil {
		return "", "", err
	}

	task := projection.BoardTask(target, nil)
	if err := fn(task); err != nil {
		return "", "", err
	}
	projection.ApplyBoardTask(target, task)
	// A task subcommand is what puts a ticket on the board in the first
	// place, matching the legacy path's create-on-first-use behavior. A
	// child never gets a file of its own, so it's top — itself for a
	// plain ticket, the parent epic for a child — that actually needs
	// board-tracking for anything to render at all.
	top.BoardTracked = true

	if err := s.PutTicket(target); err != nil {
		return "", "", fmt.Errorf("task: %w", err)
	}
	if top.ID != target.ID {
		if err := s.PutTicket(top); err != nil {
			return "", "", fmt.Errorf("task: %w", err)
		}
	}
	if err := projection.RenderTo(s, layout); err != nil {
		return "", "", fmt.Errorf("task: rendering projections: %w", err)
	}
	// Repo attribution heals here rather than following cwd, so the
	// misfiled-group guard and its --force escape hatch have nothing left
	// to guard (adb-cutover M4 heals the 29 existing strays). Always
	// keyed off top: a child's mutation still lands in the epic's file.
	repo := top.Repo
	if repo == "" {
		repo = "unknown"
	}
	return filepath.Join(devboard.DataDir(), repo, top.Slug+".yaml"), worklogID, nil
}

// resolveStoreTarget applies the same --id/--child rules mutateTaskOrChild
// enforces, against the store instead of a file path. top is the ticket
// whose devboard file the mutation ultimately lands in — itself for a
// plain ticket, or the parent epic when child is set, since a child never
// gets a file of its own (it nests inside the epic's). target is what fn
// actually mutates: top itself, or the named child.
func resolveStoreTarget(s store.Store, id, child string) (target *store.Ticket, worklogID string, top *store.Ticket, err error) {
	if id == "" {
		return nil, "", nil, errWithExit(64, "task: --id is required")
	}
	t, err := s.TicketBySlug(id)
	if store.IsNotFound(err) {
		return nil, "", nil, errWithExit(1, "task: no ticket found for id %q", id)
	}
	if err != nil {
		return nil, "", nil, err
	}

	if t.Type != store.TypeEpic {
		if child != "" {
			return nil, "", nil, errWithExit(64, "task: --child is only valid when --id names an epic")
		}
		if t.ParentID != "" {
			if parent, perr := s.Ticket(t.ParentID); perr == nil {
				return nil, "", nil, errWithExit(64,
					"task: %q is a child of epic %q; pass --id %s --child %s instead of creating a standalone file",
					t.Slug, parent.Slug, parent.Slug, t.Slug)
			}
		}
		return t, t.Slug, t, nil
	}

	kids, err := s.Children(t.ID)
	if err != nil {
		return nil, "", nil, err
	}
	if child == "" {
		return nil, "", nil, errWithExit(64,
			"task: --id %q is an epic; pass --child <id> (children: %s)",
			t.Slug, strings.Join(storeChildIDs(kids), ", "))
	}
	for _, k := range kids {
		if strings.EqualFold(k.Slug, child) {
			return k, k.Slug, t, nil
		}
	}
	// Matches findOrAppendChild: naming a not-yet-rostered child creates a
	// pending one rather than refusing.
	return &store.Ticket{
		Slug: store.NormalizeSlug(child), Type: store.TypeTicket,
		State: store.StatePending, ParentID: t.ID, Repo: t.Repo,
	}, child, t, nil
}

func storeChildIDs(kids []*store.Ticket) []string {
	if len(kids) == 0 {
		return []string{"(none yet — the epic has no started children)"}
	}
	out := make([]string, len(kids))
	for i, k := range kids {
		out[i] = k.Slug
	}
	return out
}

func storeDataDir() (string, error) {
	if env := os.Getenv("WORKLOG_MIGRATION_DATA"); env != "" {
		return env, nil
	}
	return migrate.DefaultDataDir()
}
