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

// Inputs captures the user-supplied options for a search call.
type Inputs struct {
	Term  string
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
	Term               string `json:"term"`
	Hits               []Hit  `json:"hits"`
	IndexUsed          bool   `json:"indexUsed"`
	FellBackToFullText bool   `json:"fellBackToFullText"`
	Truncated          bool   `json:"truncated"`
}

// ErrEmptyTerm is returned for blank-term queries.
var ErrEmptyTerm = errors.New("search term required")

// Run performs the search. Empty term → ErrEmptyTerm. Anything else returns
// an Output (which may have zero hits).
func Run(wd model.Workdir, in Inputs) (Output, error) {
	term := strings.TrimSpace(in.Term)
	if term == "" {
		return Output{}, ErrEmptyTerm
	}
	out := Output{Term: term, Hits: []Hit{}}
	var hits []Hit

	if !in.Deep {
		out.IndexUsed = true
		idxHits, err := scanIndex(wd, term)
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
		fm, err := scanFullText(wd, term)
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

func scanFullText(wd model.Workdir, term string) ([]fileMatch, error) {
	needle := strings.ToLower(term)
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
				for i := b.StartLine - 1; i < end; i++ {
					if strings.Contains(strings.ToLower(doc.Lines[i]), needle) {
						add(fileMatch{File: "WORK.md", Anchor: b.ID})
						break
					}
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
			for _, m := range scanArchiveFile(abs, needle, rel) {
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
			for _, m := range scanNotesFile(abs, needle, rel) {
				add(m)
			}
		}
	}

	return matches, nil
}

var entryHeadingRe = regexp.MustCompile(`^### ([a-zA-Z0-9_-]+) — `)

// scanArchiveFile tracks the current `### <id>` heading and emits a
// fileMatch the first time the needle is seen inside that entry.
func scanArchiveFile(absPath, needle, relPath string) []fileMatch {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	var matches []fileMatch
	seen := map[string]bool{}
	currentAnchor := ""
	for _, line := range lines {
		if m := entryHeadingRe.FindStringSubmatch(line); m != nil {
			currentAnchor = strings.ToLower(m[1])
		}
		if currentAnchor == "" {
			continue
		}
		if strings.Contains(strings.ToLower(line), needle) {
			if !seen[currentAnchor] {
				seen[currentAnchor] = true
				matches = append(matches, fileMatch{File: relPath, Anchor: currentAnchor})
			}
		}
	}
	return matches
}

var notesChildRe = regexp.MustCompile(`^- \[[ ~x]\]\s+([a-zA-Z0-9_-]+)`)

// scanNotesFile emits a per-child match for every checkbox line containing
// the needle. If no checkbox line matches but the file content does, it
// emits a single file-level match with the sentinel anchor `$file`.
func scanNotesFile(absPath, needle, relPath string) []fileMatch {
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
		if !strings.Contains(strings.ToLower(line), needle) {
			continue
		}
		anchor := strings.ToLower(m[1])
		if !seen[anchor] {
			seen[anchor] = true
			matches = append(matches, fileMatch{File: relPath, Anchor: anchor})
		}
	}
	if len(matches) == 0 && strings.Contains(strings.ToLower(body), needle) {
		matches = append(matches, fileMatch{File: relPath, Anchor: "$file"})
	}
	return matches
}
