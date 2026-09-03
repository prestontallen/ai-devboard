package projection

import (
	"gopkg.in/yaml.v3"

	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

// BoardYAML renders a devboard task file from canon. Output is a plain
// mapping (alphabetical key order — deterministic, and no consumer reads
// order): the frozen /api/tasks contract is about fields and passthrough,
// which merging each Extra map back in preserves. Epic children nest with
// their full in-flight detail, per the frozen children[] shape.
func BoardYAML(t *store.Ticket, kids []*store.Ticket) []byte {
	root := boardMap(t)
	root["schema"] = 1
	root["worklog"] = t.Slug
	if len(kids) > 0 {
		var ks []any
		for _, k := range kids {
			km := boardMap(k)
			km["id"] = k.Slug
			km["state"] = k.State
			ks = append(ks, km)
		}
		root["children"] = ks
	}
	out, _ := yaml.Marshal(root)
	return out
}

func boardMap(t *store.Ticket) map[string]any {
	m := map[string]any{}
	set := func(k string, v any, keep bool) {
		if keep {
			m[k] = v
		}
	}
	set("title", t.Title, t.Title != "")
	set("type", t.Type, t.Type != store.TypeTicket)
	set("phase", t.Phase, t.Phase != "")
	set("tier", t.Tier, t.Tier != 0)
	set("complexity", t.Complexity, t.Complexity != "")
	set("branch", t.Branch, t.Branch != "")
	set("session", t.Session, t.Session != "")
	set("repo_path", t.RepoPath, t.RepoPath != "")
	if t.Scout != nil {
		m["scout"] = map[string]any{"mode": t.Scout.Mode, "why": t.Scout.Why, "when": t.Scout.When}
	}
	set("plan", itemList(t.PlanSteps, func(p store.PlanStep) (map[string]any, map[string]any) {
		return map[string]any{"text": p.Text, "state": p.State}, p.Extra
	}), len(t.PlanSteps) > 0)
	set("scorecard", itemList(t.Scorecard, func(c store.ScoreItem) (map[string]any, map[string]any) {
		return map[string]any{"text": c.Text, "verify": c.Verify, "status": c.Status}, c.Extra
	}), len(t.Scorecard) > 0)
	set("decisions", itemList(t.Decisions, func(d store.Decision) (map[string]any, map[string]any) {
		out := map[string]any{"what": d.What, "why": d.Why}
		if d.When != "" {
			out["when"] = d.When
		}
		if d.Complexity != "" {
			out["complexity"] = d.Complexity
		}
		return out, d.Extra
	}), len(t.Decisions) > 0)
	set("code", itemList(t.CodeRefs, func(c store.CodeRef) (map[string]any, map[string]any) {
		out := map[string]any{"file": c.File}
		if c.Lines != "" {
			out["lines"] = c.Lines
		}
		if c.Lang != "" {
			out["lang"] = c.Lang
		}
		if c.Note != "" {
			out["note"] = c.Note
		}
		if c.Snippet != "" {
			out["snippet"] = c.Snippet
		}
		return out, c.Extra
	}), len(t.CodeRefs) > 0)
	set("needs_you", itemList(t.NeedsYou, func(n store.NeedsItem) (map[string]any, map[string]any) {
		out := map[string]any{"type": n.Type, "text": n.Text}
		if n.Detail != "" {
			out["detail"] = n.Detail
		}
		return out, n.Extra
	}), len(t.NeedsYou) > 0)
	set("waiting_on", itemList(t.WaitingOn, func(w store.WaitingItem) (map[string]any, map[string]any) {
		out := map[string]any{"text": w.Text, "who": w.Who}
		if w.Asked != "" {
			out["asked"] = w.Asked
		}
		if w.Link != "" {
			out["link"] = w.Link
		}
		if w.Detail != "" {
			out["detail"] = w.Detail
		}
		return out, w.Extra
	}), len(t.WaitingOn) > 0)
	set("links", itemList(t.Links, func(l store.Link) (map[string]any, map[string]any) {
		label := l.Label
		if l.Kind == store.LinkPR && label == "" {
			label = "PR"
		}
		return map[string]any{"label": label, "url": l.URL}, l.Extra
	}), len(t.Links) > 0)
	for k, v := range t.Extra {
		m[k] = v
	}
	return m
}

func itemList[T any](items []T, conv func(T) (map[string]any, map[string]any)) []any {
	out := make([]any, 0, len(items))
	for _, it := range items {
		m, extra := conv(it)
		for k, v := range extra {
			m[k] = v
		}
		out = append(out, m)
	}
	return out
}
