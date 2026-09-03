package verify

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/prestontallen/ai-devboard/worklog/internal/convert"
)

// compareNotes is net-new: no existing oracle test covers notes/ segmentation.
// It uses convert.Notes (the same D6 segmentation parser the converter
// itself relies on) on both sides and diffs preamble + entries.
func compareNotes(stageDir, renderDir string) []Drift {
	entries, err := os.ReadDir(filepath.Join(stageDir, "notes"))
	if err != nil {
		return nil // no notes dir in the live snapshot: nothing to compare
	}

	var drifts []Drift
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		rel := "notes/" + e.Name()
		slug := strings.TrimSuffix(e.Name(), ".md")

		origData, err := os.ReadFile(filepath.Join(stageDir, rel))
		if err != nil {
			drifts = append(drifts, Drift{Surface: "notes", File: rel, Ticket: slug, Field: "read", Live: err.Error()})
			continue
		}
		renderedData, err := os.ReadFile(filepath.Join(renderDir, rel))
		if err != nil {
			drifts = append(drifts, Drift{Surface: "notes", File: rel, Ticket: slug, Field: "presence", Live: "present", Rendered: "missing"})
			continue
		}

		orig := convert.Notes(origData)
		rendered := convert.Notes(renderedData)

		if orig.Preamble != rendered.Preamble {
			drifts = append(drifts, Drift{Surface: "notes", File: rel, Ticket: slug, Field: "preamble", Live: orig.Preamble, Rendered: rendered.Preamble})
		}
		if len(orig.Entries) != len(rendered.Entries) {
			drifts = append(drifts, Drift{
				Surface: "notes", File: rel, Ticket: slug, Field: "entry-count",
				Live: fmt.Sprintf("%d", len(orig.Entries)), Rendered: fmt.Sprintf("%d", len(rendered.Entries)),
			})
			continue // entry-index comparison below would be meaningless once counts disagree
		}
		for i := range orig.Entries {
			o, r := orig.Entries[i], rendered.Entries[i]
			if o.Stamp != r.Stamp {
				drifts = append(drifts, Drift{Surface: "notes", File: rel, Ticket: slug, Field: fmt.Sprintf("entries[%d].stamp", i), Live: o.Stamp, Rendered: r.Stamp})
			}
			if o.Body != r.Body {
				drifts = append(drifts, Drift{Surface: "notes", File: rel, Ticket: slug, Field: fmt.Sprintf("entries[%d].body", i), Live: o.Body, Rendered: r.Body})
			}
		}
	}
	return drifts
}
