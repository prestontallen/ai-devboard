// Package search implements `worklog search <term>`: INDEX-first scan with a
// full-text fallback across WORK.md + archive/ + notes/. The CLI layer
// renders snippets through Glamour for terminals; this package returns
// structured data.
package search

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/prestontallen/day2day/internal/model"
	"github.com/prestontallen/day2day/internal/parse"
)

// QueryMode identifies how multi-term matching is applied.
type QueryMode string

const (
	ModeSingle QueryMode = "single"
	ModeAllOf  QueryMode = "all-of"
	ModeAnyOf  QueryMode = "any-of"
)

// Query describes what to search for. Terms must be non-empty, already
// lowercased and whitespace-trimmed.
type Query struct {
	Terms []string  `json:"terms"`
	Mode  QueryMode `json:"mode"`
}

// Matches reports whether haystack satisfies the query.
func (q Query) Matches(haystack string) bool {
	h := strings.ToLower(haystack)
	switch q.Mode {
	case ModeSingle:
		return strings.Contains(h, q.Terms[0])
	case ModeAllOf:
		for _, t := range q.Terms {
			if !strings.Contains(h, t) {
				return false
			}
		}
		return true
	case ModeAnyOf:
		for _, t := range q.Terms {
			if strings.Contains(h, t) {
				return true
			}
		}
		return false
	}
	return false
}

// Inputs captures the user-supplied options for a search call.
type Inputs struct {
	Query Query
	Limit int  // 0 = no limit
	Deep  bool // skip the INDEX-first pass
}

// Hit is one search result. The CLI uses Snippet to render via Glamour;
// agents use the structured fields.
type Hit struct {
	ID      string `json:"id"`
	File    string `json:"file"`
	Source  string `json:"source"`  // "index" or "fulltext"
	Kind    string `json:"kind"`    // string form of Kind from snippet.go
	Title   string `json:"title"`
	Snippet string `json:"snippet"` // joined Lines from extractSnippet
}

// Output is the structured result returned to the CLI / JSON consumer.
type Output struct {
	Query              Query `json:"query"`
	Hits               []Hit `json:"hits"`
	IndexUsed          bool  `json:"indexUsed"`
	FellBackToFullText bool  `json:"fellBackToFullText"`
	Truncated          bool  `json:"truncated"`
}

// ErrEmptyTerm is returned for queries with no terms.
var ErrEmptyTerm = errors.New("search term required")

// Run performs the search. Empty query → ErrEmptyTerm. Anything else returns
// an Output (which may have zero hits).
func Run(wd model.Workdir, in Inputs) (Output, error) {
	if len(in.Query.Terms) == 0 {
		return Output{}, ErrEmptyTerm
	}
	out := Output{Query: in.Query, Hits: []Hit{}}
	var hits []Hit

	if !in.Deep {
		out.IndexUsed = true
		idxHits, err := scanIndex(wd, in.Query)
		if err != nil {
			return out, err
		}
		for _, h := range idxHits {
			sn, err := extractSnippet(wd, h.File, h.ID)
			if err != nil {
				continue
			}
			hits = append(hits, Hit{
				ID:      h.ID,
				File:    h.File,
				Source:  "index",
				Kind:    string(sn.Kind),
				Title:   sn.Title,
				Snippet: strings.Join(sn.Lines, "\n"),
			})
		}
	}

	if len(hits) == 0 {
		// Full-text fallback. Always runs in --deep mode; otherwise only
		// when INDEX returned zero hits.
		if !in.Deep {
			out.FellBackToFullText = true
		}
		fm, err := scanFullText(wd, in.Query)
		if err != nil {
			return out, err
		}
		for _, m := range fm {
			sn, err := extractSnippet(wd, m.File, m.Anchor)
			if err != nil {
				continue
			}
			id := m.Anchor
			if id == "$file" {
				// Internal sentinel — surface as the epic ID derived from
				// the notes filename (notes/<epic-id>.md → <epic-id>).
				base := filepath.Base(m.File)
				id = strings.TrimSuffix(base, ".md")
			}
			hits = append(hits, Hit{
				ID:      id,
				File:    m.File,
				Source:  "fulltext",
				Kind:    string(sn.Kind),
				Title:   sn.Title,
				Snippet: strings.Join(sn.Lines, "\n"),
			})
		}
	}

	if in.Limit > 0 && len(hits) > in.Limit {
		hits = hits[:in.Limit]
		out.Truncated = true
	}
	if hits != nil {
		out.Hits = hits
	}
	// else: leave the zero-init empty slice so JSON emits [], not null
	return out, nil
}

// fileMatch is the result of a full-text scan: a file path plus an anchor
// (block / entry / child ID) identifying what part of the file matched.
type fileMatch struct {
	File   string
	Anchor string
}

func scanFullText(wd model.Workdir, q Query) ([]fileMatch, error) {
	var matches []fileMatch
	seen := map[string]bool{}

	add := func(m fileMatch) {
		key := m.File + "\x00" + m.Anchor
		if !seen[key] {
			seen[key] = true
			matches = append(matches, m)
		}
	}

	// 1. WORK.md (via the parser so we get block ranges + IDs).
	if doc, err := parse.File(wd.WorkMD()); err == nil {
		for _, sec := range doc.Sections {
			for _, b := range sec.Blocks {
				if b.ID == "" {
					continue
				}
				end := b.EndLine
				if end > len(doc.Lines) {
					end = len(doc.Lines)
				}
				// For multi-term queries, concatenate all block lines and check once.
				blockText := strings.Join(doc.Lines[b.StartLine-1:end], "\n")
				if q.Matches(blockText) {
					add(fileMatch{File: "WORK.md", Anchor: b.ID})
				}
			}
		}
	}

	// 2. archive/*.md
	if entries, err := os.ReadDir(wd.ArchiveDir()); err == nil {
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			rel := "archive/" + e.Name()
			abs := filepath.Join(wd.ArchiveDir(), e.Name())
			for _, m := range scanArchiveFile(abs, q, rel) {
				add(m)
			}
		}
	}

	// 3. notes/*.md
	if entries, err := os.ReadDir(wd.NotesDir()); err == nil {
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			rel := "notes/" + e.Name()
			abs := filepath.Join(wd.NotesDir(), e.Name())
			for _, m := range scanNotesFile(abs, q, rel) {
				add(m)
			}
		}
	}

	return matches, nil
}

var entryHeadingRe = regexp.MustCompile(`^### ([a-zA-Z0-9_-]+) — `)

// scanArchiveFile tracks the current `### <id>` heading and emits a
// fileMatch the first time the query matches inside that entry.
func scanArchiveFile(absPath string, q Query, relPath string) []fileMatch {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	var matches []fileMatch
	seen := map[string]bool{}
	currentAnchor := ""
	var entryLines []string

	flush := func() {
		if currentAnchor == "" || seen[currentAnchor] {
			return
		}
		if q.Matches(strings.Join(entryLines, "\n")) {
			seen[currentAnchor] = true
			matches = append(matches, fileMatch{File: relPath, Anchor: currentAnchor})
		}
	}

	for _, line := range lines {
		if m := entryHeadingRe.FindStringSubmatch(line); m != nil {
			flush()
			currentAnchor = strings.ToLower(m[1])
			entryLines = []string{line}
		} else {
			entryLines = append(entryLines, line)
		}
	}
	flush()
	return matches
}

var notesChildRe = regexp.MustCompile(`^- \[[ ~x]\]\s+([a-zA-Z0-9_-]+)`)

// scanNotesFile emits a per-child match for every checkbox line matching the
// query. If no checkbox line matches but the file content does, it emits a
// single file-level match with the sentinel anchor `$file`.
func scanNotesFile(absPath string, q Query, relPath string) []fileMatch {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil
	}
	body := string(data)
	lines := strings.Split(body, "\n")
	var matches []fileMatch
	seen := map[string]bool{}
	for _, line := range lines {
		m := notesChildRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if !q.Matches(line) {
			continue
		}
		anchor := strings.ToLower(m[1])
		if !seen[anchor] {
			seen[anchor] = true
			matches = append(matches, fileMatch{File: relPath, Anchor: anchor})
		}
	}
	if len(matches) == 0 && q.Matches(body) {
		matches = append(matches, fileMatch{File: relPath, Anchor: "$file"})
	}
	return matches
}
