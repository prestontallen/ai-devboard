// Package done holds the wire types and sentinel errors for the
// `worklog done <id>` operation: archiving a completed ticket. The
// operation itself is store-backed (internal/cli/done_store.go,
// adb-cutover M3d/M4) — this package now only carries the shapes both
// the CLI and the store-backed implementation agree on.
package done

import (
	"errors"
)

// Inputs captures the user-supplied data for a `done` call.
type Inputs struct {
	ID        string
	Summary   string
	Feedback  []string
	Time      string
	PR        string
	Completed string // YYYY-MM-DD; empty = today
}

// Output is the JSON wire shape for the CLI success path.
type Output struct {
	Status          string   `json:"status"`
	Type            string   `json:"type,omitempty"` // "epic" for epic archival
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	ArchivePath     string   `json:"archivePath"`
	Completed       string   `json:"completed"`
	Parent          string   `json:"parent"`
	EpicCompletable bool     `json:"epicCompletable"`
	Warnings        []string `json:"warnings"`
}

// Sentinel errors. The CLI wrapper maps these to specific exit codes.
var (
	ErrIDNotFound          = errors.New("ticket ID not found in WORK.md")
	ErrSummaryRequired     = errors.New("summary is required")
	ErrInvalidDate         = errors.New("invalid date (expected YYYY-MM-DD)")
	ErrEpicHasOpenChildren = errors.New("epic has open children")
)
