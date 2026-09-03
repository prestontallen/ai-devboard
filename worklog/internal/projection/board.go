package projection

import (
	"gopkg.in/yaml.v3"

	"github.com/prestontallen/ai-devboard/worklog/internal/devboard"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

// BoardTask is the single store.Ticket → devboard.Task correspondence.
// Everything that needs to move a ticket's in-flight detail into the
// board's shape goes through it: BoardYAML below renders from it, and the
// CLI's task<sub> chokepoint mutates it and applies the result back
// (ApplyBoardTask), so there is exactly one place that knows which store
// field is which board field.
//
// That single-place property is the point. Two independent encodings of
// this correspondence is what produced M2's scorecard `verify` bug, where
// the renderer emitted a key the legacy writer omitted.
//
// kids nest as children[] with their own full in-flight detail, per the
// frozen children[] shape; pass nil for a non-epic.
func BoardTask(t *store.Ticket, kids []*store.Ticket) *devboard.Task {
	task := &devboard.Task{Schema: 1, Worklog: t.Slug}
	fillBoard(task, t)
	for _, k := range kids {
		c := devboard.ChildEntry{ID: k.Slug, Title: k.Title, State: k.State}
		var kt devboard.Task
		fillBoard(&kt, k)
		// A child carries in-flight detail only; identity (id/title/state)
		// is worklog-authored via the roster sync, never through a task
		// subcommand.
		c.Branch, c.Session, c.Tier, c.Complexity = kt.Branch, kt.Session, kt.Tier, kt.Complexity
		c.Phase, c.Plan, c.Score, c.Decision = kt.Phase, kt.Plan, kt.Score, kt.Decision
		c.Code, c.NeedsYou, c.WaitingOn, c.Links = kt.Code, kt.NeedsYou, kt.WaitingOn, kt.Links
		c.Scout, c.Extra = kt.Scout, kt.Extra
		task.Children = append(task.Children, c)
	}
	return task
}

func fillBoard(task *devboard.Task, t *store.Ticket) {
	task.Title = t.Title
	if t.Type != store.TypeTicket {
		task.Type = t.Type
	}
	task.Phase = t.Phase
	if t.Tier != 0 {
		tier := t.Tier
		task.Tier = &tier
	}
	task.Complexity = t.Complexity
	task.Branch = t.Branch
	task.Session = t.Session
	task.RepoPath = t.RepoPath
	if t.Scout != nil {
		task.Scout = &devboard.Scout{Mode: t.Scout.Mode, Why: t.Scout.Why, When: t.Scout.When}
	}
	for _, p := range t.PlanSteps {
		task.Plan = append(task.Plan, devboard.PlanItem{Text: p.Text, State: p.State, Extra: p.Extra})
	}
	for _, c := range t.Scorecard {
		task.Score = append(task.Score, devboard.ScoreItem{
			Text: c.Text, Verify: c.Verify, Status: c.Status, Extra: c.Extra})
	}
	for _, d := range t.Decisions {
		task.Decision = append(task.Decision, devboard.Decision{
			What: d.What, Why: d.Why, When: d.When, Complexity: d.Complexity, Extra: d.Extra})
	}
	for _, c := range t.CodeRefs {
		task.Code = append(task.Code, devboard.CodeRef{
			File: c.File, Lines: c.Lines, Lang: c.Lang, Note: c.Note, Snippet: c.Snippet, Extra: c.Extra})
	}
	for _, n := range t.NeedsYou {
		task.NeedsYou = append(task.NeedsYou, devboard.NeedsItem{
			Type: n.Type, Text: n.Text, Detail: n.Detail, Extra: n.Extra})
	}
	for _, w := range t.WaitingOn {
		task.WaitingOn = append(task.WaitingOn, devboard.WaitingItem{
			Text: w.Text, Who: w.Who, Asked: w.Asked, Link: w.Link, Detail: w.Detail, Extra: w.Extra})
	}
	for _, l := range t.Links {
		label := l.Label
		if l.Kind == store.LinkPR && label == "" {
			label = "PR"
		}
		task.Links = append(task.Links, devboard.Link{Label: label, URL: l.URL, Extra: l.Extra})
	}
	task.Extra = t.Extra
}

// BoardYAML renders a devboard task file from canon. Key order is the
// devboard.Task struct's own field order, which is what devboard.Mutate
// already writes today, so a store-rendered file and a legacy-written one
// agree byte-for-byte rather than only semantically. Unknown keys ride
// each level's inline Extra map, so passthrough is preserved and the
// frozen /api/tasks contract is unaffected.
func BoardYAML(t *store.Ticket, kids []*store.Ticket) []byte {
	out, _ := yaml.Marshal(BoardTask(t, kids))
	return out
}
