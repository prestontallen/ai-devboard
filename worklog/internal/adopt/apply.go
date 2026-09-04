package adopt

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/prestontallen/ai-devboard/worklog/internal/census"
	"github.com/prestontallen/ai-devboard/worklog/internal/convert"
	"github.com/prestontallen/ai-devboard/worklog/internal/hazard"
	"github.com/prestontallen/ai-devboard/worklog/internal/migrate"
	"github.com/prestontallen/ai-devboard/worklog/internal/projection"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

// ErrRefused is the sentinel for every gate refusal. Callers map it to an
// exit code; the message names what to fix.
var ErrRefused = errors.New("adopt refused")

// Options configures one adoption.
type Options struct {
	Roots Roots
	// SnapshotDir is where the verbatim backup lands. It MUST be outside
	// both live roots: convert.ReadCorpusDir ingests any *.md under notes/
	// or archive/, so a backup written inside the corpus poisons every
	// later conversion.
	SnapshotDir string
	// Apply writes. False (the default) is a dry run that touches nothing.
	Apply bool
	// Reindex regenerates INDEX.md from the rendered tree. Supplied by the
	// caller because internal/reindex works on a model.Workdir and adopt
	// stays free of that dependency.
	Reindex func() error
}

// Result is what one adoption did, or would do.
type Result struct {
	Plan     *Plan
	Snapshot *Manifest
	Applied  bool
}

// Run adopts a corpus, or reports what adopting it would do.
//
// The order is the safety argument, and every step before the snapshot is
// read-only:
//
//  1. census — every file accounted for, or refuse naming the strays
//  2. convert — the strict parser's own refusals, unchanged
//  3. hazard — constructs the parsers drop WITHOUT refusing
//  4. stale rows — a ticket in the store but not in this corpus would be
//     rendered back onto disk, resurrecting it
//  5. plan — what would change
//  6. snapshot — verbatim, digest-verified, before the first byte
//  7. apply — writes and deletes
//  8. reindex, then the post-condition
//
// A failure after step 6 restores from the snapshot before returning, so a
// half-applied corpus is not a reachable end state.
func Run(s store.Store, o Options) (*Result, error) {
	if o.SnapshotDir == "" {
		return nil, fmt.Errorf("adopt: SnapshotDir is required")
	}
	if err := outsideRoots(o.SnapshotDir, o.Roots); err != nil {
		return nil, err
	}

	// 1. Census.
	cen, err := census.Walk(o.Roots.Worklog, o.Roots.Devboard)
	if err != nil {
		return nil, err
	}
	if stray := cen.Unclassified(); len(stray) > 0 {
		return nil, fmt.Errorf("%w: %d file(s) no rule accounts for, so completeness cannot be claimed:\n  %s",
			ErrRefused, len(stray), strings.Join(stray, "\n  "))
	}

	// 2. Convert, through the staged shape ReadCorpusDir expects.
	staged, cleanup, err := stage(o.Roots)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	corpus, err := convert.ReadCorpusDir(staged)
	if err != nil {
		return nil, fmt.Errorf("%w: reading the corpus: %v", ErrRefused, err)
	}
	rep, err := convert.Load(s, corpus)
	if err != nil {
		return nil, fmt.Errorf("%w: %v\n(a convert refusal is a corpus a human fixes by hand; --force does not bypass it)", ErrRefused, err)
	}

	// 3. Hazards.
	haz, err := hazard.Scan(o.Roots.Worklog, o.Roots.Devboard)
	if err != nil {
		return nil, err
	}
	if len(haz) > 0 {
		lines := make([]string, len(haz))
		for i, f := range haz {
			lines[i] = f.String()
		}
		return nil, fmt.Errorf("%w: %d construct(s) the parsers drop without refusing:\n  %s",
			ErrRefused, len(haz), strings.Join(lines, "\n  "))
	}

	// 4. Stale rows.
	tickets, err := s.Tickets()
	if err != nil {
		return nil, err
	}
	converted := map[string]bool{}
	for _, slug := range rep.Slugs {
		converted[slug] = true
	}
	if stale := migrate.StaleRows(tickets, converted); len(stale) > 0 {
		return nil, fmt.Errorf("%w: %d stale row(s) in the store have no counterpart in this corpus and would be rendered back onto disk:\n  %s",
			ErrRefused, len(stale), strings.Join(stale, "\n  "))
	}

	// 5. Plan.
	plan, err := BuildPlan(s, o.Roots, rep.Skipped)
	if err != nil {
		return nil, err
	}
	res := &Result{Plan: plan}
	if !o.Apply {
		return res, nil
	}

	// 6. Snapshot, before the first byte.
	m, err := Snapshot(o.Roots, o.SnapshotDir)
	if err != nil {
		return nil, fmt.Errorf("adopt: snapshot failed, nothing was written: %w", err)
	}
	res.Snapshot = m

	// 7-8. Everything from here can leave a half-written tree, so any
	// failure rolls back before returning.
	if err := applyPlan(s, o, plan); err != nil {
		return res, rollback(o, err)
	}
	if o.Reindex != nil {
		if err := o.Reindex(); err != nil {
			return res, rollback(o, fmt.Errorf("regenerating INDEX.md: %w", err))
		}
	}
	// The post-condition is the runtime gate itself, not a proxy for it: if
	// EditedIn is not empty, the very next write verb would refuse.
	edited, err := projection.EditedIn(s, projection.Layout{WorklogDir: o.Roots.Worklog, DevboardDir: o.Roots.Devboard})
	if err != nil {
		return res, rollback(o, err)
	}
	if len(edited) > 0 {
		return res, rollback(o, fmt.Errorf("post-condition failed: %d projection(s) still differ from the store:\n  %s",
			len(edited), strings.Join(edited, "\n  ")))
	}

	res.Applied = true
	return res, nil
}

// applyPlan performs the writes and the deletes.
func applyPlan(s store.Store, o Options, plan *Plan) error {
	layout := projection.Layout{WorklogDir: o.Roots.Worklog, DevboardDir: o.Roots.Devboard}
	if err := projection.RenderTo(s, layout); err != nil {
		return fmt.Errorf("rendering projections: %w", err)
	}
	// RenderTo never prunes, so the deletes are ours to perform.
	for _, c := range plan.Changes {
		if c.Op != OpDelete {
			continue
		}
		if err := os.Remove(layoutPath(layout, c.Path)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing %s: %w", c.Path, err)
		}
	}
	return nil
}

// rollback restores and reports both failures if the restore also fails,
// since a failed restore is the one situation the operator must not miss.
func rollback(o Options, cause error) error {
	if err := Restore(o.SnapshotDir, o.Roots); err != nil {
		return fmt.Errorf("adopt failed (%v) AND the rollback failed (%v); the snapshot at %s is intact and unapplied", cause, err, o.SnapshotDir)
	}
	return fmt.Errorf("%w: %v (rolled back; the corpus is byte-identical to before)", ErrRefused, cause)
}

// stage lays the two roots out the way ReadCorpusDir expects: one root with
// devboard/ nested inside. It copies rather than reading in place so the
// conversion cannot be affected by a concurrent write mid-read.
func stage(r Roots) (string, func(), error) {
	dir, err := os.MkdirTemp("", "worklog-adopt-stage-*")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { os.RemoveAll(dir) }
	if err := copyTree(r.Worklog, dir, r.Devboard, map[string]string{}); err != nil {
		cleanup()
		return "", func() {}, err
	}
	if r.Devboard != "" {
		if err := copyTree(r.Devboard, filepath.Join(dir, "devboard"), "", map[string]string{}); err != nil {
			cleanup()
			return "", func() {}, err
		}
	}
	return dir, cleanup, nil
}

// outsideRoots refuses a snapshot destination inside either live root.
// convert.ReadCorpusDir ingests any *.md under notes/ or archive/ and
// refuses on a note whose slug names no ticket, so a backup written inside
// the corpus breaks every later conversion.
func outsideRoots(dest string, r Roots) error {
	abs, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	for _, root := range []string{r.Worklog, r.Devboard} {
		if root == "" {
			continue
		}
		ra, err := filepath.Abs(root)
		if err != nil {
			return err
		}
		if abs == ra || strings.HasPrefix(abs, ra+string(filepath.Separator)) {
			return fmt.Errorf("%w: the snapshot destination %s is inside the live root %s; a backup there would be read back as corpus and poison every later conversion",
				ErrRefused, dest, root)
		}
	}
	return nil
}
