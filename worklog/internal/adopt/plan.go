package adopt

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/prestontallen/ai-devboard/worklog/internal/census"
	"github.com/prestontallen/ai-devboard/worklog/internal/projection"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

// Op is what adoption would do to one path.
type Op string

const (
	OpCreate  Op = "create"  // the store renders it; disk has no such file
	OpRewrite Op = "rewrite" // on disk, but not what the store renders
	OpKeep    Op = "keep"    // already byte-identical to the render
	OpDelete  Op = "delete"  // on disk, canon-shaped, and the store does not own it
	OpOrphan  Op = "orphan"  // a devboard file with no worklog join and no ticket of its name: adoption refuses
	OpDerived Op = "derived" // INDEX.md: rebuilt by reindex, not rendered from the store
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
// Paths returns the relative paths carrying op, in plan order.
func (p *Plan) Paths(op Op) []string {
	var out []string
	for _, c := range p.Changes {
		if c.Op == op {
			out = append(out, c.Path)
		}
	}
	return out
}

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
// `worklog:` join key AND no ticket sharing their filename, so absorption
// could not place them. They are reported as OpOrphan, which Run refuses
// over. Deleting them instead would be silent data loss, and that is what
// the retired producer class actually existed to prevent — bare files were
// never a path anyone published to.
func BuildPlan(s store.Store, r Roots, skipped []string) (*Plan, error) {
	rendered, err := projection.Render(s)
	if err != nil {
		return nil, err
	}

	layout := projection.Layout{WorklogDir: r.Worklog, DevboardDir: r.Devboard}
	orphan := map[string]bool{}
	for _, p := range skipped {
		orphan[filepath.ToSlash(p)] = true
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
		if orphan[filepath.ToSlash(e.Path)] {
			changes = append(changes, Change{rel, OpOrphan})
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

// layoutPath resolves a render-map key to an absolute path, mirroring
// projection.Layout's own split across the two roots.
func layoutPath(l projection.Layout, rel string) string {
	if len(rel) > len("devboard/") && rel[:len("devboard/")] == "devboard/" {
		return filepath.Join(l.DevboardDir, filepath.FromSlash(rel[len("devboard/"):]))
	}
	return filepath.Join(l.WorklogDir, filepath.FromSlash(rel))
}
