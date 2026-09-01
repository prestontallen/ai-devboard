// Package summarize builds a status-report view of in-progress tickets,
// grouped by epic with per-epic aggregates.
package summarize

import (
	"strings"

	"github.com/prestontallen/day2day/internal/model"
	"github.com/prestontallen/day2day/internal/note"
	"github.com/prestontallen/day2day/internal/parse"
)

// Row is one ticket within a Group.
type Row struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Status     string   `json:"status"`     // "On Track" | "Not Started" | "DONE" | "Waiting"
	Started    string   `json:"started"`
	LastUpdate string   `json:"lastUpdate"` // "" or "YYYY-MM-DD"
	Note       string   `json:"note"`       // ≤80 chars
	Repo       string   `json:"repo"`
	PR         string   `json:"pr"`
	Tags       []string `json:"tags"`
}

// Aggregate is the per-Group count summary.
type Aggregate struct {
	Total       int    `json:"total"`
	Done        int    `json:"done"`
	Active      int    `json:"active"`
	NotStarted  int    `json:"notStarted"`
	Waiting     int    `json:"waiting"`
	PercentDone int    `json:"percentDone"`
	Status      string `json:"status"` // "On Track" | "Not Started" | "DONE"
}

// Group is an epic and its children, or the synthetic "Standalone" bucket.
type Group struct {
	Kind      string    `json:"kind"` // "epic" | "standalone"
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Rows      []Row     `json:"rows"`
	Aggregate Aggregate `json:"aggregate"`
}

// Summary is the top-level output returned by Build.
type Summary struct {
	Groups []Group `json:"groups"`
}

// Build reads WORK.md and each ticket's notes file, then assembles the summary.
func Build(wd model.Workdir) (Summary, error) {
	doc, err := parse.File(wd.WorkMD())
	if err != nil {
		return Summary{}, err
	}

	// Collect epics from ## Next, preserving file order.
	var epicOrder []string
	epicGroups := map[string]*Group{}
	if sec := doc.Section(model.SectionNext); sec != nil {
		for _, b := range sec.Blocks {
			if b.IsEpic() {
				b := b // capture
				epicOrder = append(epicOrder, b.ID)
				epicGroups[b.ID] = &Group{
					Kind:  "epic",
					ID:    b.ID,
					Title: b.Title,
					Rows:  []Row{},
				}
			}
		}
	}

	var standaloneRows []Row

	// Scan in-progress sections: Now, Waiting, Next.
	scanSections := []model.SectionName{
		model.SectionNow, model.SectionWaiting, model.SectionNext,
	}
	for _, secName := range scanSections {
		sec := doc.Section(secName)
		if sec == nil {
			continue
		}
		for _, b := range sec.Blocks {
			if b.IsEpic() {
				continue // epics are group headers, not rows
			}
			b := b // capture
			row := buildRow(wd, &b, secName)
			if b.Parent != "" {
				if g, ok := epicGroups[b.Parent]; ok {
					g.Rows = append(g.Rows, row)
					continue
				}
			}
			standaloneRows = append(standaloneRows, row)
		}
	}

	// Assemble groups in epic file order, then standalone.
	var groups []Group
	for _, id := range epicOrder {
		g := epicGroups[id]
		g.Aggregate = aggregateOf(g.Rows)
		groups = append(groups, *g)
	}
	if len(standaloneRows) > 0 {
		groups = append(groups, Group{
			Kind:      "standalone",
			ID:        "",
			Title:     "Standalone",
			Rows:      standaloneRows,
			Aggregate: aggregateOf(standaloneRows),
		})
	}

	if groups == nil {
		groups = []Group{}
	}
	return Summary{Groups: groups}, nil
}

func buildRow(wd model.Workdir, b *model.Block, sec model.SectionName) Row {
	tags := b.Tags
	if tags == nil {
		tags = []string{}
	}
	return Row{
		ID:         b.ID,
		Title:      b.Title,
		Status:     statusBadge(b, sec),
		Started:    b.Started,
		LastUpdate: lastUpdateFor(wd, b),
		Note:       progressNoteFor(wd, b),
		Repo:       b.Repo,
		PR:         b.PR,
		Tags:       tags,
	}
}

func statusBadge(b *model.Block, sec model.SectionName) string {
	if sec == model.SectionWaiting {
		return "Waiting"
	}
	switch b.State {
	case model.StateDone:
		return "DONE"
	case model.StateActive:
		return "On Track"
	default:
		return "Not Started"
	}
}

func lastUpdateFor(wd model.Workdir, b *model.Block) string {
	pr, err := note.Read(wd, b.ID)
	if err == nil && pr.Exists && len(pr.Entries) > 0 {
		ts := pr.Entries[len(pr.Entries)-1].Timestamp
		if len(ts) >= 10 {
			return ts[:10]
		}
		return ts
	}
	return b.Started
}

const noteTruncate = 80

func progressNoteFor(wd model.Workdir, b *model.Block) string {
	if strings.TrimSpace(b.Status) != "" {
		return truncateNote(b.Status, noteTruncate)
	}
	pr, err := note.Read(wd, b.ID)
	if err != nil || !pr.Exists || len(pr.Entries) == 0 {
		return ""
	}
	last := pr.Entries[len(pr.Entries)-1].Body
	for _, line := range strings.Split(last, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return truncateNote(t, noteTruncate)
		}
	}
	return ""
}

func truncateNote(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func aggregateOf(rows []Row) Aggregate {
	a := Aggregate{Total: len(rows)}
	for _, r := range rows {
		switch r.Status {
		case "DONE":
			a.Done++
		case "On Track":
			a.Active++
		case "Not Started":
			a.NotStarted++
		case "Waiting":
			a.Waiting++
		}
	}
	if a.Total > 0 {
		a.PercentDone = (a.Done * 100) / a.Total
	}
	switch {
	case a.Total > 0 && a.Done == a.Total:
		a.Status = "DONE"
	case a.Active > 0 || a.Waiting > 0:
		a.Status = "On Track"
	case a.NotStarted == a.Total:
		a.Status = "Not Started"
	default:
		a.Status = "On Track"
	}
	return a
}
