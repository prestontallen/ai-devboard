package search

import (
	"errors"
	"os"
	"regexp"
	"strings"

	"github.com/prestontallen/day2day/internal/model"
)

// indexHit is a single resolved INDEX.md match — an ID plus the file it
// points at (derived from the "By ticket" section).
type indexHit struct {
	ID   string
	File string
}

var (
	byTicketLineRe = regexp.MustCompile(`^- ([a-zA-Z0-9_-]+) → (\S+)`)
	byKeyLineRe    = regexp.MustCompile(`^- ([^:]+):\s*(.+)$`)
)

// scanIndex reads INDEX.md and returns deduplicated indexHits whose
// originating lines satisfy the query. Algorithm:
//
//  1. First pass: build an ID → file map from "By ticket" section lines.
//  2. Second pass: walk every line. For each line that matches the query,
//     collect candidate IDs:
//     - "By ticket" lines contribute their ID directly.
//     - "By tag" / "By repo" lines contribute every ID listed after the
//       colon (these are how tag and repo searches reach the file pointers).
//  3. For each candidate ID, look it up in the ID → file map and emit an
//     indexHit. Skip candidates whose ID isn't in the map (defensive).
//
// Missing INDEX.md is not an error — returns nil, nil.
func scanIndex(wd model.Workdir, q Query) ([]indexHit, error) {
	data, err := os.ReadFile(wd.IndexMD())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	lines := strings.Split(string(data), "\n")

	// Pass 1: ID → file
	byTicket := map[string]string{}
	section := ""
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			section = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			continue
		}
		if section == "By ticket" {
			if m := byTicketLineRe.FindStringSubmatch(line); m != nil {
				byTicket[strings.ToLower(m[1])] = m[2]
			}
		}
	}

	// Pass 2: collect candidate IDs from matching lines
	var hits []indexHit
	seen := map[string]bool{}
	section = ""
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			section = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			continue
		}
		if !q.Matches(line) {
			continue
		}

		var candidates []string
		switch section {
		case "By ticket":
			if m := byTicketLineRe.FindStringSubmatch(line); m != nil {
				candidates = append(candidates, strings.ToLower(m[1]))
			}
		case "By tag", "By repo":
			if m := byKeyLineRe.FindStringSubmatch(line); m != nil {
				for _, id := range strings.Split(m[2], ",") {
					id = strings.TrimSpace(id)
					if id != "" {
						candidates = append(candidates, strings.ToLower(id))
					}
				}
			}
		}
		// "By archive month" lines don't list individual ticket IDs.

		for _, id := range candidates {
			if seen[id] {
				continue
			}
			file, ok := byTicket[id]
			if !ok {
				continue
			}
			seen[id] = true
			hits = append(hits, indexHit{ID: id, File: file})
		}
	}
	return hits, nil
}
