// Package reindex rebuilds INDEX.md from a scan of WORK.md + archive/ +
// notes/. The output replaces the existing INDEX.md entirely; manual
// content in INDEX.md is not preserved.
package reindex

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/prestontallen/day2day/internal/model"
	"github.com/prestontallen/day2day/internal/parse"
	"github.com/prestontallen/day2day/internal/render"
)

// Inputs captures the user-supplied options for a reindex call.
type Inputs struct {
	DryRun bool
}

// EntryCounts summarizes how many entries landed in each section of the
// rebuilt INDEX.md.
type EntryCounts struct {
	ByTicket       int `json:"byTicket"`
	ByTag          int `json:"byTag"`
	ByRepo         int `json:"byRepo"`
	ByArchiveMonth int `json:"byArchiveMonth"`
}

// Output is the JSON wire shape returned to the CLI.
type Output struct {
	Status    string      `json:"status"`             // "regenerated" or "would-regenerate"
	IndexPath string      `json:"indexPath"`
	Entries   EntryCounts `json:"entries"`
	Content   string      `json:"content,omitempty"`  // populated on --dry-run only
}

// Record is the internal representation of a single ticket discovered
// during a scan. The four INDEX.md sections all derive from this slice.
type Record struct {
	ID            string
	Title         string
	Tags          []string
	Repo          string
	Parent        string
	Location      string // human-readable, e.g. "WORK.md", "archive/2026-05.md", "notes/epic-a.md"
	Status        string // e.g. "Now", "Next, epic", "archived 2026-05-19", "open child of epic-a"
	IsEpic        bool
	ArchivedMonth string // YYYY-MM if from archive; empty otherwise
}

var (
	monthFileRe     = regexp.MustCompile(`^\d{4}-\d{2}\.md$`)
	openChildLineRe = regexp.MustCompile(`^- \[ \]\s+([a-zA-Z0-9_-]+)(?::\s*(.*))?$`)
)

// Run scans the worklog and rebuilds INDEX.md. In DryRun mode it returns
// the would-be content in Output.Content without touching the file.
func Run(wd model.Workdir, in Inputs) (Output, error) {
	records, archiveMonths, err := collectRecords(wd)
	if err != nil {
		return Output{}, err
	}

	lines := renderIndex(records, archiveMonths)

	out := Output{
		IndexPath: wd.IndexMD(),
		Entries:   countEntries(records, archiveMonths),
	}

	if in.DryRun {
		out.Status = "would-regenerate"
		out.Content = strings.Join(lines, "\n") + "\n"
		return out, nil
	}

	if err := render.WriteAtomic(wd.IndexMD(), lines); err != nil {
		return Output{}, fmt.Errorf("write INDEX.md: %w", err)
	}
	out.Status = "regenerated"
	return out, nil
}

// collectRecords walks WORK.md → archive → notes in that order. The first
// occurrence of an ID wins (so a live WORK.md ticket isn't shadowed by an
// archived entry with the same id, which would itself be a validate-time
// invariant violation but we handle gracefully).
func collectRecords(wd model.Workdir) ([]Record, map[string]int, error) {
	var records []Record
	seen := map[string]bool{}
	archiveMonths := map[string]int{}

	// 1. WORK.md
	doc, err := parse.File(wd.WorkMD())
	switch {
	case err == nil:
		for _, sec := range doc.Sections {
			for _, b := range sec.Blocks {
				if b.ID == "" {
					continue
				}
				status := string(sec.Name)
				if b.IsEpic() {
					status += ", epic"
				}
				records = append(records, Record{
					ID:       b.ID,
					Title:    b.Title,
					Tags:     b.Tags,
					Repo:     b.Repo,
					Parent:   b.Parent,
					Location: "WORK.md",
					Status:   status,
					IsEpic:   b.IsEpic(),
				})
				seen[b.ID] = true
			}
		}
	case errors.Is(err, model.ErrWorkMDMissing):
		// no WORK.md is acceptable for reindex of an empty worklog
	default:
		return nil, nil, fmt.Errorf("read WORK.md: %w", err)
	}

	// 2. archive/*.md
	if entries, readErr := os.ReadDir(wd.ArchiveDir()); readErr == nil {
		// Sort entries for deterministic output ordering.
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, e := range entries {
			if e.IsDir() || !monthFileRe.MatchString(e.Name()) {
				continue
			}
			month := strings.TrimSuffix(e.Name(), ".md")
			abs := filepath.Join(wd.ArchiveDir(), e.Name())
			rel := "archive/" + e.Name()
			archEntries, err := ParseArchive(abs, rel)
			if err != nil {
				return nil, nil, fmt.Errorf("parse %s: %w", rel, err)
			}
			archiveMonths[month] = len(archEntries)
			for _, ae := range archEntries {
				if seen[ae.ID] {
					continue
				}
				records = append(records, Record{
					ID:            ae.ID,
					Title:         ae.Title,
					Tags:          ae.Tags,
					Repo:          ae.Repo,
					Parent:        ae.Parent,
					Location:      rel,
					Status:        "archived " + ae.Completed,
					ArchivedMonth: month,
				})
				seen[ae.ID] = true
			}
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return nil, nil, fmt.Errorf("read archive dir: %w", readErr)
	}

	// 3. notes/*.md  — open children only
	if entries, readErr := os.ReadDir(wd.NotesDir()); readErr == nil {
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			epicID := strings.ToLower(strings.TrimSuffix(e.Name(), ".md"))
			abs := filepath.Join(wd.NotesDir(), e.Name())
			rel := "notes/" + e.Name()
			data, err := os.ReadFile(abs)
			if err != nil {
				continue
			}
			for _, line := range strings.Split(string(data), "\n") {
				m := openChildLineRe.FindStringSubmatch(line)
				if m == nil {
					continue
				}
				childID := strings.ToLower(m[1])
				if seen[childID] {
					continue
				}
				title := ""
				if len(m) > 2 {
					title = strings.TrimSpace(m[2])
				}
				records = append(records, Record{
					ID:       childID,
					Title:    title,
					Parent:   epicID,
					Location: rel,
					Status:   "open child of " + epicID,
				})
				seen[childID] = true
			}
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return nil, nil, fmt.Errorf("read notes dir: %w", readErr)
	}

	return records, archiveMonths, nil
}

func renderIndex(records []Record, archiveMonths map[string]int) []string {
	timestamp := time.Now().UTC().Format(time.RFC3339)

	var lines []string
	lines = append(lines, "# Worklog Index", "")
	lines = append(lines, "Last regenerated: "+timestamp, "")

	// ## By ticket — alphabetical by ID
	sorted := append([]Record(nil), records...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	lines = append(lines, "## By ticket", "")
	if len(sorted) == 0 {
		lines = append(lines, "(empty)")
	} else {
		for _, r := range sorted {
			lines = append(lines, fmt.Sprintf("- %s → %s (%s)", r.ID, r.Location, r.Status))
		}
	}
	lines = append(lines, "")

	// ## By tag — tags alphabetical; IDs within each tag alphabetical
	tagToIDs := map[string][]string{}
	for _, r := range records {
		for _, t := range r.Tags {
			tagToIDs[t] = append(tagToIDs[t], r.ID)
		}
	}
	tags := make([]string, 0, len(tagToIDs))
	for t := range tagToIDs {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	lines = append(lines, "## By tag", "")
	if len(tags) == 0 {
		lines = append(lines, "(empty)")
	} else {
		for _, t := range tags {
			ids := tagToIDs[t]
			sort.Strings(ids)
			lines = append(lines, fmt.Sprintf("- %s: %s", t, strings.Join(ids, ", ")))
		}
	}
	lines = append(lines, "")

	// ## By repo — repos alphabetical; IDs within each alphabetical
	repoToIDs := map[string][]string{}
	for _, r := range records {
		if r.Repo == "" {
			continue
		}
		repoToIDs[r.Repo] = append(repoToIDs[r.Repo], r.ID)
	}
	repos := make([]string, 0, len(repoToIDs))
	for r := range repoToIDs {
		repos = append(repos, r)
	}
	sort.Strings(repos)
	lines = append(lines, "## By repo", "")
	if len(repos) == 0 {
		lines = append(lines, "(empty)")
	} else {
		for _, repo := range repos {
			ids := repoToIDs[repo]
			sort.Strings(ids)
			lines = append(lines, fmt.Sprintf("- %s: %s", repo, strings.Join(ids, ", ")))
		}
	}
	lines = append(lines, "")

	// ## By archive month — reverse-chronological
	months := make([]string, 0, len(archiveMonths))
	for m := range archiveMonths {
		months = append(months, m)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(months)))
	lines = append(lines, "## By archive month", "")
	if len(months) == 0 {
		lines = append(lines, "(empty)")
	} else {
		for _, m := range months {
			count := archiveMonths[m]
			word := "entries"
			if count == 1 {
				word = "entry"
			}
			lines = append(lines, fmt.Sprintf("- %s → archive/%s.md (%d %s)", m, m, count, word))
		}
	}

	// Strip any trailing blanks so WriteAtomic produces a single trailing \n.
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func countEntries(records []Record, months map[string]int) EntryCounts {
	tagSet := map[string]bool{}
	repoSet := map[string]bool{}
	for _, r := range records {
		for _, t := range r.Tags {
			tagSet[t] = true
		}
		if r.Repo != "" {
			repoSet[r.Repo] = true
		}
	}
	return EntryCounts{
		ByTicket:       len(records),
		ByTag:          len(tagSet),
		ByRepo:         len(repoSet),
		ByArchiveMonth: len(months),
	}
}
