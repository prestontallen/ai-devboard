package verify

import (
	"path/filepath"
	"strings"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/parse"
)

// workmdView is the same field set TestOracles compares
// (internal/projection/projection_test.go), flattened to strings so
// individual fields can be diffed and reported separately.
type workmdView struct {
	State, Type, Parent, Repo, Started, PR, Acceptance, Title, Tags string
}

func collectWorkMDViews(doc *model.WorkDoc) map[string]workmdView {
	out := map[string]workmdView{}
	for _, sec := range doc.Sections {
		for _, b := range sec.Blocks {
			key := b.ID
			if key == "" {
				key = "\x00" + b.Title
			}
			out[key] = workmdView{
				State: string(b.State), Type: string(b.Type), Parent: b.Parent,
				Repo: b.Repo, Started: b.Started, PR: b.PR,
				Acceptance: b.Acceptance, Title: b.Title,
				Tags: strings.Join(b.Tags, ","),
			}
		}
	}
	return out
}

// compareWorkMD localizes drift to ticket + field, per criterion 2.
func compareWorkMD(stageDir, renderDir string) []Drift {
	orig, err := parse.File(filepath.Join(stageDir, "WORK.md"))
	if err != nil {
		return []Drift{{Surface: "workmd", File: "WORK.md", Field: "parse", Live: err.Error()}}
	}
	rendered, err := parse.File(filepath.Join(renderDir, "WORK.md"))
	if err != nil {
		return []Drift{{Surface: "workmd", File: "WORK.md", Field: "parse", Rendered: err.Error()}}
	}

	o, r := collectWorkMDViews(orig), collectWorkMDViews(rendered)
	var drifts []Drift
	for id, ov := range o {
		rv, ok := r[id]
		ticket := strings.TrimPrefix(id, "\x00")
		if !ok {
			drifts = append(drifts, Drift{Surface: "workmd", File: "WORK.md", Ticket: ticket, Field: "presence", Live: "present", Rendered: "missing"})
			continue
		}
		drifts = append(drifts, diffWorkMDView(ticket, ov, rv)...)
		delete(r, id)
	}
	for id := range r {
		ticket := strings.TrimPrefix(id, "\x00")
		drifts = append(drifts, Drift{Surface: "workmd", File: "WORK.md", Ticket: ticket, Field: "presence", Live: "missing", Rendered: "present"})
	}
	return drifts
}

func diffWorkMDView(ticket string, o, r workmdView) []Drift {
	var drifts []Drift
	fields := []struct {
		name string
		o, r string
	}{
		{"state", o.State, r.State}, {"type", o.Type, r.Type},
		{"parent", o.Parent, r.Parent}, {"repo", o.Repo, r.Repo},
		{"started", o.Started, r.Started}, {"pr", o.PR, r.PR},
		{"acceptance", o.Acceptance, r.Acceptance}, {"title", o.Title, r.Title},
		{"tags", o.Tags, r.Tags},
	}
	for _, f := range fields {
		if f.o != f.r {
			drifts = append(drifts, Drift{Surface: "workmd", File: "WORK.md", Ticket: ticket, Field: f.name, Live: f.o, Rendered: f.r})
		}
	}
	return drifts
}
