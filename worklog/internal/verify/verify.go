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
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/prestontallen/ai-devboard/worklog/internal/convert"
	"github.com/prestontallen/ai-devboard/worklog/internal/migrate"
	"github.com/prestontallen/ai-devboard/worklog/internal/projection"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

// Drift class. The two need OPPOSITE fixes, which is why they are reported
// separately (this is ADB-VERIFY-EDIT-CLASS's request):
//
//   - ClassUncanonical: a live file's content is not what the store renders.
//     The store is the source, so the fix is to discard and re-render.
//   - ClassRenderer: the store does not survive its own render → re-parse
//     round trip. Nothing on disk is wrong; the fix is in the renderer or
//     the parser, and re-rendering would bake the loss in.
const (
	ClassUncanonical = "uncanonical"
	ClassRenderer    = "renderer"
)

// Drift is one field-level disagreement between the live (staged) data and
// what internal/projection renders from the converted store.
type Drift struct {
	Class    string `json:"class"`
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
func Run(s, s2 store.Store, src migrate.Sources) (*Report, error) {
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
	// Everything above compares the staged copy against the render, so it
	// is the uncanonical class by construction.
	for i := range rep.Drifts {
		rep.Drifts[i].Class = ClassUncanonical
	}

	// The oracle. Every comparator above is a hand-picked field view read
	// through internal/parse, the LENIENT parser — so it measures the
	// strict parser with the lossy one, and a difference in any field the
	// view omits (Section, Status, Plan, Source, Links, WaitingSince,
	// Files, ActiveChildren, ExtraFields) is silently clean. This compares
	// the whole struct, with the strict converter on both sides.
	drift, err := compareStores(s, s2, renderDir)
	if err != nil {
		return nil, err
	}
	rep.add(drift...)
	return rep, nil
}

// compareStores converts the rendered tree back with the SAME strict
// converter that produced s, and compares both stores whole-struct via
// store.Canonical. A difference means the store did not survive its own
// file representation: a renderer or parser defect, not a stale file.
func compareStores(s, s2 store.Store, renderDir string) ([]Drift, error) {
	corpus, err := convert.ReadCorpusDir(renderDir)
	if err != nil {
		return nil, fmt.Errorf("verify: re-reading the render: %w", err)
	}
	if _, err := convert.Load(s2, corpus); err != nil {
		// The render does not survive the strict parser at all. That is the
		// sharpest possible renderer defect, so report rather than fail.
		return []Drift{{
			Class: ClassRenderer, Surface: "store", File: "(rendered tree)",
			Field: "convert", Live: "parsed", Rendered: err.Error(),
		}}, nil
	}
	return canonicalDrift(s, s2)
}

// canonicalDrift is the whole-struct comparison itself, split out so it can
// be exercised directly against two stores that differ in a single field.
func canonicalDrift(s, s2 store.Store) ([]Drift, error) {
	want, err := store.Canonical(s)
	if err != nil {
		return nil, err
	}
	got, err := store.Canonical(s2)
	if err != nil {
		return nil, err
	}
	if bytes.Equal(want, got) {
		return nil, nil
	}
	lw, lg := firstDifferingLine(want, got)
	return []Drift{{
		Class: ClassRenderer, Surface: "store", File: "(round trip)",
		Field: "canonical", Live: lw, Rendered: lg,
	}}, nil
}

// firstDifferingLine gives a compact, actionable pointer into two large
// canonical documents.
func firstDifferingLine(a, b []byte) (string, string) {
	al := strings.Split(string(a), "\n")
	bl := strings.Split(string(b), "\n")
	for i := 0; i < len(al) && i < len(bl); i++ {
		if al[i] != bl[i] {
			return strings.TrimSpace(al[i]), strings.TrimSpace(bl[i])
		}
	}
	if len(al) != len(bl) {
		return fmt.Sprintf("%d lines", len(al)), fmt.Sprintf("%d lines", len(bl))
	}
	return "", ""
}
