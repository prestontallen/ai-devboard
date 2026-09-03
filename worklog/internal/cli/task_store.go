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

// storeWriteEnv gates the store-backed write path (adb-cutover M3c/M3d).
// Defaults on as of the M3d cutover flip: every write verb is
// store-backed unless explicitly overridden with WORKLOG_STORE_WRITE=0,
// an emergency rollback lever that doesn't require swapping the binary.
// The flip is atomic across every verb that shares an entity on purpose
// — a ported `task` reading the store while `add` still only wrote
// markdown would make a just-created ticket invisible.
const storeWriteEnv = "WORKLOG_STORE_WRITE"

func storeWriteEnabled() bool { return os.Getenv(storeWriteEnv) != "0" }

// storeMutateTaskOrChild is the store-backed twin of mutateTaskOrChild:
// same dispatch, same closures, different system of record. The ticket is
// read from the store, projected into the *devboard.Task shape the eleven
// subcommand closures already expect, mutated by the closure untouched,
// applied back, and committed — after which the affected projections are
// re-rendered to disk so the dashboard and the session-start hook keep
// reading live files.
//
// The epic/child scratch-view machinery has no counterpart here: a child
// is its own ticket row carrying ParentID, so it resolves by slug like
// any other ticket and its in-flight detail is simply its own.
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

	target, worklogID, err := resolveStoreTarget(s, id, child)
	if err != nil {
		return "", "", err
	}

	task := projection.BoardTask(target, nil)
	if err := fn(task); err != nil {
		return "", "", err
	}
	projection.ApplyBoardTask(target, task)
	// A task subcommand is what puts a ticket on the board in the first
	// place, matching the legacy path's create-on-first-use behavior.
	target.BoardTracked = true

	if err := s.PutTicket(target); err != nil {
		return "", "", fmt.Errorf("task: %w", err)
	}
	if err := projection.RenderTo(s, layout); err != nil {
		return "", "", fmt.Errorf("task: rendering projections: %w", err)
	}
	// Repo attribution heals here rather than following cwd, so the
	// misfiled-group guard and its --force escape hatch have nothing left
	// to guard (adb-cutover M4 heals the 29 existing strays).
	repo := target.Repo
	if repo == "" {
		repo = "unknown"
	}
	return filepath.Join(devboard.DataDir(), repo, target.Slug+".yaml"), worklogID, nil
}

// resolveStoreTarget applies the same --id/--child rules mutateTaskOrChild
// enforces, against the store instead of a file path.
func resolveStoreTarget(s store.Store, id, child string) (*store.Ticket, string, error) {
	if id == "" {
		return nil, "", errWithExit(64, "task: --id is required")
	}
	t, err := s.TicketBySlug(id)
	if store.IsNotFound(err) {
		return nil, "", errWithExit(1, "task: no ticket found for id %q", id)
	}
	if err != nil {
		return nil, "", err
	}

	if t.Type != store.TypeEpic {
		if child != "" {
			return nil, "", errWithExit(64, "task: --child is only valid when --id names an epic")
		}
		return t, t.Slug, nil
	}

	kids, err := s.Children(t.ID)
	if err != nil {
		return nil, "", err
	}
	if child == "" {
		return nil, "", errWithExit(64,
			"task: --id %q is an epic; pass --child <id> (children: %s)",
			t.Slug, strings.Join(storeChildIDs(kids), ", "))
	}
	for _, k := range kids {
		if strings.EqualFold(k.Slug, child) {
			return k, k.Slug, nil
		}
	}
	// Matches findOrAppendChild: naming a not-yet-rostered child creates a
	// pending one rather than refusing.
	return &store.Ticket{
		Slug: store.NormalizeSlug(child), Type: store.TypeTicket,
		State: store.StatePending, ParentID: t.ID, Repo: t.Repo,
	}, child, nil
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
