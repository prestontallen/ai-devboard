package cli

import (
	"github.com/spf13/cobra"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/serve"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

// newServeCmd wires the devboard dashboard server. Configuration is
// env-driven (DEVBOARD_DATA, DEVBOARD_WORKLOG, DEVBOARD_PORT,
// DEVBOARD_SCAN_INTERVAL), matching the retired Python server, with native
// defaults replacing the container paths.
func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Args:  cobra.NoArgs,
		Short: "Serve the devboard dashboard (frontend + API, port 8484)",
		Long: `serve runs the devboard dashboard: the embedded frontend, the
/api/tasks feed grouped by repo directory, an SSE change stream, and the
archive/unarchive endpoints. It replaces devboard/server.py; the response
shape is frozen as the frontend contract (devboard/API.md).

Environment: DEVBOARD_DATA (default ~/.local/share/devboard),
DEVBOARD_WORKLOG (default ~/.local/share/worklog), DEVBOARD_PORT (8484),
DEVBOARD_SCAN_INTERVAL (seconds, 1.0). Binds 0.0.0.0 — the board is used
over LAN.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := serve.ConfigFromEnv()
			srv := serve.New(cfg)
			// Sync the store after a dashboard archive/unarchive move,
			// injected here rather than imported by internal/serve
			// directly (internal/verify already imports serve for board
			// comparison, so that import would cycle).
			srv.MutateBoard = func(repo, id string, archived bool) (bool, error) {
				wd, err := model.NewWorkdir(cfg.WorklogDir)
				if err != nil {
					return false, err
				}
				return storeArchiveMove(wd, id, archived)
			}
			return srv.ListenAndServe()
		},
	}
}

// storeArchiveMove records a dashboard archive/unarchive in the store — the
// same PutTicket+render every other store-backed write goes through, just
// triggered from the HTTP handler instead of a CLI verb. The render is what
// actually moves the file: BoardArchived decides which path the board task
// renders to, and RenderTo clears the one it left.
//
// It reports whether the store owned the move. Two shapes it does not: an id
// with no ticket at all, and a ticket that exists but is not board-tracked —
// the second is the subtler one, because the ticket resolves and setting a
// field on it would look like success while the projection never writes that
// file and nothing moves. Both are hand-dropped producer files as far as the
// board is concerned, and the handler renames those itself.
func storeArchiveMove(wd model.Workdir, id string, archived bool) (bool, error) {
	ss, err := openStoreForWrite(wd)
	if err != nil {
		return false, err
	}
	defer ss.close()

	t, err := ss.s.TicketBySlug(id)
	if store.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !t.BoardTracked {
		return false, nil
	}
	t.BoardArchived = archived
	if err := ss.commit(t); err != nil {
		return false, err
	}
	return true, nil
}
