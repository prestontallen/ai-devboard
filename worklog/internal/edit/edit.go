// Package edit holds the wire types and sentinel errors for
// `worklog edit <id>`: writing metadata fields on a ticket that already
// exists. Writes are store-backed (internal/cli/edit_store.go,
// adb-cutover M3d/M4) — this package now only carries the shapes both
// the CLI and the store-backed implementation agree on.
//
// Every other writer in the corpus owns exactly one field at one moment in a
// ticket's life (start stamps Started, wait stamps Waiting since, pr owns PR).
// edit is the general setter for the fields nothing else claims, so agents
// don't have to reach for a text editor to correct a ticket after the fact.
package edit

import (
	"errors"
)

// TitleField is the pseudo-field name for a block's title. It lives on the
// bullet line rather than an indented metadata line, so it takes a different
// write path from everything in render's field table.
const TitleField = "Title"

var (
	// ErrIDNotFound is returned when blockID doesn't resolve to a block.
	ErrIDNotFound = errors.New("ticket ID not found in WORK.md")
	// ErrNoFields is returned when no field was requested.
	ErrNoFields = errors.New("no fields given")
	// ErrNotEditable is returned for a field edit doesn't own.
	ErrNotEditable = errors.New("field is not editable")
	// ErrEmptyTitle is returned for an attempt to clear a title.
	ErrEmptyTitle = errors.New("title cannot be empty")
)

// Assignment is one field write. An empty Value removes the metadata line.
type Assignment struct {
	Field string
	Value string
}

// Change is one applied write, before and after.
type Change struct {
	Field string `json:"field"`
	From  string `json:"from"`
	To    string `json:"to"`
}

// Result is the JSON wire shape for the CLI success path.
type Result struct {
	ID      string   `json:"id"`
	Changes []Change `json:"changes"`
}
