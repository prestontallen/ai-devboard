// Package wait holds the wire types and sentinel errors for the worklog
// wait/resume operations (moving a ticket between ## Now and ## Waiting).
// The operations themselves are store-backed (internal/cli/wait_store.go
// and start_store.go's resume-from-Waiting fast path, adb-cutover
// M3d/M4) — this package now only carries the shapes both the CLI and
// the store-backed implementation agree on.
package wait

import "errors"

var (
	ErrIDNotFound     = errors.New("ticket ID not found")
	ErrNotInNow       = errors.New("ticket is not in ## Now")
	ErrAlreadyWaiting = errors.New("ticket is already in ## Waiting")
	ErrCapExceeded    = errors.New("## Now is at cap")
	ErrNotInWaiting   = errors.New("ticket is not in ## Waiting")
)

// WaitOutput is the JSON wire shape for a successful Wait call.
type WaitOutput struct {
	Status       string `json:"status"`
	ID           string `json:"id"`
	WaitingSince string `json:"waitingSince"`
	WorkMD       string `json:"workMD"`
}

// ResumeOutput is the JSON wire shape for a successful Resume call.
type ResumeOutput struct {
	Status  string `json:"status"`
	ID      string `json:"id"`
	Section string `json:"section"`
	WorkMD  string `json:"workMD"`
}
