package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/prestontallen/day2day/internal/pr"
	"github.com/prestontallen/day2day/internal/style"
)

func newPrCmd() *cobra.Command {
	var (
		flagClear bool
		flagEdit  bool
		flagJSON  bool
	)
	cmd := &cobra.Command{
		Use:   "pr <id> [url]",
		Args:  cobra.RangeArgs(1, 2),
		Short: "Read or update the **PR**: field on a ticket",
		Long: `pr reads or updates the optional **PR**: field on a live WORK.md
ticket block.

Usage:
  worklog pr <id>                        # show current value
  worklog pr <id> <url>                  # set value
  worklog pr <id> --clear                # empty the value (line stays rendered)
  worklog pr <id> --edit                 # open a Huh input pre-populated

The **PR**: line is always rendered on the block, even when empty, so the
field stays visibly available. Any string is accepted (no URL validation).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var url string
			if len(args) == 2 {
				url = args[1]
			}
			return runPR(cmd, args[0], url, flagClear, flagEdit, flagJSON)
		},
	}
	cmd.Flags().BoolVar(&flagClear, "clear", false, "empty the PR field (keeps the line rendered)")
	cmd.Flags().BoolVar(&flagEdit, "edit", false, "open an interactive prompt pre-populated with the current value")
	cmd.Flags().BoolVar(&flagJSON, "json", false, "emit a JSON result object instead of styled text")
	return cmd
}

func runPR(cmd *cobra.Command, id, url string, clear, edit, asJSON bool) error {
	hasURL := url != ""
	if hasURL && clear {
		return jsonOrTextError(cmd, asJSON, 64,
			"pr: cannot combine a positional URL with --clear")
	}
	if hasURL && edit {
		return jsonOrTextError(cmd, asJSON, 64,
			"pr: cannot combine a positional URL with --edit")
	}
	if clear && edit {
		return jsonOrTextError(cmd, asJSON, 64,
			"pr: cannot combine --clear with --edit")
	}

	wd, err := resolveWorkdir()
	if err != nil {
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}

	id = strings.ToLower(strings.TrimSpace(id))

	// Read-only path: no flags, no positional value.
	if !hasURL && !clear && !edit {
		res, err := pr.Get(wd, id)
		if err != nil {
			return mapPRError(cmd, asJSON, err)
		}
		if asJSON {
			return emitJSON(cmd.OutOrStdout(), res)
		}
		w := cmd.OutOrStdout()
		if res.PR == "" {
			fmt.Fprintln(w, style.Dim.Render("PR: (empty)"))
		} else {
			fmt.Fprintln(w, "PR: "+res.PR)
		}
		return nil
	}

	// Edit path: prompt with current value.
	if edit {
		current, err := pr.Get(wd, id)
		if err != nil {
			return mapPRError(cmd, asJSON, err)
		}
		if !stdinIsTTY() {
			return jsonOrTextError(cmd, asJSON, 64,
				"pr --edit requires a TTY on stdin")
		}
		val := current.PR
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("PR URL").
					Value(&val),
			),
		)
		if err := form.Run(); err != nil {
			return jsonOrTextError(cmd, asJSON, 1, "%v", err)
		}
		url = val
	}

	value := ""
	if !clear {
		value = url
	}

	res, err := pr.SetPR(wd, id, value)
	if err != nil {
		return mapPRError(cmd, asJSON, err)
	}
	devboardOnPR(id, value)
	if asJSON {
		return emitJSON(cmd.OutOrStdout(), res)
	}
	w := cmd.OutOrStdout()
	switch {
	case res.PR == "" && res.Previous == "":
		fmt.Fprintln(w, style.Dim.Render("PR cleared (was already empty)"))
	case res.PR == "":
		fmt.Fprintln(w, style.Good.Render("PR cleared (was: "+res.Previous+")"))
	default:
		fmt.Fprintln(w, style.Good.Render("PR set: "+res.PR))
	}
	return nil
}

func mapPRError(cmd *cobra.Command, asJSON bool, err error) error {
	if errors.Is(err, pr.ErrIDNotFound) {
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}
	return jsonOrTextError(cmd, asJSON, 1, "%v", err)
}
