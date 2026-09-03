package store

import (
	"fmt"
	"slices"
)

// Shared aggregate semantics. Both implementations call these so the
// behavioral contract (validation, sub-item ID stability, decision
// dedupe, journal diffs) cannot drift between them — the swappability
// criterion depends on it.

var (
	validTypes     = []string{TypeTicket, TypeEpic, TypeSpike, TypeChore}
	validStates    = []string{StatePending, StateActive, StateDone}
	validSections  = []string{"", SectionNow, SectionWaiting, SectionNext, SectionSomeday, SectionBlocked}
	validPlanState = []string{"", "pending", "in_progress", "done", "blocked"}
	validScore     = []string{"", "pending", "pass", "fail"}
	validNeeds     = []string{"question", "checkpoint"}
	validScout     = []string{"ran", "inline", "skipped"}
	validLinkKinds = []string{LinkPR, LinkRef}
)

// ValidateTicket enforces the enum and relational rules the schema
// promises: canonical vocabularies only, one pr-kind link. A slug is
// optional — WORK.md's title-only quick-capture blocks are legal, and
// their identity is the ULID alone.
func ValidateTicket(t *Ticket) error {
	if !slices.Contains(validTypes, t.Type) {
		return fmt.Errorf("ticket %s: invalid type %q", t.Slug, t.Type)
	}
	if !slices.Contains(validStates, t.State) {
		return fmt.Errorf("ticket %s: invalid state %q", t.Slug, t.State)
	}
	if !slices.Contains(validSections, t.Section) {
		return fmt.Errorf("ticket %s: invalid section %q", t.Slug, t.Section)
	}
	if t.Phase != "" && !slices.Contains(Phases, t.Phase) {
		return fmt.Errorf("ticket %s: invalid phase %q (canonical vocabulary only)", t.Slug, t.Phase)
	}
	if t.Complexity != "" && t.Complexity != "low" && t.Complexity != "medium" && t.Complexity != "high" {
		return fmt.Errorf("ticket %s: invalid complexity %q", t.Slug, t.Complexity)
	}
	if t.Scout != nil && !slices.Contains(validScout, t.Scout.Mode) {
		return fmt.Errorf("ticket %s: invalid scout mode %q", t.Slug, t.Scout.Mode)
	}
	for _, p := range t.PlanSteps {
		if !slices.Contains(validPlanState, p.State) {
			return fmt.Errorf("ticket %s: invalid plan state %q", t.Slug, p.State)
		}
	}
	for _, s := range t.Scorecard {
		if !slices.Contains(validScore, s.Status) {
			return fmt.Errorf("ticket %s: invalid scorecard status %q", t.Slug, s.Status)
		}
	}
	for _, n := range t.NeedsYou {
		if !slices.Contains(validNeeds, n.Type) {
			return fmt.Errorf("ticket %s: invalid needs-you type %q", t.Slug, n.Type)
		}
	}
	prs := 0
	for _, l := range t.Links {
		if !slices.Contains(validLinkKinds, l.Kind) {
			return fmt.Errorf("ticket %s: invalid link kind %q", t.Slug, l.Kind)
		}
		if l.Kind == LinkPR {
			prs++
		}
	}
	if prs > 1 {
		return fmt.Errorf("ticket %s: more than one pr-kind link (the PR relation is unique)", t.Slug)
	}
	return nil
}

// MintSubItemIDs assigns ULIDs to sub-items that lack one and fills Rank
// from position for new items. Existing IDs and ranks are never touched:
// removing an item leaves every survivor's identity intact.
func MintSubItemIDs(t *Ticket) {
	for i := range t.PlanSteps {
		mint(&t.PlanSteps[i].ID, &t.PlanSteps[i].Rank, i)
	}
	for i := range t.Scorecard {
		mint(&t.Scorecard[i].ID, &t.Scorecard[i].Rank, i)
	}
	for i := range t.Decisions {
		mint(&t.Decisions[i].ID, &t.Decisions[i].Rank, i)
	}
	for i := range t.CodeRefs {
		mint(&t.CodeRefs[i].ID, &t.CodeRefs[i].Rank, i)
	}
	for i := range t.NeedsYou {
		mint(&t.NeedsYou[i].ID, &t.NeedsYou[i].Rank, i)
	}
	for i := range t.WaitingOn {
		mint(&t.WaitingOn[i].ID, &t.WaitingOn[i].Rank, i)
	}
	for i := range t.Links {
		mint(&t.Links[i].ID, &t.Links[i].Rank, i)
	}
	for i := range t.Transitions {
		mint(&t.Transitions[i].ID, &t.Transitions[i].Rank, i)
	}
	for i := range t.NoteEntries {
		mint(&t.NoteEntries[i].ID, &t.NoteEntries[i].Rank, i)
	}
}

func mint(id *ID, rank *int, pos int) {
	if *id == "" {
		*id = NewID()
	}
	if *rank == 0 {
		*rank = pos + 1
	}
}

// DedupeDecisions collapses entries with identical (What, Why), keeping
// the first — decisions are a set, per adb-decision-dedupe.
func DedupeDecisions(in []Decision) []Decision {
	seen := make(map[[2]string]bool, len(in))
	out := in[:0]
	for _, d := range in {
		key := [2]string{d.What, d.Why}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, d)
	}
	return out
}

// DiffScalars returns journal rows for scalar fields that changed between
// prev (nil = creation) and next. Implementations write these in the same
// transaction as the Put.
func DiffScalars(prev, next *Ticket) []FieldChange {
	get := scalarFields(next)
	var old map[string]string
	if prev != nil {
		old = scalarFields(prev)
	}
	var out []FieldChange
	for _, f := range scalarOrder {
		newV := get[f]
		oldV := ""
		if old != nil {
			oldV = old[f]
		}
		if prev == nil && newV == "" {
			continue // creation: only journal populated fields
		}
		if oldV != newV {
			out = append(out, FieldChange{Field: f, Old: oldV, New: newV})
		}
	}
	return out
}

var scalarOrder = []string{
	"slug", "title", "type", "state", "section", "parent", "repo",
	"started", "waiting_since", "pr", "source", "acceptance", "status",
	"plan_text", "archived", "completed", "summary", "time_spent",
	"archive_month", "board_tracked", "board_archived", "tier",
	"complexity", "phase", "branch", "session", "repo_path",
}

func scalarFields(t *Ticket) map[string]string {
	pr := "<absent>"
	if t.PR != nil {
		pr = *t.PR
	}
	return map[string]string{
		"slug": t.Slug, "title": t.Title, "type": t.Type, "state": t.State,
		"section": t.Section, "parent": string(t.ParentID), "repo": t.Repo,
		"started": t.Started, "waiting_since": t.WaitingSince, "pr": pr,
		"source": t.Source, "acceptance": t.Acceptance, "status": t.Status,
		"plan_text": t.PlanText, "archived": boolStr(t.Archived),
		"completed": t.Completed, "summary": t.Summary,
		"time_spent": t.TimeSpent, "archive_month": t.ArchiveMonth,
		"board_tracked": boolStr(t.BoardTracked), "board_archived": boolStr(t.BoardArchived),
		"tier": intStr(t.Tier), "complexity": t.Complexity, "phase": t.Phase,
		"branch": t.Branch, "session": t.Session, "repo_path": t.RepoPath,
	}
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return ""
}

func intStr(i int) string {
	if i == 0 {
		return ""
	}
	return fmt.Sprintf("%d", i)
}
