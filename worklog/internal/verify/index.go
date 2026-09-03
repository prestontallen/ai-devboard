package verify

import (
	"fmt"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/reindex"
)

// compareIndex is Decision #3: reindex.Run writes INDEX.md via
// render.WriteAtomic unless told not to, so every call here — on both
// sides — MUST pass Inputs{DryRun: true}. Neither call here can reach the
// real live dir anyway (both roots are scratch copies), but DryRun stays
// explicit and unconditional per the contract, and it also means neither
// scratch copy gets an INDEX.md write it doesn't need.
func compareIndex(stageDir, renderDir string) []Drift {
	liveWD, err := model.NewWorkdir(stageDir)
	if err != nil {
		return []Drift{{Surface: "index", File: "INDEX.md", Field: "workdir", Live: err.Error()}}
	}
	renderedWD, err := model.NewWorkdir(renderDir)
	if err != nil {
		return []Drift{{Surface: "index", File: "INDEX.md", Field: "workdir", Rendered: err.Error()}}
	}

	liveOut, err := reindex.Run(liveWD, reindex.Inputs{DryRun: true})
	if err != nil {
		return []Drift{{Surface: "index", File: "INDEX.md", Field: "scan", Live: err.Error()}}
	}
	renderedOut, err := reindex.Run(renderedWD, reindex.Inputs{DryRun: true})
	if err != nil {
		return []Drift{{Surface: "index", File: "INDEX.md", Field: "scan", Rendered: err.Error()}}
	}

	var drifts []Drift
	l, r := liveOut.Entries, renderedOut.Entries
	fields := []struct {
		name string
		l, r int
	}{
		{"byTicket", l.ByTicket, r.ByTicket}, {"byTag", l.ByTag, r.ByTag},
		{"byRepo", l.ByRepo, r.ByRepo}, {"byArchiveMonth", l.ByArchiveMonth, r.ByArchiveMonth},
	}
	for _, f := range fields {
		if f.l != f.r {
			drifts = append(drifts, Drift{Surface: "index", File: "INDEX.md", Field: f.name, Live: fmt.Sprintf("%d", f.l), Rendered: fmt.Sprintf("%d", f.r)})
		}
	}
	return drifts
}
