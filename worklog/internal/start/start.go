// Package start holds the wire types and sentinel errors for the
// `worklog start <id>` operation: promoting a ticket into ## Now. The
// operation itself is store-backed (internal/cli/start_store.go,
// adb-cutover M3d/M4) — this package now only carries the shapes both
// the CLI and the store-backed implementation agree on.
package start

import "errors"

// Inputs captures the user's intent for a start call.
type Inputs struct {
	ID         string
	Repo       string
	Tags       []string
	Acceptance string
}

// Output is the JSON wire shape for the CLI success path.
type Output struct {
	Status  string `json:"status"`
	ID      string `json:"id"`
	Title   string `json:"title"`
	Section string `json:"section"`
	Parent  string `json:"parent"`
	// Type is the ticket's block type ("spike", "chore"); empty for an
	// ordinary ticket. Surfaced so the CLI can mirror it to devboard
	// without re-parsing WORK.md.
	Type string `json:"type,omitempty"`
	// Repo is the ticket's declared **Repo**: value. Surfaced for the same
	// reason as Type: the CLI mirrors it to devboard without re-parsing.
	Repo     string   `json:"repo,omitempty"`
	Started  string   `json:"started"`
	WorkMD   string   `json:"workMD"`
	Warnings []string `json:"warnings"`
}

// Sentinel errors. The CLI wrapper maps these to specific exit codes and
// messages; the orchestration layer never speaks UI.
var (
	ErrIDNotFound      = errors.New("ticket ID not found")
	ErrCapExceeded     = errors.New("## Now is at cap")
	ErrAlreadyStarted  = errors.New("ticket is already in ## Now")
	ErrEpicCannotStart = errors.New("epics do not occupy ## Now")
	// ErrParentEpicGone: the child's parent epic is not in WORK.md
	// (typically archived).
	ErrParentEpicGone = errors.New("parent epic not in WORK.md")
)

// IndexNotUpdatedWarning is exported so the CLI can tell it apart from other
// warnings when deciding which follow-up hint to print.
const IndexNotUpdatedWarning = "INDEX.md not updated (deferred to Phase 2B)"
