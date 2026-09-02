package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/prestontallen/ai-devboard/worklog/internal/edit"
	"github.com/prestontallen/ai-devboard/worklog/internal/style"
)

// editFlag binds one --<flag> to the field it writes. Declaring them in a
// table keeps flag registration, the assignment order, and the help text from
// drifting apart as fields are added.
type editFlag struct {
	name  string
	field string
	usage string
}

// editFlags is in canonical field order, which is also the order writes are
// applied in, so a single invocation produces a deterministic diff.
var editFlags = []editFlag{
	{"title", edit.TitleField, "ticket title (the text after the em dash)"},
	{"repo", "Repo", "repository name"},
	{"tags", "Tags", "comma-separated tags"},
	{"notes", "Notes", "path to the notes file, relative to the worklog dir"},
	{"files", "Files", "comma-separated file paths"},
	{"acceptance", "Acceptance", "one-line acceptance criterion"},
	{"status", "Status", "free-text status line"},
}

func newEditCmd() *cobra.Command {
	var (
		values   = make(map[string]*string, len(editFlags))
		flagJSON bool
	)

	cmd := &cobra.Command{
		Use:   "edit <id>",
		Args:  cobra.ExactArgs(1),
		Short: "Set metadata fields on an existing ticket block",
		Long: `edit writes metadata fields on a ticket that already exists in
WORK.md, in any section. It is the setter for the fields no lifecycle command
owns.

Usage:
  worklog edit <id> --acceptance "login works"    # set (inserts the line if absent)
  worklog edit <id> --status "in review" --tags a,b
  worklog edit <id> --acceptance ""               # remove the line

Passing a flag with an empty value removes that field's line. Not passing the
flag leaves the field alone. Inserted lines land in the same position a freshly
rendered block would put them.

These fields belong to other commands and are not editable here:
  ID, Type, Parent           structural (ID is the join key for notes and index)
  Started, Waiting since     stamped by 'worklog start' and 'worklog wait'
  PR                         use 'worklog pr <id> <url>'
  Active children            maintained by the epic machinery`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var assignments []edit.Assignment
			for _, f := range editFlags {
				if cmd.Flags().Changed(f.name) {
					assignments = append(assignments,
						edit.Assignment{Field: f.field, Value: *values[f.name]})
				}
			}
			return runEdit(cmd, args[0], assignments, flagJSON)
		},
	}

	for _, f := range editFlags {
		v := new(string)
		values[f.name] = v
		cmd.Flags().StringVar(v, f.name, "", f.usage)
	}
	cmd.Flags().BoolVar(&flagJSON, "json", false, "emit a JSON result object instead of styled text")
	return cmd
}

func runEdit(cmd *cobra.Command, id string, assignments []edit.Assignment, asJSON bool) error {
	if len(assignments) == 0 {
		return jsonOrTextError(cmd, asJSON, 64,
			"edit: no fields given; pass at least one of --%s",
			strings.Join(editFlagNames(), ", --"))
	}

	wd, err := resolveWorkdir()
	if err != nil {
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}

	id = strings.ToLower(strings.TrimSpace(id))
	res, err := edit.Apply(wd, id, assignments)
	if err != nil {
		return mapEditError(cmd, asJSON, err)
	}

	if asJSON {
		return emitJSON(cmd.OutOrStdout(), res)
	}
	w := cmd.OutOrStdout()
	for _, c := range res.Changes {
		switch {
		case c.To == "":
			fmt.Fprintln(w, style.Good.Render(c.Field+" cleared")+
				style.Dim.Render("  (was: "+c.From+")"))
		case c.From == "":
			fmt.Fprintln(w, style.Good.Render(c.Field+": "+c.To))
		default:
			fmt.Fprintln(w, style.Good.Render(c.Field+": "+c.To)+
				style.Dim.Render("  (was: "+c.From+")"))
		}
	}
	return nil
}

func editFlagNames() []string {
	names := make([]string, len(editFlags))
	for i, f := range editFlags {
		names[i] = f.name
	}
	return names
}

func mapEditError(cmd *cobra.Command, asJSON bool, err error) error {
	switch {
	case errors.Is(err, edit.ErrNotEditable), errors.Is(err, edit.ErrEmptyTitle),
		errors.Is(err, edit.ErrNoFields):
		return jsonOrTextError(cmd, asJSON, 64, "edit: %v", err)
	default:
		return jsonOrTextError(cmd, asJSON, 1, "edit: %v", err)
	}
}
