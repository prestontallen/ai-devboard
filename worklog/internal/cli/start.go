package cli

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/prestontallen/ai-devboard/worklog/internal/devboard"
	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/parse"
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

	// Fast-path: if the ticket is in ## Waiting, resume it instead.
	// TODO: unify parse in start.Run to avoid the double-parse in this path.
	normID := strings.ToLower(strings.TrimSpace(id))
	if doc, parseErr := parse.File(wd.WorkMD()); parseErr == nil {
		if b := doc.BlockByID(normID); b != nil && b.Section == model.SectionWaiting {
			today := time.Now().Format("2006-01-02")
			out, err := wait.Resume(wd, normID, today)
			if err != nil {
				return mapResumeError(cmd, asJSON, err)
			}
			if b.Parent != "" {
				devboardSyncEpic(wd, b.Parent)
			} else {
				// Resume path: the block is already in hand, no extra parse.
				devboardOnStart(out.ID, "", string(b.Type))
			}
			if asJSON {
				return emitJSON(cmd.OutOrStdout(), out)
			}
			fmt.Fprintln(cmd.OutOrStdout(),
				style.Good.Render(fmt.Sprintf("resumed %s into ## Now",
					strings.ToUpper(out.ID))))
			return nil
		}
	}

	inputs := start.Inputs{
		ID:         strings.ToLower(strings.TrimSpace(id)),
		Repo:       strings.TrimSpace(flagRepo),
		Tags:       splitTags(flagTagsCSV),
		Acceptance: strings.TrimSpace(flagAcceptance),
	}
	today := time.Now().Format("2006-01-02")

	out, err := start.Run(wd, inputs, today)
	if err != nil {
		return mapStartError(cmd, asJSON, err)
	}
	// Checked before the write, since the write is what creates the group.
	if group := devboard.PendingNewGroup(); group != "" {
		out.Warnings = append(out.Warnings, newDevboardGroupWarning(group))
	}
	if out.Parent != "" {
		devboardSyncEpic(wd, out.Parent)
	} else {
		devboardOnStart(out.ID, out.Title, out.Type)
	}

	if asJSON {
		return emitJSON(cmd.OutOrStdout(), out)
	}
	emitStartText(cmd, out)
	return nil
}

func mapResumeError(cmd *cobra.Command, asJSON bool, err error) error {
	switch {
	case errors.Is(err, wait.ErrIDNotFound),
		errors.Is(err, wait.ErrNotInWaiting),
		errors.Is(err, wait.ErrCapExceeded):
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	default:
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}
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

// newDevboardGroupWarning describes a devboard repo group being created for
// the first time. Worth saying out loud: a group named after something that
// isn't the repo means task files are being filed where the dashboard won't
// look for them.
func newDevboardGroupWarning(group string) string {
	return "devboard: creating a new repo group \"" + group +
		"\"; if that is not this repository's name, task files are being filed in the wrong place"
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
