package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/prestontallen/day2day/internal/lint"
	"github.com/prestontallen/day2day/internal/style"
)

func newLintSpecsCmd() *cobra.Command {
	var printMode bool
	cmd := &cobra.Command{
		Use:   "lint-specs",
		Args:  cobra.NoArgs,
		Short: "Detect drift between rule blocks across the spec files",
		Long: `lint-specs extracts the rule block (between <!-- rules:start --> and
<!-- rules:end --> markers) from each spec file and pairwise-diffs the
results. Multi-agent setups intentionally duplicate the rule statements
across SKILL.md, README.md, and the slash-command spec; this command
makes accidental drift visible.

Exit codes:
  0  rule blocks identical, or --print succeeded
  1  drift detected, missing marker, or file missing
  64 usage error`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLintSpecs(cmd, printMode)
		},
	}
	cmd.Flags().BoolVar(&printMode, "print", false, "emit each rule block under a header instead of diffing")
	return cmd
}

func runLintSpecs(cmd *cobra.Command, printMode bool) error {
	repoRoot, err := resolveRepoRoot()
	if err != nil {
		return errWithExit(1, "%v", err)
	}
	paths := lint.DefaultSpecs(repoRoot)

	out := cmd.OutOrStdout()

	if printMode {
		if err := lint.RunPrint(paths, out); err != nil {
			if errors.Is(err, lint.ErrMissingMarker) {
				return errWithExit(1, "%v", err)
			}
			return errWithExit(1, "%v", err)
		}
		return nil
	}

	// Diff mode: use a styled writer that highlights DRIFT headers and the
	// +/- diff prefixes.
	var diffBuf strings.Builder
	drift, err := lint.RunCheck(paths, &diffBuf)
	if err != nil {
		if errors.Is(err, lint.ErrMissingMarker) {
			return errWithExit(1, "%v", err)
		}
		return errWithExit(1, "%v", err)
	}

	if !drift {
		fmt.Fprintln(out, style.Good.Render("lint-specs: rule blocks in sync"))
		return nil
	}

	for _, line := range strings.Split(diffBuf.String(), "\n") {
		switch {
		case strings.HasPrefix(line, "=== DRIFT"):
			fmt.Fprintln(out, style.Heading.Render(line))
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
			fmt.Fprintln(out, style.SubHeading.Render(line))
		case strings.HasPrefix(line, "+"):
			fmt.Fprintln(out, style.Good.Render(line))
		case strings.HasPrefix(line, "-"):
			fmt.Fprintln(out, style.Bad.Render(line))
		case strings.HasPrefix(line, "@@"):
			fmt.Fprintln(out, style.Dim.Render(line))
		default:
			fmt.Fprintln(out, line)
		}
	}
	fmt.Fprintln(out, style.Bad.Render("lint-specs: rule blocks differ across spec files"))
	return errWithExit(1, "")
}
