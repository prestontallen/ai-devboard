package verify

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/prestontallen/ai-devboard/worklog/internal/standup"
)

func compareArchive(stageDir, renderDir string) []Drift {
	entries, err := os.ReadDir(filepath.Join(stageDir, "archive"))
	if err != nil {
		return nil // no archive dir in the live snapshot: nothing to compare
	}

	var drifts []Drift
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		rel := "archive/" + e.Name()
		orig, err := standup.ParseFile(filepath.Join(stageDir, rel), rel)
		if err != nil {
			drifts = append(drifts, Drift{Surface: "archive", File: rel, Field: "parse", Live: err.Error()})
			continue
		}
		rendered, err := standup.ParseFile(filepath.Join(renderDir, rel), rel)
		if err != nil {
			drifts = append(drifts, Drift{Surface: "archive", File: rel, Field: "parse", Rendered: err.Error()})
			continue
		}
		drifts = append(drifts, diffArchiveEntries(rel, orig, rendered)...)
	}
	return drifts
}

func diffArchiveEntries(file string, orig, rendered []standup.Entry) []Drift {
	byID := make(map[string]standup.Entry, len(rendered))
	for _, e := range rendered {
		byID[e.ID] = e
	}
	var drifts []Drift
	for _, o := range orig {
		r, ok := byID[o.ID]
		if !ok {
			drifts = append(drifts, Drift{Surface: "archive", File: file, Ticket: o.ID, Field: "presence", Live: "present", Rendered: "missing"})
			continue
		}
		delete(byID, o.ID)
		fields := []struct{ name, o, r string }{
			{"title", o.Title, r.Title}, {"repo", o.Repo, r.Repo},
			{"parent", o.Parent, r.Parent}, {"pr", o.PR, r.PR},
			{"type", o.Type, r.Type}, {"started", o.Started, r.Started},
			{"completed", o.Completed, r.Completed}, {"summary", o.Summary, r.Summary},
		}
		for _, f := range fields {
			if f.o != f.r {
				drifts = append(drifts, Drift{Surface: "archive", File: file, Ticket: o.ID, Field: f.name, Live: f.o, Rendered: f.r})
			}
		}
	}
	for id := range byID {
		drifts = append(drifts, Drift{Surface: "archive", File: file, Ticket: id, Field: "presence", Live: "missing", Rendered: "present"})
	}
	return drifts
}
