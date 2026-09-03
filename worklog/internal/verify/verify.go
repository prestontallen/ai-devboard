// Package verify implements the read-only drift check behind `worklog
// verify` (adb-projection-render): it stages a read-only snapshot of the
// live worklog + devboard dirs (reusing internal/migrate's staging/
// tear-detection mechanism), converts that snapshot into a store the
// caller provides, renders internal/projection's five surfaces from it,
// and reports any field-level drift between the staged snapshot and the
// render — never writing to the live dirs, ever.
//
// internal/verify stays interface-only (store.Store, never a concrete
// implementation) so the CLI layer remains the sole composition root that
// constructs one, per the ticket's Decision #4.
package verify

import (
	"fmt"
	"os"

	"github.com/prestontallen/ai-devboard/worklog/internal/convert"
	"github.com/prestontallen/ai-devboard/worklog/internal/migrate"
	"github.com/prestontallen/ai-devboard/worklog/internal/projection"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

// Drift is one field-level disagreement between the live (staged) data and
// what internal/projection renders from the converted store.
type Drift struct {
	Surface  string `json:"surface"`          // "workmd" | "archive" | "notes" | "index" | "feedback" | "board"
	File     string `json:"file"`             // relative path, e.g. "WORK.md", "archive/2026-09.md"
	Ticket   string `json:"ticket,omitempty"` // slug/ID the drift belongs to, when applicable
	Field    string `json:"field"`
	Live     string `json:"live"`
	Rendered string `json:"rendered"`
}

// Report is the full result of one verify run.
type Report struct {
	Drifts []Drift `json:"drifts"`
}

// Clean reports whether the run found no drift at all.
func (r *Report) Clean() bool { return len(r.Drifts) == 0 }

func (r *Report) add(d ...Drift) { r.Drifts = append(r.Drifts, d...) }

// stageFunc is a test seam: production code always uses migrate.Stage.
// Tests override it to simulate a torn read (TestVerifyDetectsTornRead)
// without reaching into internal/migrate's own unexported test hooks, and
// to count staging calls (TestVerifySingleStagedRead).
var stageFunc = migrate.Stage

// Run stages the live worklog + devboard dirs named by src into an
// ephemeral scratch copy (one staging call — the same copy feeds both the
// store conversion below and the devboard-feed comparator, per criterion
// 12), converts that copy into s, renders s's projections into a second
// ephemeral scratch dir, and compares every surface between the two
// scratch copies. Neither live directory is ever opened for writing.
func Run(s store.Store, src migrate.Sources) (*Report, error) {
	stageDir, err := os.MkdirTemp("", "worklog-verify-stage-*")
	if err != nil {
		return nil, fmt.Errorf("verify: %w", err)
	}
	defer os.RemoveAll(stageDir)

	if err := stageFunc(src, stageDir); err != nil {
		return nil, err // *migrate.ErrTornSnapshot, or a hard I/O error
	}

	corpus, err := convert.ReadCorpusDir(stageDir)
	if err != nil {
		return nil, fmt.Errorf("verify: reading staged corpus: %w", err)
	}
	if _, err := convert.Load(s, corpus); err != nil {
		return nil, fmt.Errorf("verify: %w", err)
	}

	renderDir, err := os.MkdirTemp("", "worklog-verify-render-*")
	if err != nil {
		return nil, fmt.Errorf("verify: %w", err)
	}
	defer os.RemoveAll(renderDir)

	if err := projection.RenderAll(s, renderDir); err != nil {
		return nil, fmt.Errorf("verify: rendering projections: %w", err)
	}

	rep := &Report{}
	rep.add(compareWorkMD(stageDir, renderDir)...)
	rep.add(compareArchive(stageDir, renderDir)...)
	rep.add(compareNotes(stageDir, renderDir)...)
	rep.add(compareIndex(stageDir, renderDir)...)
	rep.add(compareFeedback(stageDir, renderDir)...)
	rep.add(compareBoard(stageDir, renderDir)...)
	return rep, nil
}
