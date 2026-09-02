package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/prestontallen/ai-devboard/worklog/internal/reindex"
	"github.com/prestontallen/ai-devboard/worklog/internal/style"
)

func newReindexCmd() *cobra.Command {
	var (
		flagJSON   bool
		flagDryRun bool
	)
	cmd := &cobra.Command{
		Use:   "reindex",
		Args:  cobra.NoArgs,
		Short: "Rebuild INDEX.md from WORK.md + archive/ + notes/",
		Long: `reindex scans the worklog data directory and rebuilds INDEX.md from
scratch — destructive: any manual content in INDEX.md is replaced. Run
periodically (e.g. once per session) or after a batch of mutations to clear
the "INDEX.md not updated" warnings that add/start/done emit.

The output has four sections:
  - By ticket (alphabetical by ID)
  - By tag    (alphabetical by tag; IDs sorted within each)
  - By repo   (alphabetical by repo; IDs sorted within each)
  - By archive month (reverse-chronological)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReindex(cmd, flagDryRun, flagJSON)
		},
	}
	cmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "print would-be INDEX.md content to stdout; do not write")
	cmd.Flags().BoolVar(&flagJSON, "json", false, "emit a structured JSON result instead of styled text")
	return cmd
}

func runReindex(cmd *cobra.Command, dryRun, asJSON bool) error {
	wd, err := resolveWorkdir()
	if err != nil {
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}

	out, err := reindex.Run(wd, reindex.Inputs{DryRun: dryRun})
	if err != nil {
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}

	if asJSON {
		return emitJSON(cmd.OutOrStdout(), out)
	}

	w := cmd.OutOrStdout()
	if dryRun {
		// Print the proposed content to stdout, then a styled status line on stderr.
		fmt.Fprint(w, out.Content)
		fmt.Fprintln(cmd.ErrOrStderr(),
			style.Dim.Render(fmt.Sprintf(
				"would regenerate %s (tickets=%d, tags=%d, repos=%d, months=%d)",
				out.IndexPath, out.Entries.ByTicket, out.Entries.ByTag,
				out.Entries.ByRepo, out.Entries.ByArchiveMonth)))
		return nil
	}

	fmt.Fprintln(w, style.Good.Render(
		fmt.Sprintf("regenerated %s", out.IndexPath)))
	fmt.Fprintln(w, style.Dim.Render(fmt.Sprintf(
		"  tickets=%d, tags=%d, repos=%d, months=%d",
		out.Entries.ByTicket, out.Entries.ByTag, out.Entries.ByRepo, out.Entries.ByArchiveMonth)))
	return nil
}
