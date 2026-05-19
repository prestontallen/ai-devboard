package cli

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/prestontallen/day2day/internal/done"
	"github.com/prestontallen/day2day/internal/style"
)

func newDoneCmd() *cobra.Command {
	var (
		flagSummary   string
		flagFeedback  []string
		flagTime      string
		flagPR        string
		flagCompleted string
		flagJSON      bool
	)
	cmd := &cobra.Command{
		Use:   "done <id>",
		Args:  cobra.ExactArgs(1),
		Short: "Archive a completed ticket (form by default; flag-driven for agents)",
		Long: `done writes an archive entry for <id>, removes the ticket from
WORK.md, and (when the ticket was a child of an epic) flips the child's
notes-file checkbox to [x] and removes it from the parent's Active children.

Without flags and under a TTY: opens a Huh form to collect Summary and
Feedback.
With --summary provided: skips the form and executes immediately.
Without a TTY and without --summary: fails fast with exit 64.

JSON output includes 'epicCompletable: true' when the just-archived ticket
was the last open child of its epic — the agent can then decide whether to
follow up with an epic archive.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDone(cmd, args[0],
				flagSummary, flagFeedback, flagTime, flagPR, flagCompleted, flagJSON)
		},
	}
	cmd.Flags().StringVar(&flagSummary, "summary", "", "one-or-two-sentence outcome (required for non-interactive)")
	cmd.Flags().StringArrayVar(&flagFeedback, "feedback", nil, "bullet for the Feedback / Notes section (repeatable)")
	cmd.Flags().StringVar(&flagTime, "time", "", "free-form effort estimate (e.g. ~3h)")
	cmd.Flags().StringVar(&flagPR, "pr", "", "PR URL (overrides any existing **PR**: field on the ticket)")
	cmd.Flags().StringVar(&flagCompleted, "completed", "", "completion date YYYY-MM-DD (defaults to today)")
	cmd.Flags().BoolVar(&flagJSON, "json", false, "emit a JSON result object instead of styled text")
	return cmd
}

func runDone(
	cmd *cobra.Command,
	id, flagSummary string,
	flagFeedback []string,
	flagTime, flagPR, flagCompleted string,
	asJSON bool,
) error {
	wd, err := resolveWorkdir()
	if err != nil {
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}

	in := done.Inputs{
		ID:        strings.ToLower(strings.TrimSpace(id)),
		Summary:   strings.TrimSpace(flagSummary),
		Feedback:  trimAll(flagFeedback),
		Time:      strings.TrimSpace(flagTime),
		PR:        strings.TrimSpace(flagPR),
		Completed: strings.TrimSpace(flagCompleted),
	}

	// Form fallback when summary missing.
	if in.Summary == "" {
		if !stdinIsTTY() {
			return jsonOrTextError(cmd, asJSON, 64,
				"done requires --summary when stdin is not a TTY")
		}
		if err := promptDoneForm(&in); err != nil {
			return jsonOrTextError(cmd, asJSON, 1, "%v", err)
		}
	}

	today := time.Now().Format("2006-01-02")
	out, err := done.Run(wd, in, today)
	if err != nil {
		return mapDoneError(cmd, asJSON, err)
	}

	if asJSON {
		return emitJSON(cmd.OutOrStdout(), out)
	}
	emitDoneText(cmd, out)
	return nil
}

func mapDoneError(cmd *cobra.Command, asJSON bool, err error) error {
	switch {
	case errors.Is(err, done.ErrSummaryRequired):
		return jsonOrTextError(cmd, asJSON, 64, "%v", err)
	case errors.Is(err, done.ErrIDNotFound),
		errors.Is(err, done.ErrCannotDoneEpic),
		errors.Is(err, done.ErrInvalidDate):
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	default:
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}
}

func promptDoneForm(in *done.Inputs) error {
	// Multi-line feedback is collected as a single textarea and split on
	// newlines after the form completes.
	feedbackTemp := strings.Join(in.Feedback, "\n")

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Summary").
				Description("One or two sentences describing the outcome.").
				Value(&in.Summary).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("summary is required")
					}
					return nil
				}),
			huh.NewText().
				Title("Feedback / Notes (optional)").
				Description("One bullet per line. Reviewer comments, gotchas, follow-ups.").
				Value(&feedbackTemp),
			huh.NewInput().
				Title("Time (optional, e.g. ~3h)").
				Value(&in.Time),
		),
	)
	if err := form.Run(); err != nil {
		return err
	}

	in.Summary = strings.TrimSpace(in.Summary)
	in.Time = strings.TrimSpace(in.Time)
	in.Feedback = splitFeedback(feedbackTemp)
	return nil
}

func splitFeedback(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, "\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func trimAll(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func emitDoneText(cmd *cobra.Command, out done.Output) {
	w := cmd.OutOrStdout()
	headline := fmt.Sprintf("archived %s on %s", strings.ToUpper(out.ID), out.Completed)
	if out.Parent != "" {
		headline += fmt.Sprintf(" (parent: %s)", out.Parent)
	}
	fmt.Fprintln(w, style.Good.Render(headline))

	if out.EpicCompletable {
		fmt.Fprintln(w,
			style.SubHeading.Render("note:")+
				" epic "+strings.ToUpper(out.Parent)+
				" now has no open children — consider archiving it next.")
	}

	for _, warning := range out.Warnings {
		fmt.Fprintln(w,
			style.Warn.Render("NOTE: "+warning)+
				" Run "+style.SubHeading.Render("worklog validate")+
				" to check the rest of the worklog.")
	}
}
