package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/prestontallen/day2day/internal/feedback"
)

func newFeedbackCmd() *cobra.Command {
	var (
		flagSignal string
		flagSince  string
		flagJSON   bool
		flagPlain  bool
	)

	cmd := &cobra.Command{
		Use:   "feedback",
		Short: "List or append captured feedback",
		Long: `feedback lists friction-signal entries captured by the worklog agent.

Entries are stored in FEEDBACK.md in the worklog data directory. Each entry
carries a signal type, a one-line trigger summary, an optional excerpt of the
relevant conversation, and an optional dispatcher context note.

Output modes:
  default + TTY  : Glamour-rendered styled markdown
  default + pipe : raw markdown (no ANSI)
  --plain        : raw markdown (no ANSI), regardless of TTY
  --json         : structured {"entries": [...], "count": N}

Use 'worklog feedback append' (intended for the capture subagent) to write
new entries.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFeedbackList(cmd, flagSignal, flagSince, flagJSON, flagPlain)
		},
	}
	cmd.Flags().StringVar(&flagSignal, "signal", "", "filter by signal (missing-feature|tui-error|profanity|agent-frustration)")
	cmd.Flags().StringVar(&flagSince, "since", "", "only show entries on or after this date (YYYY-MM-DD)")
	cmd.Flags().BoolVar(&flagJSON, "json", false, "emit structured JSON output")
	cmd.Flags().BoolVar(&flagPlain, "plain", false, "emit raw markdown, no styling")

	cmd.AddCommand(newFeedbackAppendCmd())
	return cmd
}

func newFeedbackAppendCmd() *cobra.Command {
	var (
		flagSignal  string
		flagTrigger string
		flagExcerpt string
		flagContext string
		flagJSON    bool
	)

	cmd := &cobra.Command{
		Use:   "append",
		Short: "Append a captured feedback entry (used by the capture subagent)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFeedbackAppend(cmd, flagSignal, flagTrigger, flagExcerpt, flagContext, flagJSON)
		},
	}
	cmd.Flags().StringVar(&flagSignal, "signal", "", "signal type (required): missing-feature|tui-error|profanity|agent-frustration")
	cmd.Flags().StringVar(&flagTrigger, "trigger", "", "one-line summary of what happened (required)")
	cmd.Flags().StringVar(&flagExcerpt, "excerpt", "", "verbatim conversation excerpt (optional)")
	cmd.Flags().StringVar(&flagContext, "context", "", "dispatcher context note (optional)")
	cmd.Flags().BoolVar(&flagJSON, "json", false, "emit the stamped entry as JSON on success")
	return cmd
}

func runFeedbackList(cmd *cobra.Command, signal, since string, asJSON, plain bool) error {
	if signal != "" && !feedback.IsValidSignal(signal) {
		return jsonOrTextError(cmd, asJSON, 64,
			"feedback: unknown signal %q; valid: %s", signal, signalList())
	}

	var sinceTime time.Time
	if since != "" {
		t, err := time.ParseInLocation("2006-01-02", since, time.Local)
		if err != nil {
			return jsonOrTextError(cmd, asJSON, 64,
				"feedback: --since must be YYYY-MM-DD, got %q", since)
		}
		sinceTime = t
	}

	wd, err := resolveWorkdir()
	if err != nil {
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}

	entries, err := feedback.Parse(wd.FeedbackMD())
	if err != nil {
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}

	filtered := feedback.Filter(entries, feedback.Signal(signal), sinceTime)

	if asJSON {
		if filtered == nil {
			filtered = []feedback.Entry{}
		}
		return emitJSON(cmd.OutOrStdout(), map[string]any{
			"entries": filtered,
			"count":   len(filtered),
		})
	}

	w := cmd.OutOrStdout()
	if len(filtered) == 0 {
		fmt.Fprintln(w, "no feedback entries")
		return nil
	}

	md := buildFeedbackMarkdown(filtered)
	if !plain && stdoutIsTTY() {
		rendered, err := renderMarkdown(md)
		if err == nil {
			fmt.Fprint(w, rendered)
			return nil
		}
	}
	fmt.Fprint(w, md)
	return nil
}

func runFeedbackAppend(cmd *cobra.Command, signal, trigger, excerpt, context string, asJSON bool) error {
	if !feedback.IsValidSignal(signal) {
		return jsonOrTextError(cmd, asJSON, 64,
			"feedback: unknown signal %q; valid: %s", signal, signalList())
	}
	if strings.TrimSpace(trigger) == "" {
		return jsonOrTextError(cmd, asJSON, 64, "feedback: --trigger is required")
	}

	wd, err := resolveWorkdir()
	if err != nil {
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}

	e := feedback.Entry{
		Signal:  feedback.Signal(signal),
		Trigger: trigger,
		Excerpt: excerpt,
		Context: context,
	}

	out, err := feedback.Append(wd.FeedbackMD(), e)
	if err != nil {
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}

	if asJSON {
		return emitJSON(cmd.OutOrStdout(), out)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "appended: %d %s\n", out.Timestamp, out.Signal)
	return nil
}

// buildFeedbackMarkdown renders filtered entries as a human-readable markdown
// string, converting unix timestamps to local time in headings.
func buildFeedbackMarkdown(entries []feedback.Entry) string {
	var sb strings.Builder
	for _, e := range entries {
		ts := time.Unix(e.Timestamp, 0).Local().Format("2006-01-02 15:04:05")
		fmt.Fprintf(&sb, "## %s — %s\n", ts, e.Signal)
		fmt.Fprintf(&sb, "**Trigger**: %s\n", e.Trigger)
		if e.Excerpt != "" {
			sb.WriteString("**Excerpt**:\n")
			for _, line := range strings.Split(e.Excerpt, "\n") {
				fmt.Fprintf(&sb, "> %s\n", line)
			}
		}
		if e.Context != "" {
			fmt.Fprintf(&sb, "**Context**: %s\n", e.Context)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// signalList returns a comma-separated list of valid signal names.
func signalList() string {
	sigs := feedback.AllSignals()
	names := make([]string, len(sigs))
	for i, s := range sigs {
		names[i] = string(s)
	}
	return strings.Join(names, ", ")
}
