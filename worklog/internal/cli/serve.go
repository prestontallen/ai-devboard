package cli

import (
	"github.com/spf13/cobra"

	"github.com/prestontallen/ai-devboard/worklog/internal/serve"
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
			return serve.New(serve.ConfigFromEnv()).ListenAndServe()
		},
	}
}
