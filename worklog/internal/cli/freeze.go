package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/prestontallen/ai-devboard/worklog/internal/freeze"
	"github.com/prestontallen/ai-devboard/worklog/internal/style"
)

// jsonFreezeStatus is the --json shape for freeze status/acquire/release.
type jsonFreezeStatus struct {
	Frozen   bool   `json:"frozen"`
	PID      int    `json:"pid,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Acquired string `json:"acquired,omitempty"`
}

func newFreezeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "freeze",
		Args:  cobra.NoArgs,
		Short: "Report, acquire, or release the code-enforced write freeze",
		Long: `freeze guards the worklog cutover window. While a freeze is held, every
write verb (add/start/done/edit/pr/link/note/reindex/feedback/import/wait/
task) refuses immediately. Read-only commands (validate, status, standup,
tui, search, summarize, verify, migrate, install, hook, serve, freeze
itself) still run.

The freeze is a sentinel file, not a lock held by a running process — it
outlives the command that creates it, so it stays in effect across every
worklog invocation (including from other sessions) until explicitly
released.

Bare "worklog freeze" is an alias for "worklog freeze status".`,
	}
	var asJSON bool
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runFreezeStatus(cmd, asJSON)
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit a JSON result object instead of styled text")

	cmd.AddCommand(newFreezeAcquireCmd(), newFreezeReleaseCmd(), newFreezeStatusCmd())
	return cmd
}

func newFreezeAcquireCmd() *cobra.Command {
	var (
		reason string
		asJSON bool
	)
	cmd := &cobra.Command{
		Use:   "acquire",
		Args:  cobra.NoArgs,
		Short: "Acquire the write freeze",
		RunE: func(cmd *cobra.Command, args []string) error {
			if reason == "" {
				return jsonOrTextError(cmd, asJSON, 1, "--reason is required (name why the freeze is being held)")
			}
			wd, err := resolveWorkdir()
			if err != nil {
				return jsonOrTextError(cmd, asJSON, 1, "%v", err)
			}
			info, err := freeze.Acquire(wd.Root, reason)
			if err != nil {
				return jsonOrTextError(cmd, asJSON, 1, "%v", err)
			}
			if asJSON {
				return emitJSON(cmd.OutOrStdout(), jsonFreezeStatus{
					Frozen: true, PID: info.PID, Reason: info.Reason,
					Acquired: info.Acquired.Format(time.RFC3339),
				})
			}
			fmt.Fprintln(cmd.OutOrStdout(), style.Good.Render(
				fmt.Sprintf("frozen: %s (pid %d)", info.Reason, info.PID)))
			return nil
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "why the freeze is being held (required)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit a JSON result object instead of styled text")
	return cmd
}

func newFreezeReleaseCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "release",
		Args:  cobra.NoArgs,
		Short: "Release the write freeze",
		RunE: func(cmd *cobra.Command, args []string) error {
			wd, err := resolveWorkdir()
			if err != nil {
				return jsonOrTextError(cmd, asJSON, 1, "%v", err)
			}
			if err := freeze.Release(wd.Root); err != nil {
				return jsonOrTextError(cmd, asJSON, 1, "%v", err)
			}
			if asJSON {
				return emitJSON(cmd.OutOrStdout(), jsonFreezeStatus{Frozen: false})
			}
			fmt.Fprintln(cmd.OutOrStdout(), style.Good.Render("released"))
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit a JSON result object instead of styled text")
	return cmd
}

func newFreezeStatusCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "status",
		Args:  cobra.NoArgs,
		Short: "Show whether the write freeze is held",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFreezeStatus(cmd, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit a JSON result object instead of styled text")
	return cmd
}

func runFreezeStatus(cmd *cobra.Command, asJSON bool) error {
	wd, err := resolveWorkdir()
	if err != nil {
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}
	frozen, info, err := freeze.Check(wd.Root)
	if err != nil {
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}
	if asJSON {
		out := jsonFreezeStatus{Frozen: frozen}
		if frozen {
			out.PID, out.Reason = info.PID, info.Reason
			if !info.Acquired.IsZero() {
				out.Acquired = info.Acquired.Format(time.RFC3339)
			}
		}
		return emitJSON(cmd.OutOrStdout(), out)
	}
	if !frozen {
		fmt.Fprintln(cmd.OutOrStdout(), style.Dim.Render("not frozen"))
		return nil
	}
	fmt.Fprintln(cmd.OutOrStdout(), style.Warn.Render(
		fmt.Sprintf("frozen: %s", freeze.RefusalError(info).Error())))
	return nil
}
