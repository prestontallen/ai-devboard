package cli

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/prestontallen/ai-devboard/worklog/internal/start"
	"github.com/prestontallen/ai-devboard/worklog/internal/style"
	"github.com/prestontallen/ai-devboard/worklog/internal/wait"
)

func newStartCmd() *cobra.Command {
	var (
		flagRepo       string
		flagTagsCSV    string
		flagAcceptance string
		flagJSON       bool
	)
	cmd := &cobra.Command{
		Use:   "start <id>",
		Args:  cobra.ExactArgs(1),
		Short: "Promote a ticket to ## Now (cap-checked, start-dated)",
		Long: `start moves a standalone ticket from ## Next or ## Someday into
## Now, or promotes a child of an epic from its notes file. In every case the
state flips to [~], today's date is stamped, and (for child promotions) the
parent epic's **Active children**: field is updated.

Cap-controlled: ## Now is capped at 5 tickets. start refuses with exit 1 if
the cap would be exceeded.

Use --repo, --tags, --acceptance when promoting a child of an epic to
populate the newly-created ticket block. For standalone moves these flags
override the source block's existing metadata.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStart(cmd, args[0],
				flagRepo, flagTagsCSV, flagAcceptance, flagJSON)
		},
	}
	cmd.Flags().StringVar(&flagRepo, "repo", "", "repository name (mostly for the child-of-epic path)")
	cmd.Flags().StringVar(&flagTagsCSV, "tags", "", "comma-separated tags")
	cmd.Flags().StringVar(&flagAcceptance, "acceptance", "", "one-line acceptance criterion")
	cmd.Flags().BoolVar(&flagJSON, "json", false, "emit a JSON result object instead of styled text")
	return cmd
}

func runStart(cmd *cobra.Command, id, flagRepo, flagTagsCSV, flagAcceptance string, asJSON bool) error {
	wd, err := resolveWorkdir()
	if err != nil {
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}

	today := time.Now().Format("2006-01-02")
	out, err := runStoreStart(wd, id, flagRepo, flagTagsCSV, flagAcceptance, today)
	if err != nil {
		return mapStartError(cmd, asJSON, err)
	}
	if asJSON {
		return emitJSON(cmd.OutOrStdout(), out)
	}
	switch v := out.(type) {
	case wait.ResumeOutput:
		fmt.Fprintln(cmd.OutOrStdout(),
			style.Good.Render(fmt.Sprintf("resumed %s into ## Now", strings.ToUpper(v.ID))))
	case start.Output:
		emitStartText(cmd, v)
	}
	return nil
}

func mapStartError(cmd *cobra.Command, asJSON bool, err error) error {
	switch {
	case errors.Is(err, start.ErrIDNotFound),
		errors.Is(err, start.ErrAlreadyStarted),
		errors.Is(err, start.ErrCapExceeded),
		errors.Is(err, start.ErrEpicCannotStart):
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	default:
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}
}

func emitStartText(cmd *cobra.Command, out start.Output) {
	w := cmd.OutOrStdout()
	headline := fmt.Sprintf("started %s in ## %s (Started: %s)",
		strings.ToUpper(out.ID), out.Section, out.Started)
	if out.Parent != "" {
		headline += fmt.Sprintf(" — parent: %s", out.Parent)
	}
	fmt.Fprintln(w, style.Good.Render(headline))
	for _, warning := range out.Warnings {
		line := style.Warn.Render("NOTE: " + warning)
		// The validate hint only makes sense for the INDEX warning; it is no
		// help at all for a devboard grouping notice.
		if warning == start.IndexNotUpdatedWarning {
			line += " Run " + style.SubHeading.Render("worklog validate") +
				" to check the rest of the worklog."
		}
		fmt.Fprintln(w, line)
	}
}
