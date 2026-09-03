package feedback

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/prestontallen/ai-devboard/worklog/internal/lockfile"
	"github.com/prestontallen/ai-devboard/worklog/internal/render"
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

// Append appends a new entry to FEEDBACK.md, creating the file with its
// header if absent. The read-modify-write runs under an exclusive lock and
// the write itself is atomic via render.WriteAtomic.
//
// e.Timestamp is set to time.Now().Unix() if zero.
// e.Trigger must be non-empty; e.Excerpt and e.Context may be empty.
// e.Signal must be one of the four valid values.
func Append(path string, e Entry) (Entry, error) {
	if !IsValidSignal(string(e.Signal)) {
		return Entry{}, ErrInvalidSignal
	}
	if strings.TrimSpace(e.Trigger) == "" {
		return Entry{}, ErrEmptyTrigger
	}
	if e.Timestamp == 0 {
		e.Timestamp = time.Now().Unix()
	}

	unlock, err := acquire(path)
	if err != nil {
		return Entry{}, err
	}
	defer unlock()

	existing, err := os.ReadFile(path)
	var lines []string
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return Entry{}, fmt.Errorf("feedback: read %s: %w", path, err)
		}
		lines = []string{"# Worklog Feedback Log", ""}
	} else {
		raw := strings.TrimRight(string(existing), "\n")
		lines = strings.Split(raw, "\n")
	}

	lines = append(lines, serialize(e)...)
	lines = append(lines, "")

	if err := render.WriteAtomic(path, lines); err != nil {
		return Entry{}, err
	}
	return e, nil
}

// serialize converts an Entry into the lines that appear in FEEDBACK.md,
// without a trailing blank line (the caller adds one).
func serialize(e Entry) []string {
	out := []string{
		fmt.Sprintf("## %d — %s", e.Timestamp, e.Signal),
		fmt.Sprintf("**Trigger**: %s", e.Trigger),
	}
	if e.Excerpt != "" {
		out = append(out, "**Excerpt**:")
		for _, line := range strings.Split(e.Excerpt, "\n") {
			out = append(out, "> "+line)
		}
	}
	if e.Context != "" {
		out = append(out, fmt.Sprintf("**Context**: %s", e.Context))
	}
	if e.Resolved != 0 {
		out = append(out, fmt.Sprintf("%s%d", resolvedPrefix, e.Resolved))
	}
	return out
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

// acquire takes the exclusive lock guarding path. Both writers (Append and
// Resolve) are read-modify-write cycles: without it, two concurrent captures
// read the same file and the second write silently drops the first entry.
// The lock file sits beside FEEDBACK.md and is never unlinked.
func acquire(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("feedback: mkdir %s: %w", filepath.Dir(path), err)
	}
	return lockfile.Acquire(path + ".lock")
}

// Resolve marks the entry stamped ts as reviewed, inserting a Resolved line
// at the end of that entry's block and leaving every other byte of the file
// untouched.
//
// It returns the resolution time and whether the entry was already resolved.
// When already is true nothing is written and the returned time is the
// existing one. An unknown ts (or a missing file) yields ErrEntryNotFound.
func Resolve(path string, ts int64) (resolved int64, already bool, err error) {
	unlock, err := acquire(path)
	if err != nil {
		return 0, false, err
	}
	defer unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, false, ErrEntryNotFound
		}
		return 0, false, fmt.Errorf("feedback: read %s: %w", path, err)
	}
	// TrimSuffix, not TrimRight: entries are separated by a blank line, so
	// collapsing trailing newlines here would silently reflow the file that
	// Resolve promises to leave alone apart from the line it inserts.
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")

	// Bound the target entry: its heading, up to the next heading or EOF.
	start, end := -1, len(lines)
	for i, line := range lines {
		m := headingRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if start >= 0 {
			end = i
			break
		}
		if v, perr := strconv.ParseInt(m[1], 10, 64); perr == nil && v == ts {
			start = i
		}
	}
	if start < 0 {
		return 0, false, ErrEntryNotFound
	}

	last := start
	for i := start; i < end; i++ {
		if strings.HasPrefix(lines[i], resolvedPrefix) {
			existing, _ := strconv.ParseInt(strings.TrimPrefix(lines[i], resolvedPrefix), 10, 64)
			return existing, true, nil
		}
		if strings.TrimSpace(lines[i]) != "" {
			last = i
		}
	}

	now := time.Now().Unix()
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:last+1]...)
	out = append(out, fmt.Sprintf("%s%d", resolvedPrefix, now))
	out = append(out, lines[last+1:]...)

	if err := render.WriteAtomic(path, out); err != nil {
		return 0, false, err
	}
	return now, false, nil
}
