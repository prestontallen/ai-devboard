package cli

import (
	"strings"

	"github.com/prestontallen/ai-devboard/worklog/internal/devboard"
	"github.com/prestontallen/ai-devboard/worklog/internal/parse"
)

// mutateTaskOrChild is the shared dispatch every task subcommand's mutation
// goes through: on a plain ticket file it runs fn against the file
// directly, same as before. On an epic file it requires child, finds or
// appends that child's entry (see devboard/schema.md's "Epic files"), runs
// fn against a scratch Task view of just that child's in-flight-detail
// fields, and copies the result back — so the exact same subcommand
// closures work unchanged against either shape.
//
// Returns the worklog id the caller should treat as "the ticket this
// mutation is about" — the file's own Worklog for a plain ticket, or the
// targeted child's own id for an epic (each child has its own notes file;
// the epic's is a different ticket entirely).
func mutateTaskOrChild(path, child string, fn func(*devboard.Task) error) (worklogID string, err error) {
	err = devboard.Mutate(path, func(t *devboard.Task) error {
		if t.Type != "epic" {
			if child != "" {
				return errWithExit(64, "task: --child is only valid when --id names an epic")
			}
			worklogID = t.Worklog
			return fn(t)
		}
		if child == "" {
			return errWithExit(64,
				"task: --id %q is an epic; pass --child <id> (children: %s)",
				t.Worklog, strings.Join(childIDs(t.Children), ", "))
		}
		idx := findOrAppendChild(t, child)
		worklogID = child
		view := childWorkView(&t.Children[idx])
		if err := fn(view); err != nil {
			return err
		}
		applyChildWorkView(&t.Children[idx], view)
		return nil
	})
	return worklogID, err
}

// childIDs lists an epic's known children, for the missing---child refusal.
func childIDs(children []devboard.ChildEntry) []string {
	if len(children) == 0 {
		return []string{"(none yet — the epic has no started children)"}
	}
	out := make([]string, len(children))
	for i, c := range children {
		out[i] = c.ID
	}
	return out
}

// findOrAppendChild returns the index of childID in t.Children, appending
// a pending placeholder entry if absent — matches devboard.MutateChild's
// contract, kept in sync here since the epic branch of mutateTaskOrChild
// operates on the scratch view rather than calling MutateChild directly.
func findOrAppendChild(t *devboard.Task, childID string) int {
	for i, c := range t.Children {
		if strings.EqualFold(c.ID, childID) {
			return i
		}
	}
	t.Children = append(t.Children, devboard.ChildEntry{ID: childID, State: devboard.ChildPending})
	return len(t.Children) - 1
}

// childWorkView copies a ChildEntry's in-flight-detail fields into a
// scratch Task so subcommand closures written for *devboard.Task work
// unchanged against a child of an epic. Identity fields (ID/Title/State)
// are deliberately excluded — those are worklog-authored via the roster
// sync, never through a task subcommand.
func childWorkView(c *devboard.ChildEntry) *devboard.Task {
	return &devboard.Task{
		Branch: c.Branch, Session: c.Session, Tier: c.Tier, Complexity: c.Complexity,
		Phase: c.Phase, Plan: c.Plan, Score: c.Score, Decision: c.Decision,
		Code: c.Code, NeedsYou: c.NeedsYou, WaitingOn: c.WaitingOn, Links: c.Links,
	}
}

// applyChildWorkView copies a mutated scratch Task's in-flight-detail
// fields back onto the ChildEntry.
func applyChildWorkView(c *devboard.ChildEntry, t *devboard.Task) {
	c.Branch, c.Session, c.Tier, c.Complexity = t.Branch, t.Session, t.Tier, t.Complexity
	c.Phase, c.Plan, c.Score, c.Decision = t.Phase, t.Plan, t.Score, t.Decision
	c.Code, c.NeedsYou, c.WaitingOn, c.Links = t.Code, t.NeedsYou, t.WaitingOn, t.Links
}

// childOfEpicParent reports the parent epic id when id names a ticket
// that is itself a child of an epic (non-empty Parent in WORK.md), so
// resolveTaskPath can refuse recreating a stray per-child file instead of
// silently creating one. Best-effort: a ticket no longer in WORK.md (e.g.
// already archived) can't be checked this way and is let through
// unchanged — this only catches the live-ticket case the misuse actually
// happens in.
func childOfEpicParent(id string) string {
	wd, err := resolveWorkdir()
	if err != nil {
		return ""
	}
	doc, err := parse.File(wd.WorkMD())
	if err != nil {
		return ""
	}
	b := doc.BlockByID(id)
	if b == nil {
		return ""
	}
	return b.Parent
}
