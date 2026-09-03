// Package feedback holds the parsing/filtering half of FEEDBACK.md.
// Writes are store-backed (internal/cli/feedback_store.go, adb-cutover
// M3d/M4) — this package now only carries the shapes and the read path
// both the CLI and the store-backed implementation agree on.
package feedback

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Signal enumerates the four friction signals the agent watches for.
type Signal string

const (
	SignalMissingFeature   Signal = "missing-feature"
	SignalTUIError         Signal = "tui-error"
	SignalProfanity        Signal = "profanity"
	SignalAgentFrustration Signal = "agent-frustration"
)

// AllSignals returns the valid set, ordered as documented in SKILL.md.
func AllSignals() []Signal {
	return []Signal{SignalMissingFeature, SignalTUIError, SignalProfanity, SignalAgentFrustration}
}

// IsValidSignal returns true iff s is one of the four enum values.
func IsValidSignal(s string) bool {
	for _, sig := range AllSignals() {
		if string(sig) == s {
			return true
		}
	}
	return false
}

// Entry is one feedback record.
//
// Timestamp is unix seconds — chosen so headings stay short and the
// format is consistent with other shell tooling.
type Entry struct {
	Timestamp int64  `json:"timestamp"`
	Signal    Signal `json:"signal"`
	Trigger   string `json:"trigger"` // one-line summary
	Excerpt   string `json:"excerpt"` // verbatim user/assistant exchange
	Context   string `json:"context"` // dispatcher's context note

	// Resolved is the unix time the human marked this entry reviewed, or 0
	// while it is still outstanding. Written by Resolve, never by Append.
	Resolved int64 `json:"resolved,omitempty"`
}

var (
	ErrInvalidSignal = errors.New("invalid signal")
	ErrEmptyTrigger  = errors.New("trigger is required")
	ErrEntryNotFound = errors.New("no entry with that timestamp")
)

var headingRE = regexp.MustCompile(`^## (\d+) — (\S+)$`)

// resolvedPrefix marks an entry the human has reviewed. It is written last
// in an entry's block so appending it never disturbs the lines above.
const resolvedPrefix = "**Resolved**: "

// Parse reads FEEDBACK.md and returns all entries in file order (oldest first).
// Returns (nil, nil) if the file does not exist.
func Parse(path string) ([]Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("feedback: read %s: %w", path, err)
	}
	return ParseBytes(data)
}

// ParseBytes parses FEEDBACK.md content already in memory.
func ParseBytes(data []byte) ([]Entry, error) {
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")

	var entries []Entry
	var cur *Entry
	inExcerpt := false

	flush := func() {
		if cur != nil {
			cur.Excerpt = strings.TrimSpace(cur.Excerpt)
			entries = append(entries, *cur)
			cur = nil
		}
	}

	for _, line := range lines {
		if m := headingRE.FindStringSubmatch(line); m != nil {
			flush()
			inExcerpt = false
			ts, _ := strconv.ParseInt(m[1], 10, 64)
			cur = &Entry{
				Timestamp: ts,
				Signal:    Signal(m[2]),
			}
			continue
		}
		if cur == nil {
			continue
		}

		if strings.HasPrefix(line, "**Trigger**: ") {
			cur.Trigger = strings.TrimPrefix(line, "**Trigger**: ")
			inExcerpt = false
			continue
		}
		if line == "**Excerpt**:" {
			inExcerpt = true
			continue
		}
		if strings.HasPrefix(line, "**Context**: ") {
			cur.Context = strings.TrimPrefix(line, "**Context**: ")
			inExcerpt = false
			continue
		}
		if strings.HasPrefix(line, resolvedPrefix) {
			cur.Resolved, _ = strconv.ParseInt(strings.TrimPrefix(line, resolvedPrefix), 10, 64)
			inExcerpt = false
			continue
		}
		if strings.HasPrefix(line, "**") {
			// Unknown field: skip the line, keep the entry — and end any
			// excerpt, so a future field never leaks into excerpt text.
			inExcerpt = false
			continue
		}

		if inExcerpt {
			quotedLine := strings.TrimPrefix(line, "> ")
			if cur.Excerpt != "" {
				cur.Excerpt += "\n"
			}
			cur.Excerpt += quotedLine
		}
	}
	flush()

	return entries, nil
}

// Filter returns entries matching every constraint given — the filters AND
// together. An empty signal matches all signals, a zero since matches all
// dates, and unresolvedOnly=false matches regardless of review state.
func Filter(entries []Entry, signal Signal, since time.Time, unresolvedOnly bool) []Entry {
	var out []Entry
	sinceUnix := since.Unix()

	for _, e := range entries {
		if signal != "" && e.Signal != signal {
			continue
		}
		if !since.IsZero() && e.Timestamp < sinceUnix {
			continue
		}
		if unresolvedOnly && e.Resolved != 0 {
			continue
		}
		out = append(out, e)
	}
	return out
}
