package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/prestontallen/day2day/internal/style"
	"github.com/prestontallen/day2day/internal/summarize"
)

func newSummarizeCmd() *cobra.Command {
	var (
		flagJSON  bool
		flagPlain bool
		flagLimit int
	)
	cmd := &cobra.Command{
		Use:   "summarize",
		Args:  cobra.NoArgs,
		Short: "Status-report view of in-progress tickets, grouped by epic",
		Long: `summarize produces a status-report table grouped by epic. Each
group lists its children with status, started date, last update, and a
short progress note. Standalone tickets (no parent epic) appear in a
final "Standalone" group.

Source: in-memory parse of WORK.md plus each ticket's notes/<id>.md
(for last-update + progress note). No archive scanning.

Output modes:
  default + TTY  : Glamour-rendered markdown table
  default + pipe : raw markdown
  --plain        : raw markdown regardless of TTY
  --json         : structured Summary`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSummarize(cmd, flagJSON, flagPlain, flagLimit)
		},
	}
	cmd.Flags().BoolVar(&flagJSON, "json", false, "emit structured JSON")
	cmd.Flags().BoolVar(&flagPlain, "plain", false, "emit raw markdown, no styling")
	cmd.Flags().IntVar(&flagLimit, "limit", 0, "cap rows per group (0 = unlimited)")
	return cmd
}

func runSummarize(cmd *cobra.Command, asJSON, plain bool, limit int) error {
	wd, err := resolveWorkdir()
	if err != nil {
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}

	s, err := summarize.Build(wd)
	if err != nil {
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}

	if limit > 0 {
		for i := range s.Groups {
			if len(s.Groups[i].Rows) > limit {
				s.Groups[i].Rows = s.Groups[i].Rows[:limit]
			}
		}
	}

	if asJSON {
		return emitJSON(cmd.OutOrStdout(), s)
	}

	md := renderSummaryMarkdown(s)
	if !plain && stdoutIsTTY() {
		if rendered, err := renderMarkdown(md); err == nil {
			fmt.Fprint(cmd.OutOrStdout(), rendered)
			return nil
		}
	}
	fmt.Fprint(cmd.OutOrStdout(), md)
	return nil
}

func renderSummaryMarkdown(s summarize.Summary) string {
	if len(s.Groups) == 0 {
		return "No active work to summarize.\n"
	}

	var sb strings.Builder
	for _, g := range s.Groups {
		agg := g.Aggregate
		var header string
		if g.Kind == "epic" {
			header = fmt.Sprintf("## %s — %s (%d%%, %d/%d)",
				g.Title, agg.Status, agg.PercentDone, agg.Done, agg.Total)
		} else {
			header = fmt.Sprintf("## %s — %s", g.Title, agg.Status)
		}
		sb.WriteString(style.Heading.Render(header))
		sb.WriteString("\n\n")

		if len(g.Rows) == 0 {
			sb.WriteString("_no rows_\n\n")
			continue
		}

		sb.WriteString("| Item | Status | Started | Updated | Note |\n")
		sb.WriteString("|---|---|---|---|---|\n")
		for _, r := range g.Rows {
			note := r.Note
			started := r.Started
			updated := r.LastUpdate
			sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s |\n",
				mdEscape(r.ID), mdEscape(r.Status),
				mdEscape(started), mdEscape(updated), mdEscape(note)))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// mdEscape replaces pipe characters to avoid breaking markdown tables.
func mdEscape(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}
