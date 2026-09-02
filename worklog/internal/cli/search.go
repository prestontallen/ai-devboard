package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/spf13/cobra"

	"github.com/prestontallen/ai-devboard/worklog/internal/search"
	"github.com/prestontallen/ai-devboard/worklog/internal/style"
)

func newSearchCmd() *cobra.Command {
	var (
		flagLimit int
		flagDeep  bool
		flagJSON  bool
		flagPlain bool
		flagAllOf string
		flagAnyOf string
	)
	cmd := &cobra.Command{
		Use:   "search [term]",
		Args:  cobra.MaximumNArgs(1),
		Short: "Search the worklog (INDEX-first, full-text fallback)",
		Long: `search looks up <term> across the worklog. Algorithm:

  1. INDEX-first: greps INDEX.md for matching By-ticket/By-tag/By-repo
     lines and follows the pointers to extract content.
  2. Full-text fallback: only when INDEX has zero hits, scans WORK.md,
     every archive/*.md, and every notes/*.md directly.

Use --all-of "a,b,c" for AND search (all terms must appear in a hit).
Use --any-of "a,b,c" for OR search (at least one term must appear).
Use --deep to skip the INDEX-first pass (useful when INDEX may be stale).
Use --limit N to cap the number of returned hits (default 50).

Output modes:
  default + TTY  : Glamour-rendered styled markdown
  default + pipe : raw markdown (no ANSI)
  --plain        : raw markdown (no ANSI), regardless of TTY
  --json         : structured Output document with Hit array`,
		RunE: func(cmd *cobra.Command, args []string) error {
			positional := ""
			if len(args) == 1 {
				positional = args[0]
			}
			return runSearch(cmd, positional, flagAllOf, flagAnyOf, flagLimit, flagDeep, flagJSON, flagPlain)
		},
	}
	cmd.Flags().IntVar(&flagLimit, "limit", 50, "max hits to return (0 = no limit)")
	cmd.Flags().BoolVar(&flagDeep, "deep", false, "skip INDEX-first pass; force full-text scan")
	cmd.Flags().BoolVar(&flagJSON, "json", false, "emit a JSON Output object instead of styled text")
	cmd.Flags().BoolVar(&flagPlain, "plain", false, "emit raw markdown, no styling")
	cmd.Flags().StringVar(&flagAllOf, "all-of", "", "comma-separated terms; all must appear in a hit")
	cmd.Flags().StringVar(&flagAnyOf, "any-of", "", "comma-separated terms; at least one must appear in a hit")
	return cmd
}

func runSearch(cmd *cobra.Command, positional, allOf, anyOf string, limit int, deep, asJSON, plain bool) error {
	q, err := buildQuery(positional, allOf, anyOf)
	if err != nil {
		return jsonOrTextError(cmd, asJSON, 64, "%v", err)
	}

	wd, wdErr := resolveWorkdir()
	if wdErr != nil {
		return jsonOrTextError(cmd, asJSON, 1, "%v", wdErr)
	}

	out, err := search.Run(wd, search.Inputs{
		Query: q,
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

func buildQuery(positional, allOf, anyOf string) (search.Query, error) {
	hasPositional := strings.TrimSpace(positional) != ""
	hasAll := strings.TrimSpace(allOf) != ""
	hasAny := strings.TrimSpace(anyOf) != ""

	setCount := 0
	if hasPositional {
		setCount++
	}
	if hasAll {
		setCount++
	}
	if hasAny {
		setCount++
	}
	if setCount == 0 {
		return search.Query{}, errors.New("search: provide a positional term, --all-of, or --any-of")
	}
	if setCount > 1 {
		return search.Query{}, errors.New("search: positional, --all-of, --any-of are mutually exclusive")
	}

	switch {
	case hasPositional:
		return search.Query{Terms: []string{strings.ToLower(strings.TrimSpace(positional))}, Mode: search.ModeSingle}, nil
	case hasAll:
		terms := splitTerms(allOf)
		if len(terms) == 0 {
			return search.Query{}, errors.New("search: --all-of must contain at least one term after splitting")
		}
		return search.Query{Terms: terms, Mode: search.ModeAllOf}, nil
	default: // hasAny
		terms := splitTerms(anyOf)
		if len(terms) == 0 {
			return search.Query{}, errors.New("search: --any-of must contain at least one term after splitting")
		}
		return search.Query{Terms: terms, Mode: search.ModeAnyOf}, nil
	}
}

func splitTerms(csv string) []string {
	var out []string
	for _, t := range strings.Split(csv, ",") {
		if t = strings.ToLower(strings.TrimSpace(t)); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func emitSearchText(cmd *cobra.Command, out search.Output, plain bool) error {
	w := cmd.OutOrStdout()

	if len(out.Hits) == 0 {
		label := queryLabel(out.Query)
		fmt.Fprintln(w, style.Dim.Render(fmt.Sprintf("no hits for %s", label)))
		return nil
	}

	suffix := ""
	if out.Truncated {
		suffix = " (truncated)"
	}
	label := queryLabel(out.Query)
	fmt.Fprintln(w, style.Good.Render(
		fmt.Sprintf("found %d hit(s) for %s%s", len(out.Hits), label, suffix)))
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

// queryLabel formats the query for human-readable output.
func queryLabel(q search.Query) string {
	if q.Mode == search.ModeSingle {
		return fmt.Sprintf("%q", q.Terms[0])
	}
	return fmt.Sprintf("%s [%s]", string(q.Mode), strings.Join(q.Terms, ", "))
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
