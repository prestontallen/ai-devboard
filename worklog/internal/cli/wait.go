package cli

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/prestontallen/ai-devboard/worklog/internal/style"
	"github.com/prestontallen/ai-devboard/worklog/internal/wait"
)

func newWaitCmd() *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "wait <id>",
		Args:  cobra.ExactArgs(1),
		Short: "Park a Now ticket into ## Waiting (cap-exempt holding area)",
		Long: `wait moves a ticket from ## Now into ## Waiting, stamping
**Waiting since**: with today's date. The ## Waiting section is created
automatically if absent.

To resume a waiting ticket back to ## Now, use: worklog start <id>
To archive directly from Waiting, use: worklog done <id> --summary "..."`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWait(cmd, args[0], asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit a JSON result object")
	return cmd
}

func runWait(cmd *cobra.Command, id string, asJSON bool) error {
	wd, err := resolveWorkdir()
	if err != nil {
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}

	id = strings.ToLower(strings.TrimSpace(id))
	today := time.Now().Format("2006-01-02")

	out, err := wait.Wait(wd, id, today)
	if err != nil {
		return mapWaitError(cmd, asJSON, err)
	}

	if asJSON {
		return emitJSON(cmd.OutOrStdout(), out)
	}
	fmt.Fprintln(cmd.OutOrStdout(),
		style.Good.Render(fmt.Sprintf("parked %s into ## Waiting (since %s)",
			strings.ToUpper(out.ID), out.WaitingSince)))
	return nil
}

func mapWaitError(cmd *cobra.Command, asJSON bool, err error) error {
	switch {
	case errors.Is(err, wait.ErrIDNotFound),
		errors.Is(err, wait.ErrNotInNow),
		errors.Is(err, wait.ErrAlreadyWaiting):
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	default:
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}
}
