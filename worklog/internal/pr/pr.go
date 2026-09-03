// Package pr implements the read half of `worklog pr <id>`: the optional
// **PR**: field on a ticket block. Writes are store-backed
// (internal/cli/pr_store.go, adb-cutover M3d/M4) — write-through keeps
// WORK.md current, so this package's read path is correct under either
// backend and never needed porting.
package pr

import (
	"errors"
	"fmt"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/parse"
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
