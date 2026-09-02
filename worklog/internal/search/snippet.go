package search

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/parse"
)

// Kind labels the source-shape of a snippet so the CLI / agent can present
// hits with appropriate context.
type Kind string

const (
	KindLive      Kind = "live"      // a block in WORK.md
	KindEpic      Kind = "epic"      // an epic block in WORK.md
	KindArchived  Kind = "archived"  // an entry in an archive/YYYY-MM.md
	KindChild     Kind = "child"     // a `- [ ]` line in a notes/<id>.md
	KindNotesFile Kind = "notesFile" // a whole notes/<id>.md (no specific child anchor)
)

// Snippet is the rendered content for a single hit.
type Snippet struct {
	Lines []string
	Kind  Kind
	Title string
}

// extractSnippet picks the appropriate extractor based on file path. The
// anchor's meaning depends on the file kind: for WORK.md and notes files it
// is a lowercase block / child ID; for archive files it is the `### <id>`
// heading. The sentinel anchor `$file` returns a whole notes file.
func extractSnippet(wd model.Workdir, file, anchor string) (Snippet, error) {
	if file == "WORK.md" {
		return extractFromWorkMD(wd.WorkMD(), anchor)
	}
	abs := filepath.Join(wd.Root, file)
	switch {
	case strings.HasPrefix(file, "archive/"):
		return extractFromArchive(abs, anchor)
	case strings.HasPrefix(file, "notes/"):
		if anchor == "$file" {
			return extractWholeNotes(abs)
		}
		return extractFromNotes(abs, anchor)
	}
	return Snippet{}, fmt.Errorf("snippet: unknown file kind: %s", file)
}

func extractFromWorkMD(workMDPath, anchor string) (Snippet, error) {
	doc, err := parse.File(workMDPath)
	if err != nil {
		return Snippet{}, err
	}
	b := doc.BlockByID(anchor)
	if b == nil {
		return Snippet{}, fmt.Errorf("snippet: anchor %q not found in WORK.md", anchor)
	}
	// 1-indexed [StartLine, EndLine] → 0-indexed [StartLine-1, EndLine).
	end := b.EndLine
	if end > len(doc.Lines) {
		end = len(doc.Lines)
	}
	lines := append([]string(nil), doc.Lines[b.StartLine-1:end]...)
	// Trim trailing blank lines for a cleaner Glamour render.
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	kind := KindLive
	if b.IsEpic() {
		kind = KindEpic
	}
	return Snippet{Lines: lines, Kind: kind, Title: b.Title}, nil
}

var archiveHeadingRe = regexp.MustCompile(`^### ([a-zA-Z0-9_-]+) — (.+)$`)

func extractFromArchive(path, anchor string) (Snippet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Snippet{}, err
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	start := -1
	var title string
	for i, line := range lines {
		m := archiveHeadingRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if strings.EqualFold(m[1], anchor) {
			start = i
			title = strings.TrimSpace(m[2])
			break
		}
	}
	if start < 0 {
		return Snippet{}, fmt.Errorf("snippet: anchor %q not found in %s", anchor, path)
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "### ") || strings.HasPrefix(lines[i], "## ") {
			end = i
			break
		}
	}
	for end > start+1 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return Snippet{
		Lines: append([]string(nil), lines[start:end]...),
		Kind:  KindArchived,
		Title: title,
	}, nil
}

var notesChildLineRe = regexp.MustCompile(`^- \[[ ~x]\]\s+([a-zA-Z0-9_-]+)(?::\s*(.*))?$`)

func extractFromNotes(path, anchor string) (Snippet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Snippet{}, err
	}
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		m := notesChildLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if !strings.EqualFold(m[1], anchor) {
			continue
		}
		title := ""
		if len(m) > 2 {
			title = strings.TrimSpace(m[2])
		}
		return Snippet{
			Lines: []string{line},
			Kind:  KindChild,
			Title: title,
		}, nil
	}
	return Snippet{}, fmt.Errorf("snippet: anchor %q not found in %s", anchor, path)
}

func extractWholeNotes(path string) (Snippet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Snippet{}, err
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	title := strings.TrimSuffix(filepath.Base(path), ".md")
	if len(lines) > 0 && strings.HasPrefix(lines[0], "# ") {
		title = strings.TrimSpace(strings.TrimPrefix(lines[0], "# "))
	}
	return Snippet{
		Lines: lines,
		Kind:  KindNotesFile,
		Title: title,
	}, nil
}
