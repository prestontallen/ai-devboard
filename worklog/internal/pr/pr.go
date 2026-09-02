// Package pr implements the `worklog pr <id>` operation: reading or updating
// the optional **PR**: field on a ticket block. Both the CLI subcommand and
// the TUI keybinding go through SetPR so the on-disk write path is identical.
package pr

import (
	"errors"
	"fmt"
	"strings"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/parse"
	"github.com/prestontallen/ai-devboard/worklog/internal/render"
)

// Result is the JSON wire shape for the CLI success path.
type Result struct {
	ID       string `json:"id"`
	PR       string `json:"pr"`
	Previous string `json:"previous"`
	Parent   string `json:"parent,omitempty"` // non-empty when id is a child of an epic
}

// ErrIDNotFound is returned when blockID does not resolve to a ticket in
// WORK.md.
var ErrIDNotFound = errors.New("ticket ID not found in WORK.md")

// Get returns the current PR value for blockID without mutating any file.
func Get(wd model.Workdir, blockID string) (Result, error) {
	doc, err := parse.File(wd.WorkMD())
	if err != nil {
		return Result{}, err
	}
	b := doc.BlockByID(blockID)
	if b == nil {
		return Result{}, fmt.Errorf("%w: %q", ErrIDNotFound, blockID)
	}
	return Result{ID: b.ID, PR: b.PR, Previous: b.PR, Parent: b.Parent}, nil
}

// SetPR writes value as the new PR on blockID's ticket block and returns the
// before/after pair. value may be empty — the rendered line is preserved as
// `  - **PR**: ` (trailing space, no value) so the field stays visible.
func SetPR(wd model.Workdir, blockID, value string) (Result, error) {
	doc, err := parse.File(wd.WorkMD())
	if err != nil {
		return Result{}, err
	}
	b := doc.BlockByID(blockID)
	if b == nil {
		return Result{}, fmt.Errorf("%w: %q", ErrIDNotFound, blockID)
	}
	previous := b.PR
	value = strings.TrimSpace(value)

	newLines, err := render.SetBlockPR(doc, blockID, value)
	if err != nil {
		return Result{}, err
	}
	if err := render.WriteAtomic(wd.WorkMD(), newLines); err != nil {
		return Result{}, fmt.Errorf("write WORK.md: %w", err)
	}
	return Result{ID: b.ID, PR: value, Previous: previous, Parent: b.Parent}, nil
}
