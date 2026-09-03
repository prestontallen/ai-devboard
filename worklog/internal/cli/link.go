package cli

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/prestontallen/ai-devboard/worklog/internal/link"
	"github.com/prestontallen/ai-devboard/worklog/internal/storesync"
	"github.com/prestontallen/ai-devboard/worklog/internal/style"
)

func newLinkCmd() *cobra.Command {
	var (
		flagClear bool
		flagEdit  bool
		flagJSON  bool
	)
	cmd := &cobra.Command{
		Use:   "link <id> [name] [url]",
		Args:  cobra.RangeArgs(1, 3),
		Short: "Read, list, or update a ticket's named **Link**: entries",
		Long: `link reads or updates a ticket's arbitrarily-named links — Jira, a Slack
thread, a design doc, anything that isn't the PR (use 'worklog pr' for
that). A ticket may carry any number of links, each with its own name.

Usage:
  worklog link <id>                        # list every link on the ticket
  worklog link <id> <name>                 # show one link's current value
  worklog link <id> <name> <url>           # set (insert or update)
  worklog link <id> <name> --clear         # remove that one link
  worklog link <id> <name> --edit          # open a Huh input pre-populated

Unlike **PR**:, a cleared or never-set link's line is omitted entirely —
most tickets won't have every possible link, so nothing renders until it's
set. Any string is accepted for the URL (no validation).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLink(cmd, args, flagClear, flagEdit, flagJSON)
		},
	}
	cmd.Flags().BoolVar(&flagClear, "clear", false, "remove the named link")
	cmd.Flags().BoolVar(&flagEdit, "edit", false, "open an interactive prompt pre-populated with the current value")
	cmd.Flags().BoolVar(&flagJSON, "json", false, "emit a JSON result object instead of styled text")
	return cmd
}

func runLink(cmd *cobra.Command, args []string, clear, edit, asJSON bool) error {
	id := strings.ToLower(strings.TrimSpace(args[0]))
	hasName := len(args) >= 2
	var name string
	if hasName {
		name = strings.TrimSpace(args[1])
	}
	hasURL := len(args) == 3
	var url string
	if hasURL {
		url = args[2]
	}

	if hasURL && clear {
		return jsonOrTextError(cmd, asJSON, 64,
			"link: cannot combine a positional URL with --clear")
	}
	if hasURL && edit {
		return jsonOrTextError(cmd, asJSON, 64,
			"link: cannot combine a positional URL with --edit")
	}
	if clear && edit {
		return jsonOrTextError(cmd, asJSON, 64,
			"link: cannot combine --clear with --edit")
	}
	if (clear || edit) && !hasName {
		return jsonOrTextError(cmd, asJSON, 64,
			"link: --clear/--edit require a <name>")
	}
	// "PR" is reserved. OnPR mirrors the PR into the same label array OnLink
	// edits, and the label match is case-insensitive, so a link by this name
	// reaches worklog pr's data. Refusing the name is what makes that
	// unreachable; a guard on the write path alone would still leave --clear.
	if hasName && strings.EqualFold(name, "PR") {
		return jsonOrTextError(cmd, asJSON, 64,
			"link: %q is a reserved name; use 'worklog pr' to read or set the PR", name)
	}

	wd, err := resolveWorkdir()
	if err != nil {
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}

	// No name at all: list every link on the ticket.
	if !hasName {
		res, err := link.List(wd, id)
		if err != nil {
			return jsonOrTextError(cmd, asJSON, 1, "%v", err)
		}
		if asJSON {
			return emitJSON(cmd.OutOrStdout(), res)
		}
		w := cmd.OutOrStdout()
		if len(res.Links) == 0 {
			fmt.Fprintln(w, style.Dim.Render("no links set"))
			return nil
		}
		for _, l := range res.Links {
			fmt.Fprintf(w, "%s: %s\n", l.Name, l.URL)
		}
		return nil
	}

	// Read-only path: name given, no URL, no flags.
	if !hasURL && !clear && !edit {
		res, err := link.Get(wd, id, name)
		if err != nil {
			return jsonOrTextError(cmd, asJSON, 1, "%v", err)
		}
		if asJSON {
			return emitJSON(cmd.OutOrStdout(), res)
		}
		w := cmd.OutOrStdout()
		if res.URL == "" {
			fmt.Fprintln(w, style.Dim.Render(name+": (not set)"))
		} else {
			fmt.Fprintln(w, name+": "+res.URL)
		}
		return nil
	}

	// Edit path: prompt with current value. hasURL is guaranteed false
	// here (rejected above), so url is still unset going in.
	if edit {
		current, err := link.Get(wd, id, name)
		if err != nil {
			return jsonOrTextError(cmd, asJSON, 1, "%v", err)
		}
		if !stdinIsTTY() {
			return jsonOrTextError(cmd, asJSON, 64,
				"link --edit requires a TTY on stdin")
		}
		val := current.URL
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title(name + " URL").
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

	res, err := link.SetLink(wd, id, name, value)
	if err != nil {
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}
	if res.Parent != "" {
		devboardOnLinkChild(res.Parent, id, name, value)
	} else {
		devboardOnLink(id, name, value)
	}
	storesync.WarnAfterWrite(wd)
	if asJSON {
		return emitJSON(cmd.OutOrStdout(), res)
	}
	w := cmd.OutOrStdout()
	switch {
	case res.URL == "" && res.Previous == "":
		fmt.Fprintln(w, style.Dim.Render(name+" cleared (was already empty)"))
	case res.URL == "":
		fmt.Fprintln(w, style.Good.Render(name+" cleared (was: "+res.Previous+")"))
	default:
		fmt.Fprintln(w, style.Good.Render(name+" set: "+res.URL))
	}
	return nil
}
