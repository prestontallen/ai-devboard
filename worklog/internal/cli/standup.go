package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/prestontallen/day2day/internal/standup"
)

func newStandupCmd() *cobra.Command {
	var (
		flagSince  string
		flagUntil  string
		flagDays   int
		flagSimple bool
		flagJSON   bool
	)
	cmd := &cobra.Command{
		Use:   "standup",
		Args:  cobra.NoArgs,
		Short: "Daily standup report: completed yesterday, active today, blockers",
		Long: `Builds a standup-style report from existing worklog data:

  Yesterday : archived entries completed in the window (default: yesterday)
  Today     : tickets in [~] state under ## Now
  Blockers  : tickets under ## Waiting, with waiting-since age

Default window is the previous calendar day. Override with --since/--until
or --days N. All dates are local-time YYYY-MM-DD. Use --simple for a
compact flat bullet list with done:/active:/waiting: prefixes.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStandup(cmd, flagSince, flagUntil, flagDays, flagSimple, flagJSON,
				cmd.Flags().Changed("days"))
		},
	}
	cmd.Flags().StringVar(&flagSince, "since", "", "lower bound (YYYY-MM-DD), inclusive (default: yesterday)")
	cmd.Flags().StringVar(&flagUntil, "until", "", "upper bound (YYYY-MM-DD), inclusive (default: today)")
	cmd.Flags().IntVar(&flagDays, "days", 0, "shortcut: last N days (conflicts with --since)")
	cmd.Flags().BoolVar(&flagSimple, "simple", false, "compact flat bullet list (text mode only)")
	cmd.Flags().BoolVar(&flagJSON, "json", false, "emit structured JSON output")
	return cmd
}

func runStandup(cmd *cobra.Command, flagSince, flagUntil string, flagDays int, simple, asJSON, daysChanged bool) error {
	// Flag validation.
	if daysChanged && flagSince != "" {
		return jsonOrTextError(cmd, asJSON, 64, "standup: use either --days or --since, not both")
	}
	if daysChanged && flagDays <= 0 {
		return jsonOrTextError(cmd, asJSON, 64, "standup: --days must be positive")
	}

	today := truncateLocalDate(time.Now())

	var opts standup.Options
	opts.Today = today

	if daysChanged {
		opts.Since = today.AddDate(0, 0, -flagDays)
		opts.Until = today
	} else {
		if flagSince != "" {
			t, err := time.ParseInLocation("2006-01-02", flagSince, time.Local)
			if err != nil {
				return jsonOrTextError(cmd, asJSON, 64, "standup: --since: expected YYYY-MM-DD, got %q", flagSince)
			}
			opts.Since = t
		}
		if flagUntil != "" {
			t, err := time.ParseInLocation("2006-01-02", flagUntil, time.Local)
			if err != nil {
				return jsonOrTextError(cmd, asJSON, 64, "standup: --until: expected YYYY-MM-DD, got %q", flagUntil)
			}
			opts.Until = t
		}
	}

	wd, err := resolveWorkdir()
	if err != nil {
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}

	report, err := standup.Build(wd, opts)
	if err != nil {
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}

	if asJSON {
		return emitJSON(cmd.OutOrStdout(), report)
	}

	var md string
	if simple {
		md = formatSimple(report)
	} else {
		md = formatReport(report)
	}

	if stdoutIsTTY() {
		if rendered, err := renderMarkdown(md); err == nil {
			fmt.Fprint(cmd.OutOrStdout(), rendered)
			return nil
		}
	}
	fmt.Fprint(cmd.OutOrStdout(), md)
	return nil
}

func truncateLocalDate(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func formatReport(r standup.Report) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Standup — %s\n\n", r.Today))

	// Yesterday / Completed heading.
	var completedHeading string
	if r.Since == r.Until {
		completedHeading = fmt.Sprintf("## Yesterday (%s)", r.Since)
	} else {
		completedHeading = fmt.Sprintf("## Completed (%s → %s)", r.Since, r.Until)
	}
	sb.WriteString(completedHeading + "\n")
	if len(r.Completed) == 0 {
		sb.WriteString("_(none)_\n")
	} else {
		for _, e := range r.Completed {
			sb.WriteString(formatCompletedLine(e))
		}
	}
	sb.WriteString("\n")

	sb.WriteString("## Today\n")
	if len(r.Active) == 0 {
		sb.WriteString("_(none)_\n")
	} else {
		for _, e := range r.Active {
			sb.WriteString(formatActiveLine(e))
		}
	}
	sb.WriteString("\n")

	sb.WriteString("## Blockers\n")
	if len(r.Waiting) == 0 {
		sb.WriteString("_(none)_\n")
	} else {
		for _, e := range r.Waiting {
			sb.WriteString(formatWaitingLine(e))
		}
	}
	sb.WriteString("\n")

	return sb.String()
}

func formatCompletedLine(e standup.CompletedEntry) string {
	meta := buildMeta(e.Repo, e.PR)
	line := fmt.Sprintf("- **%s** — %s", strings.ToUpper(e.ID), e.Title)
	if meta != "" {
		line += fmt.Sprintf(" (%s)", meta)
	}
	line += "\n"
	if e.Summary != "" {
		s := e.Summary
		if len(s) > 100 {
			s = s[:100]
		}
		line += fmt.Sprintf("  > %s\n", s)
	}
	return line
}

func formatActiveLine(e standup.ActiveEntry) string {
	parts := []string{}
	if e.Repo != "" {
		parts = append(parts, e.Repo)
	}
	if e.PR != "" {
		parts = append(parts, e.PR)
	}
	if e.Started != "" {
		parts = append(parts, "started "+e.Started)
	}
	line := fmt.Sprintf("- **%s** — %s", strings.ToUpper(e.ID), e.Title)
	if len(parts) > 0 {
		line += fmt.Sprintf(" (%s)", strings.Join(parts, ", "))
	}
	return line + "\n"
}

func formatWaitingLine(e standup.BlockerEntry) string {
	parts := []string{}
	if e.Repo != "" {
		parts = append(parts, e.Repo)
	}
	if e.PR != "" {
		parts = append(parts, e.PR)
	}
	if e.WaitingSince != "" {
		if e.WaitingDays >= 0 {
			parts = append(parts, fmt.Sprintf("waiting since %s, %dd", e.WaitingSince, e.WaitingDays))
		} else {
			parts = append(parts, fmt.Sprintf("waiting since %s", e.WaitingSince))
		}
	}
	line := fmt.Sprintf("- **%s** — %s", strings.ToUpper(e.ID), e.Title)
	if len(parts) > 0 {
		line += fmt.Sprintf(" (%s)", strings.Join(parts, ", "))
	}
	return line + "\n"
}

func buildMeta(repo, pr string) string {
	var parts []string
	if repo != "" {
		parts = append(parts, repo)
	}
	if pr != "" {
		parts = append(parts, pr)
	}
	return strings.Join(parts, ", ")
}

func formatSimple(r standup.Report) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Standup — %s\n\n", r.Today))

	hasAny := len(r.Completed)+len(r.Active)+len(r.Waiting) > 0
	if !hasAny {
		sb.WriteString("_no entries in window_\n")
		return sb.String()
	}

	for _, e := range r.Completed {
		sb.WriteString(fmt.Sprintf("- done: %s — %s\n", strings.ToUpper(e.ID), e.Title))
	}
	for _, e := range r.Active {
		sb.WriteString(fmt.Sprintf("- active: %s — %s\n", strings.ToUpper(e.ID), e.Title))
	}
	for _, e := range r.Waiting {
		if e.WaitingDays >= 0 {
			sb.WriteString(fmt.Sprintf("- waiting: %s — %s (%dd)\n",
				strings.ToUpper(e.ID), e.Title, e.WaitingDays))
		} else {
			sb.WriteString(fmt.Sprintf("- waiting: %s — %s\n",
				strings.ToUpper(e.ID), e.Title))
		}
	}
	return sb.String()
}
