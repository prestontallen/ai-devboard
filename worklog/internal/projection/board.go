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
		task.Plan = append(task.Plan, devboard.PlanItem{Ident: ident(p.ID, p.Rank), Text: p.Text, State: p.State, Extra: p.Extra})
	}
	for _, c := range t.Scorecard {
		task.Score = append(task.Score, devboard.ScoreItem{Ident: ident(c.ID, c.Rank),
			Text: c.Text, Verify: c.Verify, Status: c.Status, Extra: c.Extra})
	}
	for _, d := range t.Decisions {
		task.Decision = append(task.Decision, devboard.Decision{Ident: ident(d.ID, d.Rank),
			What: d.What, Why: d.Why, When: d.When, Complexity: d.Complexity, Extra: d.Extra})
	}
	for _, c := range t.CodeRefs {
		task.Code = append(task.Code, devboard.CodeRef{Ident: ident(c.ID, c.Rank),
			File: c.File, Lines: c.Lines, Lang: c.Lang, Note: c.Note, Snippet: c.Snippet, Extra: c.Extra})
	}
	for _, n := range t.NeedsYou {
		task.NeedsYou = append(task.NeedsYou, devboard.NeedsItem{Ident: ident(n.ID, n.Rank),
			Type: n.Type, Text: n.Text, Detail: n.Detail, Extra: n.Extra})
	}
	for _, w := range t.WaitingOn {
		task.WaitingOn = append(task.WaitingOn, devboard.WaitingItem{Ident: ident(w.ID, w.Rank),
			Text: w.Text, Who: w.Who, Asked: w.Asked, Link: w.Link, Detail: w.Detail, Extra: w.Extra})
	}
	for _, l := range t.Links {
		label := l.Label
		if l.Kind == store.LinkPR && label == "" {
			label = "PR"
		}
		task.Links = append(task.Links, devboard.Link{Ident: ident(l.ID, l.Rank), Label: label, URL: l.URL, Extra: l.Extra})
	}
	task.Extra = t.Extra
}

// BoardYAML renders a devboard task file from canon. Key order is the
// devboard.Task struct's own field order — the same order the retired
// devboard.Mutate used to write, kept here so a store-rendered file
// agrees byte-for-byte with a pre-cutover one rather than only
// semantically. Unknown keys ride each level's inline Extra map, so
// passthrough is preserved and the frozen /api/tasks contract is
// unaffected.
func BoardYAML(t *store.Ticket, kids []*store.Ticket) []byte {
	out, _ := yaml.Marshal(BoardTask(t, kids))
	return out
}

func ident(id store.ID, rank int) devboard.Ident {
	return devboard.Ident{ID: string(id), Rank: rank}
}

// ApplyBoardTask copies a mutated devboard.Task's in-flight detail back
// onto the ticket — the reverse of BoardTask, and the second half of the
// single correspondence. Identity fields (slug, title, type, parent,
// section, state) are deliberately not applied: a task subcommand mutates
// in-flight detail, never who the ticket is.
//
// Each sub-item's ULID and rank ride back through Ident, so editing an
// item's text keeps its identity, reordering keeps it with the right row,
// and only genuinely new items arrive with a zero Ident for PutTicket to
// mint. That is what makes `plan remove` stop renumbering the survivors.
func ApplyBoardTask(t *store.Ticket, task *devboard.Task) {
	t.Phase = task.Phase
	t.Tier = 0
	if task.Tier != nil {
		t.Tier = *task.Tier
	}
	t.Complexity = task.Complexity
	t.Branch = task.Branch
	t.Session = task.Session
	t.RepoPath = task.RepoPath

	t.Scout = nil
	if task.Scout != nil {
		t.Scout = &store.Scout{Mode: task.Scout.Mode, Why: task.Scout.Why, When: task.Scout.When}
	}

	t.PlanSteps = t.PlanSteps[:0]
	for _, p := range task.Plan {
		t.PlanSteps = append(t.PlanSteps, store.PlanStep{
			ID: store.ID(p.ID), Rank: p.Rank, Text: p.Text, State: p.State, Extra: p.Extra})
	}
	t.Scorecard = t.Scorecard[:0]
	for _, c := range task.Score {
		t.Scorecard = append(t.Scorecard, store.ScoreItem{
			ID: store.ID(c.ID), Rank: c.Rank, Text: c.Text, Verify: c.Verify, Status: c.Status, Extra: c.Extra})
	}
	t.Decisions = t.Decisions[:0]
	for _, d := range task.Decision {
		t.Decisions = append(t.Decisions, store.Decision{
			ID: store.ID(d.ID), Rank: d.Rank, What: d.What, Why: d.Why, When: d.When,
			Complexity: d.Complexity, Extra: d.Extra})
	}
	t.CodeRefs = t.CodeRefs[:0]
	for _, c := range task.Code {
		t.CodeRefs = append(t.CodeRefs, store.CodeRef{
			ID: store.ID(c.ID), Rank: c.Rank, File: c.File, Lines: c.Lines, Lang: c.Lang,
			Note: c.Note, Snippet: c.Snippet, Extra: c.Extra})
	}
	t.NeedsYou = t.NeedsYou[:0]
	for _, n := range task.NeedsYou {
		t.NeedsYou = append(t.NeedsYou, store.NeedsItem{
			ID: store.ID(n.ID), Rank: n.Rank, Type: n.Type, Text: n.Text, Detail: n.Detail, Extra: n.Extra})
	}
	t.WaitingOn = t.WaitingOn[:0]
	for _, w := range task.WaitingOn {
		t.WaitingOn = append(t.WaitingOn, store.WaitingItem{
			ID: store.ID(w.ID), Rank: w.Rank, Text: w.Text, Who: w.Who, Asked: w.Asked,
			Link: w.Link, Detail: w.Detail, Extra: w.Extra})
	}
	t.Links = t.Links[:0]
	for _, l := range task.Links {
		kind, label := store.LinkRef, l.Label
		// "PR" is a reserved label (adb-link-pr-label-collision), so it can
		// only have come from the pr relation. BoardTask synthesizes it for
		// an unlabelled PR link, and dropping it again here is what keeps
		// the round trip exact — otherwise every write would rewrite an
		// empty label to "PR" and the store would drift a field per pass.
		if label == "PR" {
			kind, label = store.LinkPR, ""
		}
		t.Links = append(t.Links, store.Link{
			ID: store.ID(l.ID), Rank: l.Rank, Kind: kind, Label: label, URL: l.URL, Extra: l.Extra})
	}
	t.Extra = task.Extra
}
