package cli

import (
	"errors"
	"fmt"

	"github.com/charmbracelet/glamour"
	"github.com/spf13/cobra"

	"github.com/prestontallen/day2day/internal/search"
	"github.com/prestontallen/day2day/internal/style"
)

func newSearchCmd() *cobra.Command {
	var (
		flagLimit int
		flagDeep  bool
		flagJSON  bool
		flagPlain bool
	)
	cmd := &cobra.Command{
		Use:   "search <term>",
		Args:  cobra.ExactArgs(1),
		Short: "Search the worklog (INDEX-first, full-text fallback)",
		Long: `search looks up <term> across the worklog. Algorithm:

  1. INDEX-first: greps INDEX.md for matching By-ticket/By-tag/By-repo
     lines and follows the pointers to extract content.
  2. Full-text fallback: only when INDEX has zero hits, scans WORK.md,
     every archive/*.md, and every notes/*.md directly.

Use --deep to skip the INDEX-first pass (useful when INDEX may be stale).
Use --limit N to cap the number of returned hits (default 50).

Output modes:
  default + TTY  : Glamour-rendered styled markdown
  default + pipe : raw markdown (no ANSI)
  --plain        : raw markdown (no ANSI), regardless of TTY
  --json         : structured Output document with Hit array`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSearch(cmd, args[0], flagLimit, flagDeep, flagJSON, flagPlain)
		},
	}
	cmd.Flags().IntVar(&flagLimit, "limit", 50, "max hits to return (0 = no limit)")
	cmd.Flags().BoolVar(&flagDeep, "deep", false, "skip INDEX-first pass; force full-text scan")
	cmd.Flags().BoolVar(&flagJSON, "json", false, "emit a JSON Output object instead of styled text")
	cmd.Flags().BoolVar(&flagPlain, "plain", false, "emit raw markdown, no styling")
	return cmd
}

func runSearch(cmd *cobra.Command, term string, limit int, deep, asJSON, plain bool) error {
	wd, err := resolveWorkdir()
	if err != nil {
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}

	out, err := search.Run(wd, search.Inputs{
		Term:  term,
		Limit: limit,
		Deep:  deep,
	})
	if err != nil {
		if errors.Is(err, search.ErrEmptyTerm) {
			return jsonOrTextError(cmd, asJSON, 64, "%v", err)
		}
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}

	if asJSON {
		return emitJSON(cmd.OutOrStdout(), out)
	}
	return emitSearchText(cmd, out, plain)
}

func emitSearchText(cmd *cobra.Command, out search.Output, plain bool) error {
	w := cmd.OutOrStdout()

	if len(out.Hits) == 0 {
		fmt.Fprintln(w, style.Dim.Render(fmt.Sprintf("no hits for %q", out.Term)))
		return nil
	}

	suffix := ""
	if out.Truncated {
		suffix = " (truncated)"
	}
	fmt.Fprintln(w, style.Good.Render(
		fmt.Sprintf("found %d hit(s) for %q%s", len(out.Hits), out.Term, suffix)))
	fmt.Fprintln(w)

	useGlamour := !plain && stdoutIsTTY()

	for i, h := range out.Hits {
		header := fmt.Sprintf("%d. %s → %s (%s)", i+1, h.ID, h.File, h.Source)
		fmt.Fprintln(w, style.SubHeading.Render(header))

		if useGlamour {
			if rendered, err := renderMarkdown(h.Snippet); err == nil {
				fmt.Fprint(w, rendered)
			} else {
				fmt.Fprintln(w, h.Snippet)
			}
		} else {
			fmt.Fprintln(w, h.Snippet)
		}
		fmt.Fprintln(w)
	}
	return nil
}

// renderMarkdown runs the snippet through Glamour for styled output. Uses
// auto-style so Glamour picks light/dark based on terminal background.
func renderMarkdown(md string) (string, error) {
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(100),
	)
	if err != nil {
		return md, err
	}
	return r.Render(md)
}
