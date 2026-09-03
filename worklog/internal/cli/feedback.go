package cli

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/prestontallen/ai-devboard/worklog/internal/feedback"
	"github.com/prestontallen/ai-devboard/worklog/internal/storesync"
)

func newFeedbackCmd() *cobra.Command {
	var (
		flagSignal     string
		flagSince      string
		flagUnresolved bool
		flagJSON       bool
		flagPlain      bool
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

Filters AND together: --signal, --since and --unresolved can be combined.

Use 'worklog feedback append' (intended for the capture subagent) to write
new entries, and 'worklog feedback resolve <timestamp>' to mark one
reviewed once you have dealt with it.`,
		// NoArgs so a mistyped subcommand errors instead of silently falling
		// through to the listing — 'worklog feedback resolv 123' should not
		// look like it worked.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFeedbackList(cmd, flagSignal, flagSince, flagUnresolved, flagJSON, flagPlain)
		},
	}
	cmd.Flags().StringVar(&flagSignal, "signal", "", "filter by signal (missing-feature|tui-error|profanity|agent-frustration)")
	cmd.Flags().StringVar(&flagSince, "since", "", "only show entries on or after this date (YYYY-MM-DD)")
	cmd.Flags().BoolVar(&flagUnresolved, "unresolved", false, "only show entries not yet marked reviewed")
	cmd.Flags().BoolVar(&flagJSON, "json", false, "emit structured JSON output")
	cmd.Flags().BoolVar(&flagPlain, "plain", false, "emit raw markdown, no styling")

	cmd.AddCommand(newFeedbackAppendCmd())
	cmd.AddCommand(newFeedbackResolveCmd())
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

func newFeedbackResolveCmd() *cobra.Command {
	var flagJSON bool

	cmd := &cobra.Command{
		Use:   "resolve <timestamp>",
		Short: "Mark a captured feedback entry as reviewed",
		Long: `resolve records that you have dealt with one feedback entry.

Entries are addressed by the unix timestamp in their heading — the same
value 'worklog feedback --json' reports as "timestamp" — not by their
position in a listing, which shifts as filters change.

Resolving writes a '**Resolved**: <unix-ts>' line into that entry and
leaves the rest of FEEDBACK.md byte-for-byte unchanged. Resolving an
already-resolved entry is a no-op.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFeedbackResolve(cmd, args[0], flagJSON)
		},
	}
	cmd.Flags().BoolVar(&flagJSON, "json", false, "emit the result as JSON")
	return cmd
}

func runFeedbackList(cmd *cobra.Command, signal, since string, unresolvedOnly, asJSON, plain bool) error {
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

	filtered := feedback.Filter(entries, feedback.Signal(signal), sinceTime, unresolvedOnly)

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
	storesync.WarnAfterWrite(wd)

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
		if e.Resolved != 0 {
			fmt.Fprintf(&sb, "**Resolved**: %s\n", time.Unix(e.Resolved, 0).Local().Format("2006-01-02 15:04:05"))
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

func runFeedbackResolve(cmd *cobra.Command, handle string, asJSON bool) error {
	ts, err := strconv.ParseInt(handle, 10, 64)
	if err != nil {
		return jsonOrTextError(cmd, asJSON, 64,
			"feedback: %q is not a timestamp; entries are addressed by the unix stamp in their heading (see 'worklog feedback')", handle)
	}

	wd, err := resolveWorkdir()
	if err != nil {
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}

	at, already, err := feedback.Resolve(wd.FeedbackMD(), ts)
	switch {
	case errors.Is(err, feedback.ErrEntryNotFound):
		return jsonOrTextError(cmd, asJSON, 64, "feedback: no entry stamped %d", ts)
	case err != nil:
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}
	if !already {
		storesync.WarnAfterWrite(wd)
	}

	if asJSON {
		return emitJSON(cmd.OutOrStdout(), map[string]any{
			"timestamp": ts,
			"resolved":  at,
			"already":   already,
		})
	}
	if already {
		fmt.Fprintf(cmd.OutOrStdout(), "already resolved: %d (on %s)\n",
			ts, time.Unix(at, 0).Local().Format("2006-01-02 15:04:05"))
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "resolved: %d\n", ts)
	return nil
}
