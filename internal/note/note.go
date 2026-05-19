// Package note implements worklog note-taking: reading and appending
// timestamped journal entries to notes/<id>.md files.
package note

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/prestontallen/day2day/internal/model"
	"github.com/prestontallen/day2day/internal/parse"
	"github.com/prestontallen/day2day/internal/render"
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

// Append adds one entry to notes/<id>.md, creating the file with a
// "# Notes — <id>" header if missing. For non-epic tickets that have not yet
// linked a notes file, also updates the WORK.md block's **Notes**: field.
// body is trimmed; empty body → ErrEmptyBody. Unknown id → ErrUnknownID.
// now is injected so callers can write deterministic tests.
func Append(wd model.Workdir, id, body string, now time.Time) (AppendResult, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return AppendResult{}, ErrEmptyBody
	}

	doc, err := parse.File(wd.WorkMD())
	if err != nil {
		return AppendResult{}, err
	}
	b := doc.BlockByID(id)
	if b == nil {
		return AppendResult{}, fmt.Errorf("%w: %q", ErrUnknownID, id)
	}

	path := wd.NotesFile(id)
	timestamp := now.Local().Format("2006-01-02 15:04")
	entry := Entry{Timestamp: timestamp, Body: body}

	existingData, readErr := os.ReadFile(path)
	createdFile := false
	var newLines []string

	bodyLines := strings.Split(body, "\n")

	if readErr != nil {
		if !os.IsNotExist(readErr) {
			return AppendResult{}, fmt.Errorf("read notes: %w", readErr)
		}
		createdFile = true
		newLines = []string{"# Notes — " + id, ""}
		newLines = append(newLines, "## "+timestamp)
		newLines = append(newLines, bodyLines...)
	} else {
		s := strings.TrimSuffix(string(existingData), "\n")
		var existing []string
		if s != "" {
			existing = strings.Split(s, "\n")
		}
		for len(existing) > 0 && strings.TrimSpace(existing[len(existing)-1]) == "" {
			existing = existing[:len(existing)-1]
		}
		existing = append(existing, "", "## "+timestamp)
		existing = append(existing, bodyLines...)
		newLines = existing
	}

	if err := os.MkdirAll(wd.NotesDir(), 0o755); err != nil {
		return AppendResult{}, fmt.Errorf("mkdir notes: %w", err)
	}
	if err := render.WriteAtomic(path, newLines); err != nil {
		return AppendResult{}, fmt.Errorf("write notes: %w", err)
	}

	totalEntries := 0
	for _, line := range newLines {
		if timestampRE.MatchString(line) {
			totalEntries++
		}
	}

	linkedInWorkMD := false
	if b.NotesRef == "" {
		notesRef := "notes/" + strings.ToLower(id) + ".md"
		newWORKLines, err := render.SetBlockNotesRef(doc, id, notesRef)
		if err != nil {
			return AppendResult{}, fmt.Errorf("set notes ref: %w", err)
		}
		if err := render.WriteAtomic(wd.WorkMD(), newWORKLines); err != nil {
			return AppendResult{}, fmt.Errorf("write WORK.md: %w", err)
		}
		linkedInWorkMD = true
	}

	return AppendResult{
		ID:             id,
		File:           path,
		Appended:       entry,
		TotalEntries:   totalEntries,
		CreatedFile:    createdFile,
		LinkedInWorkMD: linkedInWorkMD,
	}, nil
}
