package feedback

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/prestontallen/day2day/internal/render"
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
}

var (
	ErrInvalidSignal = errors.New("invalid signal")
	ErrEmptyTrigger  = errors.New("trigger is required")
)

var headingRE = regexp.MustCompile(`^## (\d+) — (\S+)$`)

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
// header if absent. Writes are atomic via render.WriteAtomic.
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
	return out
}

// Filter returns entries matching the (optional) signal and >= since time.
// If signal is empty, all signals match. If since is zero, all dates match.
func Filter(entries []Entry, signal Signal, since time.Time) []Entry {
	var out []Entry
	sinceUnix := since.Unix()

	for _, e := range entries {
		if signal != "" && e.Signal != signal {
			continue
		}
		if !since.IsZero() && e.Timestamp < sinceUnix {
			continue
		}
		out = append(out, e)
	}
	return out
}
