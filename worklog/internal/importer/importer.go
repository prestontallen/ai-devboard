// Package importer holds the wire types and JSON decoding for
// `worklog import`. Writes are store-backed (internal/cli/import_store.go,
// adb-cutover M3d/M4) — this package now only carries the shapes both the
// CLI and the store-backed implementation agree on, plus Decode.
package importer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

func decodeJSON(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

// Ticket is one ticket to import.
type Ticket struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Type    string   `json:"type,omitempty"`
	Parent  string   `json:"parent,omitempty"`
	Repo    string   `json:"repo,omitempty"`
	Tags    []string `json:"tags,omitempty"`
	PR      string   `json:"pr,omitempty"`
	Section string   `json:"section,omitempty"` // now/next/someday
	Source  string   `json:"source,omitempty"`
}

// Imported describes one successfully-imported ticket.
type Imported struct {
	ID      string `json:"id"`
	Section string `json:"section"`
}

// Failed describes one ticket that failed to import.
type Failed struct {
	Index  int    `json:"index"`
	ID     string `json:"id,omitempty"`
	Reason string `json:"reason"`
}

// Result bundles per-ticket outcomes.
type Result struct {
	Imported []Imported `json:"imported"`
	Failed   []Failed   `json:"failed"`
}

// Options bundles per-call overrides.
type Options struct {
	SectionOverride string    // overrides every ticket's Section when non-empty
	DryRun          bool      // validate without writing
	Now             time.Time // injected for deterministic tests
}

var (
	ErrIDRequired     = errors.New("id is required")
	ErrTitleRequired  = errors.New("title is required")
	ErrInvalidSection = errors.New("section must be one of: now, next, someday")
	ErrInvalidType    = errors.New("type must be one of: ticket, epic, spike, chore")
	ErrParentMissing  = errors.New("parent epic not found in WORK.md")
	ErrParentNotEpic  = errors.New("parent block is not an epic")
	ErrAlreadyExists  = errors.New("ticket id already exists in WORK.md")
)

// Decode reads JSON from r. Accepts a single object or an array.
func Decode(r io.Reader) ([]Ticket, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, nil
	}
	switch trimmed[0] {
	case '[':
		var tickets []Ticket
		if err := decodeJSON(trimmed, &tickets); err != nil {
			return nil, fmt.Errorf("expected JSON object or array: %w", err)
		}
		return tickets, nil
	case '{':
		var t Ticket
		if err := decodeJSON(trimmed, &t); err != nil {
			return nil, fmt.Errorf("expected JSON object or array: %w", err)
		}
		return []Ticket{t}, nil
	default:
		return nil, fmt.Errorf("expected JSON object or array")
	}
}
