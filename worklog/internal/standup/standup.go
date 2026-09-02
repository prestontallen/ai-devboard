package standup

import (
	"path/filepath"
	"sort"
	"time"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/parse"
)

// Options control the standup window.
type Options struct {
	Since time.Time // inclusive lower bound on Completed date
	Until time.Time // inclusive upper bound on Completed date
	Today time.Time // used for the report heading; defaults to time.Now()
}

// CompletedEntry is one entry in "Yesterday".
type CompletedEntry struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Repo      string `json:"repo,omitempty"`
	PR        string `json:"pr,omitempty"`
	Parent    string `json:"parent,omitempty"`
	Started   string `json:"started,omitempty"`
	Completed string `json:"completed"`
	Summary   string `json:"summary,omitempty"`
}

// ActiveEntry is one entry in "Today".
type ActiveEntry struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Type    string `json:"type,omitempty"`
	Repo    string `json:"repo,omitempty"`
	PR      string `json:"pr,omitempty"`
	Parent  string `json:"parent,omitempty"`
	Started string `json:"started"`
}

// BlockerEntry is one entry in "Blockers".
type BlockerEntry struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Repo         string `json:"repo,omitempty"`
	PR           string `json:"pr,omitempty"`
	Parent       string `json:"parent,omitempty"`
	WaitingSince string `json:"waitingSince"`
	WaitingDays  int    `json:"waitingDays"` // -1 if unparseable
}

// Report is the structured standup output.
type Report struct {
	Today     string           `json:"today"` // YYYY-MM-DD
	Since     string           `json:"since"` // YYYY-MM-DD (inclusive)
	Until     string           `json:"until"` // YYYY-MM-DD (inclusive)
	Completed []CompletedEntry `json:"completed"`
	// EpicsClosed lists archived epics separately: their children already
	// appear in Completed, so counting the epic there would double-report.
	EpicsClosed []CompletedEntry `json:"epicsClosed,omitempty"`
	Active      []ActiveEntry    `json:"active"`
	Waiting     []BlockerEntry   `json:"waiting"`
}

// Build assembles the standup report.
func Build(wd model.Workdir, opts Options) (Report, error) {
	// Normalize options.
	today := opts.Today
	if today.IsZero() {
		today = time.Now()
	}
	today = truncateDate(today)

	until := opts.Until
	if until.IsZero() {
		until = today
	}
	until = truncateDate(until)

	since := opts.Since
	if since.IsZero() {
		since = today.AddDate(0, 0, -1)
	}
	since = truncateDate(since)

	report := Report{
		Today:     today.Format("2006-01-02"),
		Since:     since.Format("2006-01-02"),
		Until:     until.Format("2006-01-02"),
		Completed: []CompletedEntry{},
		Active:    []ActiveEntry{},
		Waiting:   []BlockerEntry{},
	}

	// Completed: scan all archive files.
	files, _ := filepath.Glob(filepath.Join(wd.ArchiveDir(), "*.md"))
	for _, absPath := range files {
		sourcePath := filepath.Join("archive", filepath.Base(absPath))
		entries, err := ParseFile(absPath, sourcePath)
		if err != nil {
			return report, err
		}
		for _, e := range entries {
			if e.Completed < since.Format("2006-01-02") || e.Completed > until.Format("2006-01-02") {
				continue
			}
			ce := CompletedEntry{
				ID:        e.ID,
				Title:     e.Title,
				Repo:      e.Repo,
				PR:        e.PR,
				Parent:    e.Parent,
				Started:   e.Started,
				Completed: e.Completed,
				Summary:   e.Summary,
			}
			if e.Type == "epic" {
				report.EpicsClosed = append(report.EpicsClosed, ce)
			} else {
				report.Completed = append(report.Completed, ce)
			}
		}
	}
	// Sort completed: descending by Completed, then ascending by ID.
	sort.Slice(report.Completed, func(i, j int) bool {
		a, b := report.Completed[i], report.Completed[j]
		if a.Completed != b.Completed {
			return a.Completed > b.Completed
		}
		return a.ID < b.ID
	})

	// Active: [~] blocks in ## Now.
	doc, err := parse.File(wd.WorkMD())
	if err != nil {
		return report, err
	}

	if sec := doc.Section(model.SectionNow); sec != nil {
		for _, b := range sec.Blocks {
			if b.State != model.StateActive {
				continue
			}
			report.Active = append(report.Active, ActiveEntry{
				ID:      b.ID,
				Title:   b.Title,
				Type:    string(b.Type),
				Repo:    b.Repo,
				PR:      b.PR,
				Parent:  b.Parent,
				Started: b.Started,
			})
		}
	}
	// Sort active: ascending by Started (empty last), then ID ascending.
	sort.Slice(report.Active, func(i, j int) bool {
		a, b := report.Active[i], report.Active[j]
		if a.Started == "" && b.Started != "" {
			return false
		}
		if a.Started != "" && b.Started == "" {
			return true
		}
		if a.Started != b.Started {
			return a.Started < b.Started
		}
		return a.ID < b.ID
	})

	// Waiting: blocks in ## Waiting with age.
	if sec := doc.Section(model.SectionWaiting); sec != nil {
		for _, b := range sec.Blocks {
			days := -1
			if b.WaitingSince != "" {
				if t, err := time.ParseInLocation("2006-01-02", b.WaitingSince, time.Local); err == nil {
					d := int(today.Sub(t).Hours() / 24)
					if d < 0 {
						d = 0
					}
					days = d
				}
			}
			report.Waiting = append(report.Waiting, BlockerEntry{
				ID:           b.ID,
				Title:        b.Title,
				Repo:         b.Repo,
				PR:           b.PR,
				Parent:       b.Parent,
				WaitingSince: b.WaitingSince,
				WaitingDays:  days,
			})
		}
	}
	// Sort waiting: ascending by WaitingSince (oldest first), then ID ascending.
	sort.Slice(report.Waiting, func(i, j int) bool {
		a, b := report.Waiting[i], report.Waiting[j]
		if a.WaitingSince == "" && b.WaitingSince != "" {
			return false
		}
		if a.WaitingSince != "" && b.WaitingSince == "" {
			return true
		}
		if a.WaitingSince != b.WaitingSince {
			return a.WaitingSince < b.WaitingSince
		}
		return a.ID < b.ID
	})

	return report, nil
}

func truncateDate(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}
