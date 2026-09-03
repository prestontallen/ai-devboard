package verify

import (
	"fmt"
	"path/filepath"

	"github.com/prestontallen/ai-devboard/worklog/internal/feedback"
)

func compareFeedback(stageDir, renderDir string) []Drift {
	orig, err := feedback.Parse(filepath.Join(stageDir, "FEEDBACK.md"))
	if err != nil {
		return []Drift{{Surface: "feedback", File: "FEEDBACK.md", Field: "parse", Live: err.Error()}}
	}
	rendered, err := feedback.Parse(filepath.Join(renderDir, "FEEDBACK.md"))
	if err != nil {
		return []Drift{{Surface: "feedback", File: "FEEDBACK.md", Field: "parse", Rendered: err.Error()}}
	}

	var drifts []Drift
	if len(orig) != len(rendered) {
		drifts = append(drifts, Drift{
			Surface: "feedback", File: "FEEDBACK.md", Field: "entry-count",
			Live: fmt.Sprintf("%d", len(orig)), Rendered: fmt.Sprintf("%d", len(rendered)),
		})
		return drifts
	}
	for i := range orig {
		o, r := orig[i], rendered[i]
		ticket := fmt.Sprintf("%d", o.Timestamp)
		fields := []struct{ name, o, r string }{
			{"signal", string(o.Signal), string(r.Signal)}, {"trigger", o.Trigger, r.Trigger},
			{"excerpt", o.Excerpt, r.Excerpt}, {"context", o.Context, r.Context},
		}
		for _, f := range fields {
			if f.o != f.r {
				drifts = append(drifts, Drift{Surface: "feedback", File: "FEEDBACK.md", Ticket: ticket, Field: f.name, Live: f.o, Rendered: f.r})
			}
		}
		if o.Resolved != r.Resolved {
			drifts = append(drifts, Drift{Surface: "feedback", File: "FEEDBACK.md", Ticket: ticket, Field: "resolved", Live: fmt.Sprintf("%d", o.Resolved), Rendered: fmt.Sprintf("%d", r.Resolved)})
		}
	}
	return drifts
}
