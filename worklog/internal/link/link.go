// Package link implements the read half of `worklog link <id> [name]`:
// listing or reading a ticket's arbitrarily-named **Link**: entries —
// Jira, Slack threads, design docs, anything that isn't the PR (worklog
// pr owns that field separately; see internal/pr). Writes are
// store-backed (internal/cli/link_store.go, adb-cutover M3d/M4).
package link

import (
	"errors"
	"fmt"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/parse"
)

// Result is the JSON wire shape for a single-link CLI operation.
type Result struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	Previous string `json:"previous"`
	Parent   string `json:"parent,omitempty"` // non-empty when id is a child of an epic
}

// ListResult is the JSON wire shape for `worklog link <id>` (no name).
type ListResult struct {
	ID     string            `json:"id"`
	Links  []model.LinkEntry `json:"links"`
	Parent string            `json:"parent,omitempty"`
}

// ErrIDNotFound is returned when blockID does not resolve to a ticket in
// WORK.md.
var ErrIDNotFound = errors.New("ticket ID not found in WORK.md")

// Get returns the current value of the named link on blockID without
// mutating any file.
func Get(wd model.Workdir, blockID, name string) (Result, error) {
	doc, err := parse.File(wd.WorkMD())
	if err != nil {
		return Result{}, err
	}
	b := doc.BlockByID(blockID)
	if b == nil {
		return Result{}, fmt.Errorf("%w: %q", ErrIDNotFound, blockID)
	}
	url := ""
	if l, ok := b.LinkByName(name); ok {
		url = l.URL
	}
	return Result{ID: b.ID, Name: name, URL: url, Previous: url, Parent: b.Parent}, nil
}

// List returns every link on blockID without mutating any file.
func List(wd model.Workdir, blockID string) (ListResult, error) {
	doc, err := parse.File(wd.WorkMD())
	if err != nil {
		return ListResult{}, err
	}
	b := doc.BlockByID(blockID)
	if b == nil {
		return ListResult{}, fmt.Errorf("%w: %q", ErrIDNotFound, blockID)
	}
	return ListResult{ID: b.ID, Links: b.Links, Parent: b.Parent}, nil
}
