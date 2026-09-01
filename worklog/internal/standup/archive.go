package standup

import (
	"os"
	"regexp"
	"strings"
)

// Entry is a fuller archive entry than reindex.ArchiveEntry. It carries
// the fields the standup report needs: Started, PR, Summary, in addition
// to ID/Title/Repo/Parent/Completed.
type Entry struct {
	ID         string
	Title      string
	Repo       string
	Parent     string
	PR         string
	Type       string // "epic" for archived epics
	Started    string // YYYY-MM-DD (left side of the arrow)
	Completed  string // YYYY-MM-DD (right side of the arrow)
	Summary    string // single line; empty if absent
	SourceFile string // e.g. "archive/2026-05.md"
}

var (
	headingRe   = regexp.MustCompile(`^### ([a-zA-Z0-9_-]+) — (.+)$`)
	metaRe      = regexp.MustCompile(`^- \*\*(.+?)\*\*:\s*(.*)$`)
	completedRe = regexp.MustCompile(`(\d{4}-\d{2}-\d{2})\s*→\s*(\d{4}-\d{2}-\d{2})`)
	bareDateRe  = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

// ParseFile reads one archive month-file and returns entries in document
// order. sourcePath is recorded into each entry's SourceFile.
func ParseFile(absPath, sourcePath string) ([]Entry, error) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}

	var entries []Entry
	var cur *Entry

	flush := func() {
		if cur == nil {
			return
		}
		entries = append(entries, *cur)
		cur = nil
	}

	for _, line := range strings.Split(string(data), "\n") {
		if m := headingRe.FindStringSubmatch(line); m != nil {
			flush()
			cur = &Entry{
				ID:         strings.ToLower(m[1]),
				Title:      strings.TrimSpace(m[2]),
				SourceFile: sourcePath,
			}
			continue
		}
		if cur == nil {
			continue
		}
		m := metaRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		field := strings.ToLower(strings.TrimSpace(m[1]))
		value := strings.TrimSpace(m[2])
		switch field {
		case "repo":
			cur.Repo = value
		case "parent":
			cur.Parent = strings.ToLower(value)
		case "pr":
			cur.PR = value
		case "started → completed":
			if cm := completedRe.FindStringSubmatch(value); cm != nil {
				cur.Started = cm[1]
				cur.Completed = cm[2]
			}
		case "completed": // Completed-only date line (epic entries)
			if bareDateRe.MatchString(value) {
				cur.Completed = value
			}
		case "type":
			cur.Type = strings.ToLower(value)
		case "summary":
			cur.Summary = value
		}
	}
	flush()
	return entries, nil
}
