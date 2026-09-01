// Package start implements the `worklog start <id>` operation: promoting a
// ticket from `## Next` / `## Someday` (or a child of an epic from its notes
// file) into `## Now`, with cap enforcement and parent-linkage upkeep.
package start

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/prestontallen/day2day/internal/model"
	"github.com/prestontallen/day2day/internal/parse"
	"github.com/prestontallen/day2day/internal/reindex"
	"github.com/prestontallen/day2day/internal/render"
)

// Resolution identifies what kind of ID was passed to start.
type Resolution int

const (
	ResUnknown Resolution = iota
	ResStandalone
	ResChildOfEpic
	ResEpic
	ResAlreadyActive
)

// ResolveResult bundles everything Resolve learned, so Run can branch
// without re-doing the lookup work.
type ResolveResult struct {
	Resolution Resolution
	Block      *model.Block // populated for Standalone, AlreadyActive, Epic, or (synthesized) ChildOfEpic
	EpicID     string       // parent epic id for ChildOfEpic; self id for Epic
	ChildLine  string       // raw `- [ ] ...` line text for ChildOfEpic
	Children   []string     // open child IDs for Epic refusal
}

// Inputs captures the user's intent for a start call.
type Inputs struct {
	ID         string
	Repo       string
	Tags       []string
	Acceptance string
}

// Output is the JSON wire shape for the CLI success path.
type Output struct {
	Status   string   `json:"status"`
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Section  string   `json:"section"`
	Parent   string   `json:"parent"`
	Started  string   `json:"started"`
	WorkMD   string   `json:"workMD"`
	Warnings []string `json:"warnings"`
}

// Sentinel errors. The CLI wrapper maps these to specific exit codes and
// messages; the orchestration layer never speaks UI.
var (
	ErrIDNotFound      = errors.New("ticket ID not found")
	ErrCapExceeded     = errors.New("## Now is at cap")
	ErrAlreadyStarted  = errors.New("ticket is already in ## Now")
	ErrEpicCannotStart = errors.New("epics do not occupy ## Now")
	// ErrParentEpicGone: the child resolved from a notes file, but its epic
	// block is no longer in WORK.md (typically archived). Detected before
	// any mutation so no partial state is written.
	ErrParentEpicGone = errors.New("parent epic not in WORK.md")
)

const cap = 5

const indexNotUpdatedWarning = "INDEX.md not updated (deferred to Phase 2B)"

// childLineRe matches a checkbox child line in a notes file. The state char
// is captured but ignored for matching; only `[ ]` lines represent open
// children in practice, but the regex accepts any state so we can detect
// "already started elsewhere" scenarios if they ever come up.
var childLineRe = regexp.MustCompile(`^- \[[ ~x]\]\s+([a-zA-Z0-9_-]+)(?::\s*(.*))?$`)

// openChildLineRe is strictly `- [ ]` lines (open children only).
var openChildLineRe = regexp.MustCompile(`^- \[ \]\s+([a-zA-Z0-9_-]+)`)

// Resolve inspects the parsed WorkDoc and (if needed) the notes/ directory to
// determine what `id` refers to.
func Resolve(wd model.Workdir, doc *model.WorkDoc, id string) (ResolveResult, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		return ResolveResult{}, fmt.Errorf("empty id")
	}

	// Direct block lookup.
	if block := doc.BlockByID(id); block != nil {
		switch {
		case block.Section == model.SectionNow && block.IsActive():
			return ResolveResult{Resolution: ResAlreadyActive, Block: block}, nil
		case block.IsEpic():
			children, err := readEpicChildren(wd, id)
			if err != nil {
				return ResolveResult{}, err
			}
			return ResolveResult{
				Resolution: ResEpic,
				Block:      block,
				EpicID:     id,
				Children:   children,
			}, nil
		default:
			return ResolveResult{Resolution: ResStandalone, Block: block}, nil
		}
	}

	// Otherwise scan notes/*.md for a checkbox child matching id.
	epicID, line, title, err := scanNotesForChild(wd, id)
	if err != nil {
		return ResolveResult{}, err
	}
	if epicID != "" {
		return ResolveResult{
			Resolution: ResChildOfEpic,
			EpicID:     epicID,
			ChildLine:  line,
			Block: &model.Block{
				ID:     id,
				Title:  title,
				Parent: epicID,
				Type:   model.TypeTicket,
			},
		}, nil
	}

	return ResolveResult{Resolution: ResUnknown}, nil
}

// scanNotesForChild walks notes/*.md looking for a checkbox line whose first
// token matches id (case-insensitive). Returns (epicID, raw line, title, nil)
// on first match; ("", "", "", nil) when not found.
func scanNotesForChild(wd model.Workdir, id string) (string, string, string, error) {
	entries, err := os.ReadDir(wd.NotesDir())
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", "", nil
		}
		return "", "", "", err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		epic := strings.TrimSuffix(e.Name(), ".md")
		path := filepath.Join(wd.NotesDir(), e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			m := childLineRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			if strings.EqualFold(m[1], id) {
				return epic, line, strings.TrimSpace(m[2]), nil
			}
		}
	}
	return "", "", "", nil
}

// readEpicChildren reads notes/<epicID>.md and returns the open child IDs in
// document order.
func readEpicChildren(wd model.Workdir, epicID string) ([]string, error) {
	data, err := os.ReadFile(wd.NotesFile(epicID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		if m := openChildLineRe.FindStringSubmatch(line); m != nil {
			out = append(out, m[1])
		}
	}
	return out, nil
}

// Run is the orchestration entry-point. It parses WORK.md, resolves the id,
// applies the appropriate mutation, and writes the result atomically.
func Run(wd model.Workdir, in Inputs, today string) (Output, error) {
	doc, err := parse.File(wd.WorkMD())
	if err != nil {
		return Output{}, err
	}

	res, err := Resolve(wd, doc, in.ID)
	if err != nil {
		return Output{}, err
	}

	switch res.Resolution {
	case ResUnknown:
		return Output{}, fmt.Errorf("%w: %q", ErrIDNotFound, in.ID)
	case ResAlreadyActive:
		return Output{}, fmt.Errorf("%w: %q is already in ## Now", ErrAlreadyStarted, in.ID)
	case ResEpic:
		inNow := nowIDSet(doc)
		var startable []string
		for _, c := range res.Children {
			if !inNow[strings.ToLower(c)] {
				startable = append(startable, c)
			}
		}
		return Output{}, fmt.Errorf("%w; epic %q %s",
			ErrEpicCannotStart, in.ID, describeChildren(startable, res.Children))
	}

	// Cap check. The check only applies when the operation would INCREASE
	// the count in ## Now (i.e. block isn't already there).
	nowCount, nowIDs := nowSnapshot(doc)
	movingFromNow := res.Resolution == ResStandalone &&
		res.Block.Section == model.SectionNow
	if !movingFromNow && nowCount >= cap {
		return Output{}, fmt.Errorf("%w (%d/%d); current Now: %s",
			ErrCapExceeded, nowCount, cap, strings.Join(nowIDs, ", "))
	}

	var (
		newLines []string
		title    string
		parent   string
	)

	switch res.Resolution {
	case ResStandalone:
		title = res.Block.Title
		parent = res.Block.Parent
		afterRemove, src, err := render.RemoveBlock(doc, in.ID)
		if err != nil {
			return Output{}, err
		}
		// Re-parse so line numbers are accurate for AppendToSection.
		afterDoc, err := parse.Bytes(doc.Path, []byte(strings.Join(afterRemove, "\n")))
		if err != nil {
			return Output{}, err
		}
		blockLines := render.FormatTicketBlock(render.BlockOptions{
			Title:      src.Title,
			ID:         src.ID,
			Type:       string(src.Type),
			Parent:     src.Parent,
			Repo:       coalesceStr(in.Repo, src.Repo),
			Tags:       coalesceTags(in.Tags, src.Tags),
			Started:    today,
			PR:         src.PR,
			Files:      src.Files,
			Acceptance: coalesceStr(in.Acceptance, src.Acceptance),
			NotesRef:   src.NotesRef,
			Status:     src.Status,
			State:      model.StateActive,
		})
		final, err := render.AppendToSection(afterDoc, model.SectionNow, blockLines)
		if err != nil {
			return Output{}, err
		}
		newLines = final

	case ResChildOfEpic:
		// The child came from a notes-file scan; the epic block itself may
		// be gone (archived epics keep their notes file as history). Refuse
		// with a clear cause before any mutation.
		if doc.BlockByID(res.EpicID) == nil {
			hint := reindex.ArchivedHint(wd.ArchiveDir(), res.EpicID)
			if hint == "" {
				hint = "not found and not in the archive"
			}
			return Output{}, fmt.Errorf("%w: %q %s; archived epics cannot take new work",
				ErrParentEpicGone, res.EpicID, hint)
		}
		title = res.Block.Title
		parent = res.EpicID
		blockLines := render.FormatTicketBlock(render.BlockOptions{
			Title:      title,
			ID:         in.ID,
			Parent:     res.EpicID,
			Repo:       in.Repo,
			Tags:       in.Tags,
			Acceptance: in.Acceptance,
			Started:    today,
			State:      model.StateActive,
		})
		afterAppend, err := render.AppendToSection(doc, model.SectionNow, blockLines)
		if err != nil {
			return Output{}, err
		}
		// Re-parse so UpdateEpicActiveChildren sees up-to-date line numbers.
		afterDoc, err := parse.Bytes(doc.Path, []byte(strings.Join(afterAppend, "\n")))
		if err != nil {
			return Output{}, err
		}
		final, err := render.UpdateEpicActiveChildren(afterDoc, res.EpicID, in.ID)
		if err != nil {
			return Output{}, err
		}
		newLines = final
	}

	if err := render.WriteAtomic(wd.WorkMD(), newLines); err != nil {
		return Output{}, err
	}

	return Output{
		Status:   "started",
		ID:       in.ID,
		Title:    title,
		Section:  "Now",
		Parent:   parent,
		Started:  today,
		WorkMD:   wd.WorkMD(),
		Warnings: []string{indexNotUpdatedWarning},
	}, nil
}

// nowIDSet returns the set of lowercase block IDs currently in ## Now.
func nowIDSet(doc *model.WorkDoc) map[string]bool {
	out := map[string]bool{}
	if now := doc.Section(model.SectionNow); now != nil {
		for _, b := range now.Blocks {
			if b.ID != "" {
				out[strings.ToLower(b.ID)] = true
			}
		}
	}
	return out
}

// describeChildren composes the human-readable suffix for an epic-refusal
// error. Prefers listing children that aren't already in flight.
func describeChildren(startable, allOpen []string) string {
	switch {
	case len(startable) > 0:
		return "has startable children: " + strings.Join(startable, ", ")
	case len(allOpen) > 0:
		return "has no startable children (all in progress)"
	default:
		return "has no open children"
	}
}

func nowSnapshot(doc *model.WorkDoc) (int, []string) {
	now := doc.Section(model.SectionNow)
	if now == nil {
		return 0, nil
	}
	ids := make([]string, 0, len(now.Blocks))
	for _, b := range now.Blocks {
		if b.ID != "" {
			ids = append(ids, b.ID)
		}
	}
	return len(now.Blocks), ids
}

func coalesceStr(override, def string) string {
	if override != "" {
		return override
	}
	return def
}

func coalesceTags(override, def []string) []string {
	if override != nil {
		return override
	}
	return def
}
