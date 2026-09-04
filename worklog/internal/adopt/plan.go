package adopt

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/prestontallen/ai-devboard/worklog/internal/census"
	"github.com/prestontallen/ai-devboard/worklog/internal/projection"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

// Op is what adoption would do to one path.
type Op string

const (
	OpCreate  Op = "create"   // the store renders it; disk has no such file
	OpRewrite Op = "rewrite"  // on disk, but not what the store renders
	OpKeep    Op = "keep"     // already byte-identical to the render
	OpDelete  Op = "delete"   // on disk, canon-shaped, and the store does not own it
	OpProduce Op = "producer" // a bare devboard file with no worklog join: never touched
	OpDerived Op = "derived"  // INDEX.md: rebuilt by reindex, not rendered from the store
)

// Change is one planned file operation. Path is slash-relative, devboard
// files carrying the "devboard/" prefix projection.Render itself uses.
type Change struct {
	Path string
	Op   Op
}

// Plan is the full set of operations adoption would perform.
type Plan struct {
	Changes []Change
}

// Counts summarises a plan by operation.
func (p *Plan) Counts() map[Op]int {
	out := map[Op]int{}
	for _, c := range p.Changes {
		out[c.Op]++
	}
	return out
}

// Writes reports whether the plan would change anything on disk.
func (p *Plan) Writes() bool {
	for _, c := range p.Changes {
		if c.Op == OpCreate || c.Op == OpRewrite || c.Op == OpDelete {
			return true
		}
	}
	return false
}

func (c Change) String() string { return string(c.Op) + " " + c.Path }

// BuildPlan diffs what the store renders against what is on disk.
//
// The delete class is the reason this exists rather than calling RenderTo
// directly. projection.RenderTo only ever writes the paths the store
// produces and never prunes, so on a corpus carrying misfiled or orphaned
// board files a plain render leaves each stray in place beside its
// canonical twin, and the dashboard shows both. Naming the deletes is what
// makes adoption converge instead of accumulate.
//
// skipped is convert.Load's Report.Skipped: devboard files with no
// `worklog:` join key. They are producer-owned, not store-owned, and are
// reported as OpProduce so it is visible that they were considered and
// deliberately left alone.
func BuildPlan(s store.Store, r Roots, skipped []string) (*Plan, error) {
	rendered, err := projection.Render(s)
	if err != nil {
		return nil, err
	}

	layout := projection.Layout{WorklogDir: r.Worklog, DevboardDir: r.Devboard}
	// convert.Load reports a skipped file as <repo>/<slug>.yaml whether or
	// not it actually lives under <repo>/_archive/, so matching on the full
	// relative path silently misses every archived producer file. Keying on
	// repo plus basename is what the two sides genuinely agree on.
	//
	// Found by previewing a plan against the real pre-cutover corpus: the
	// path-keyed version planned to DELETE nole/_archive/embed-retry.yaml
	// and workflow-skills/_archive/canonize-scripts.yaml, two of the three
	// bare producer files adb-cutover's criterion 8 promises to keep.
	producer := map[string]bool{}
	for _, p := range skipped {
		producer[producerKey(filepath.ToSlash(p))] = true
	}

	seen := map[string]bool{}
	var changes []Change

	for rel, want := range rendered {
		seen[rel] = true
		got, err := os.ReadFile(layoutPath(layout, rel))
		switch {
		case os.IsNotExist(err):
			changes = append(changes, Change{rel, OpCreate})
		case err != nil:
			return nil, fmt.Errorf("adopt: reading %s: %w", rel, err)
		case bytes.Equal(got, want):
			changes = append(changes, Change{rel, OpKeep})
		default:
			changes = append(changes, Change{rel, OpRewrite})
		}
	}

	// Everything on disk the store does not render.
	cen, err := census.Walk(r.Worklog, r.Devboard)
	if err != nil {
		return nil, err
	}
	for _, e := range cen.Worklog {
		rel := e.Path
		if seen[rel] {
			continue
		}
		switch e.Class {
		case census.Derived:
			changes = append(changes, Change{rel, OpDerived})
		case census.Canon:
			changes = append(changes, Change{rel, OpDelete})
		}
	}
	for _, e := range cen.Devboard {
		rel := "devboard/" + e.Path
		if seen[rel] {
			continue
		}
		if e.Class != census.Canon {
			continue
		}
		if producer[producerKey(e.Path)] {
			changes = append(changes, Change{rel, OpProduce})
			continue
		}
		changes = append(changes, Change{rel, OpDelete})
	}

	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Op != changes[j].Op {
			return changes[i].Op < changes[j].Op
		}
		return changes[i].Path < changes[j].Path
	})
	return &Plan{Changes: changes}, nil
}

// producerKey reduces a devboard-relative path to the identity both
// convert.Load's Skipped list and an on-disk walk agree on: the repo group
// and the file name, with any _archive/ segment dropped.
func producerKey(rel string) string {
	parts := strings.Split(rel, "/")
	if len(parts) == 0 {
		return rel
	}
	return parts[0] + "/" + parts[len(parts)-1]
}

// layoutPath resolves a render-map key to an absolute path, mirroring
// projection.Layout's own split across the two roots.
func layoutPath(l projection.Layout, rel string) string {
	if len(rel) > len("devboard/") && rel[:len("devboard/")] == "devboard/" {
		return filepath.Join(l.DevboardDir, filepath.FromSlash(rel[len("devboard/"):]))
	}
	return filepath.Join(l.WorklogDir, filepath.FromSlash(rel))
}
