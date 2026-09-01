package reindex

import (
	"os"
	"regexp"
	"strings"
)

// ArchiveEntry is the minimal metadata reindex pulls out of an archive
// YYYY-MM.md file. Fields that the on-disk entry doesn't carry (e.g. PR,
// Files, Time, Summary, Feedback) are intentionally omitted — reindex only
// cares about what feeds the four INDEX.md sections.
type ArchiveEntry struct {
	ID         string
	Title      string
	Repo       string
	Tags       []string
	Parent     string
	Type       string // "epic" for archived epics; empty for tickets
	NotesRef   string // retained notes file, e.g. `notes/<id>.md` (epics)
	Completed  string // YYYY-MM-DD
	SourceFile string // repo-relative path, e.g. `archive/2026-05.md`
}

// IsEpic reports whether the entry is an archived epic.
func (e ArchiveEntry) IsEpic() bool { return e.Type == "epic" }

var (
	archiveEntryHeadingRe = regexp.MustCompile(`^### ([a-zA-Z0-9_-]+) — (.+)$`)
	archiveMetaRe         = regexp.MustCompile(`^- \*\*(.+?)\*\*:\s*(.*)$`)
	completedRe           = regexp.MustCompile(`(\d{4}-\d{2}-\d{2})\s*→\s*(\d{4}-\d{2}-\d{2})`)
	bareDateRe            = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

// ParseArchive reads an archive month-file and returns its entries in
// document order. sourcePath is the repo-relative path used to populate
// each entry's SourceFile (e.g. `archive/2026-05.md`).
func ParseArchive(absPath, sourcePath string) ([]ArchiveEntry, error) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}

	var entries []ArchiveEntry
	var cur *ArchiveEntry

	flush := func() {
		if cur == nil {
			return
		}
		entries = append(entries, *cur)
		cur = nil
	}

	for _, line := range strings.Split(string(data), "\n") {
		if m := archiveEntryHeadingRe.FindStringSubmatch(line); m != nil {
			flush()
			cur = &ArchiveEntry{
				ID:         strings.ToLower(m[1]),
				Title:      strings.TrimSpace(m[2]),
				SourceFile: sourcePath,
			}
			continue
		}
		if cur == nil {
			continue
		}
		m := archiveMetaRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		field := strings.ToLower(strings.TrimSpace(m[1]))
		value := strings.TrimSpace(m[2])
		switch field {
		case "repo":
			cur.Repo = value
		case "tags":
			cur.Tags = splitCSVLower(value)
		case "parent":
			cur.Parent = strings.ToLower(value)
		case "started → completed":
			if cm := completedRe.FindStringSubmatch(value); cm != nil {
				cur.Completed = cm[2]
			}
		case "completed": // Completed-only date line (epic entries)
			if bareDateRe.MatchString(value) {
				cur.Completed = value
			}
		case "type":
			cur.Type = strings.ToLower(value)
		case "notes":
			cur.NotesRef = value
		}
	}
	flush()
	return entries, nil
}

// FindEntry scans every archive month file for the entry with the given id
// (case-insensitive). Used by callers that need to distinguish "archived"
// from "never existed" in error messages.
func FindEntry(archiveDir, id string) (ArchiveEntry, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	dirents, err := os.ReadDir(archiveDir)
	if err != nil {
		return ArchiveEntry{}, false
	}
	for _, d := range dirents {
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			continue
		}
		entries, err := ParseArchive(archiveDir+string(os.PathSeparator)+d.Name(), "archive/"+d.Name())
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.ID == id {
				return e, true
			}
		}
	}
	return ArchiveEntry{}, false
}

// ArchivedHint returns a human hint like "archived on 2026-09-01 (see
// archive/2026-09.md)" when id has an archive entry, else "".
func ArchivedHint(archiveDir, id string) string {
	e, ok := FindEntry(archiveDir, id)
	if !ok {
		return ""
	}
	return "archived on " + e.Completed + " (see " + e.SourceFile + ")"
}

func splitCSVLower(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
