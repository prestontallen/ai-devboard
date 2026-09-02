package cli

import (
	"os"
	"regexp"
	"strings"

	"github.com/prestontallen/ai-devboard/worklog/internal/devboard"
	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/parse"
)

// syncDevboard runs a devboard side effect and warns on failure. Devboard
// mirroring must never fail the primary worklog operation: the ticket
// change has already landed by the time these run.
func syncDevboard(fn func() error) {
	if err := fn(); err != nil {
		logger.Warn("devboard sync failed", "err", err)
	}
}

func devboardOnStart(id, title, blockType, declaredRepo string) {
	syncDevboard(func() error { return devboard.OnStart(id, title, blockType, declaredRepo) })
}
func devboardOnDone(id string) { syncDevboard(func() error { return devboard.OnDone(id) }) }

// devboardOnPR/devboardOnPRChild are thin name="PR" wrappers around the
// generic devboardOnLink/devboardOnLinkChild below — kept as their own
// named functions only so pr.go's call sites don't change.
func devboardOnPR(id, url string)                   { devboardOnLink(id, "PR", url) }
func devboardOnPRChild(epicID, childID, url string) { devboardOnLinkChild(epicID, childID, "PR", url) }

func devboardOnLink(id, name, url string) {
	syncDevboard(func() error { return devboard.OnLink(id, name, url) })
}

// devboardOnLinkChild mirrors a named link onto childID's own entry in
// the epic's devboard file (via mutateTaskOrChild, same link-replace
// logic devboard.OnLink uses for a plain ticket). No-op when the epic has
// no devboard file yet — same "no-op when absent" contract as OnLink;
// nothing has synced the epic into existence yet, and a bare link mirror
// shouldn't be the thing that does (devboardSyncEpic already owns that,
// from start/done).
func devboardOnLinkChild(epicID, childID, name, url string) {
	syncDevboard(func() error {
		if !devboard.Enabled() {
			return nil
		}
		path, err := devboard.Find(epicID)
		if err != nil || path == "" {
			return err
		}
		_, err = mutateTaskOrChild(path, childID, func(t *devboard.Task) error {
			kept := t.Links[:0]
			for _, l := range t.Links {
				if l.Label != name {
					kept = append(kept, l)
				}
			}
			t.Links = kept
			if url != "" {
				t.Links = append(t.Links, devboard.Link{Label: name, URL: url})
			}
			return nil
		})
		return err
	})
}

// epicChildLineRe matches a notes-file child checkbox line, capturing
// state, id, and (unlike the state-only regexes in internal/start and
// internal/done) the title — the only place a not-yet-started child's
// title lives.
var epicChildLineRe = regexp.MustCompile(`^- \[([ ~x])\]\s+([a-zA-Z0-9_-]+)(?::\s*(.*))?$`)

// epicRoster reads notes/<epicID>.md's child checkboxes and combines them
// with the epic block's WORK.md **Active children**: field to produce the
// full three-state roster. A notes checkbox only ever flips to [x] on
// archival (never on promotion — see dev-context's "Epic children"
// section), so "active" can only be determined from WORK.md, never from
// the notes file alone. Missing/empty notes file: returns (nil, nil), not
// an error — a child can still start before the epic has any other
// history recorded.
func epicRoster(wd model.Workdir, epicID string) ([]devboard.ChildIdentity, error) {
	data, err := os.ReadFile(wd.NotesFile(epicID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var active map[string]bool
	if doc, derr := parse.File(wd.WorkMD()); derr == nil {
		if blk := doc.BlockByID(epicID); blk != nil {
			active = make(map[string]bool, len(blk.ActiveChildren))
			for _, id := range blk.ActiveChildren {
				active[strings.ToLower(id)] = true
			}
		}
	}
	var roster []devboard.ChildIdentity
	for _, line := range strings.Split(string(data), "\n") {
		m := epicChildLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		id := strings.ToLower(m[2])
		state := devboard.ChildPending
		switch {
		case m[1] == "x":
			state = devboard.ChildDone
		case active[id]:
			state = devboard.ChildActive
		}
		roster = append(roster, devboard.ChildIdentity{ID: id, Title: strings.TrimSpace(m[3]), State: state})
	}
	return roster, nil
}

// devboardSyncEpic re-syncs an epic's devboard file from the CURRENT
// WORK.md + notes state (identity, title, full roster) — called after a
// child-of-epic start/resume/done, once WORK.md and notes already reflect
// that event, rather than passed incremental deltas. Silent-warn on
// failure, same contract as the other devboard* hooks.
func devboardSyncEpic(wd model.Workdir, epicID string) {
	syncDevboard(func() error {
		if !devboard.Enabled() {
			return nil
		}
		title := ""
		if doc, err := parse.File(wd.WorkMD()); err == nil {
			if blk := doc.BlockByID(epicID); blk != nil {
				title = blk.Title
			}
		}
		roster, err := epicRoster(wd, epicID)
		if err != nil {
			return err
		}
		return devboard.SyncEpicRoster(epicID, title, roster)
	})
}
