// Package note implements the read half of worklog note-taking:
// timestamped journal entries in notes/<id>.md files. Writes are
// store-backed (internal/cli/note_store.go, adb-cutover M3d/M4) — this
// package now only carries the shapes and Read, which stays correct
// under either backend since write-through keeps notes/<id>.md current
// after every store-backed write.
package note

import (
	"errors"
	"os"
	"regexp"
	"strings"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
)

// Entry is one timestamped note within a notes/<id>.md file.
type Entry struct {
	Timestamp string `json:"timestamp"` // "2006-01-02 15:04" in local time
	Body      string `json:"body"`      // verbatim markdown after the heading
}

// ParseResult bundles the parsed entries with any non-entry preamble.
type ParseResult struct {
	Path     string  `json:"path"`
	Exists   bool    `json:"exists"`
	Preamble string  `json:"-"` // everything before the first timestamp heading
	Entries  []Entry `json:"entries"`
	Count    int     `json:"count"`
}

// AppendResult describes the side effects of a single Append call.
type AppendResult struct {
	ID             string `json:"id"`
	File           string `json:"file"`
	Appended       Entry  `json:"appended"`
	TotalEntries   int    `json:"totalEntries"`
	CreatedFile    bool   `json:"createdFile"`
	LinkedInWorkMD bool   `json:"linkedInWorkMD"`
}

var (
	ErrEmptyBody = errors.New("note body is required")
	ErrUnknownID = errors.New("no block with that id")
)

// timestampRE matches a note-entry heading: "## YYYY-MM-DD HH:MM".
var timestampRE = regexp.MustCompile(`^## (\d{4}-\d{2}-\d{2} \d{2}:\d{2})$`)

// Read parses notes/<id>.md and returns a structured view. If the file does not
// exist, ParseResult.Exists is false and Entries is empty (no error).
func Read(wd model.Workdir, id string) (ParseResult, error) {
	path := wd.NotesFile(id)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ParseResult{Path: path, Exists: false, Entries: []Entry{}}, nil
		}
		return ParseResult{}, err
	}

	s := strings.TrimSuffix(string(data), "\n")
	var lines []string
	if s != "" {
		lines = strings.Split(s, "\n")
	}

	var entries []Entry
	var preambleParts []string
	var curTimestamp string
	var bodyLines []string
	inEntry := false

	flushEntry := func() {
		if !inEntry {
			return
		}
		body := strings.TrimRight(strings.Join(bodyLines, "\n"), " \t\n")
		entries = append(entries, Entry{Timestamp: curTimestamp, Body: body})
		bodyLines = nil
		inEntry = false
	}

	for _, line := range lines {
		if m := timestampRE.FindStringSubmatch(line); m != nil {
			flushEntry()
			curTimestamp = m[1]
			inEntry = true
		} else if inEntry {
			bodyLines = append(bodyLines, line)
		} else {
			preambleParts = append(preambleParts, line)
		}
	}
	flushEntry()

	if entries == nil {
		entries = []Entry{}
	}

	return ParseResult{
		Path:     path,
		Exists:   true,
		Preamble: strings.Join(preambleParts, "\n"),
		Entries:  entries,
		Count:    len(entries),
	}, nil
}
