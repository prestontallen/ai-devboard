package convert

import (
	"fmt"
	"strings"

	"github.com/prestontallen/ai-devboard/worklog/internal/store"
	"github.com/prestontallen/ai-devboard/worklog/internal/yamlx"
)

// BoardFile is one parsed devboard YAML: the ticket-side fragment (nil
// for a bare producer file, which is NOT canon — D7), any epic children
// fragments, and the worklog join slug.
type BoardFile struct {
	Join     string // task's worklog slug; "" = bare producer file
	Fragment *store.Ticket
	Children []*store.Ticket
}

// DevboardYAML parses one task file with full fidelity: known keys map to
// typed fields, everything unknown — at any level — lands in Extra maps.
// The lossy schema structs are never used here (that is the ChildEntry
// field-eating class this design retires).
func DevboardYAML(name string, data []byte, boardArchived bool) (*BoardFile, error) {
	// The shared walk (yamlx) keeps scalar behavior identical to the
	// frozen /api/tasks pipeline: unquoted dates stay date strings,
	// NaN/Inf sanitize, YAML 1.2 semantics throughout.
	v, err := yamlx.YAMLToAny(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	raw, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: top level must be a mapping", name)
	}
	join, _ := raw["worklog"].(string)
	if join == "" {
		return &BoardFile{}, nil // bare producer file: stays on disk, untouched
	}
	frag, err := boardFragment(name, raw, boardArchived)
	if err != nil {
		return nil, err
	}
	frag.Slug = store.NormalizeSlug(join)

	bf := &BoardFile{Join: frag.Slug, Fragment: frag}
	if kids, ok := raw["children"].([]any); ok {
		for i, kv := range kids {
			km, ok := kv.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%s: children[%d] is not a mapping", name, i)
			}
			kid, err := boardFragment(fmt.Sprintf("%s children[%d]", name, i), km, false)
			if err != nil {
				return nil, err
			}
			slug, _ := km["id"].(string)
			if slug == "" {
				return nil, fmt.Errorf("%s: children[%d] has no id", name, i)
			}
			kid.Slug = store.NormalizeSlug(slug)
			if title, ok := km["title"].(string); ok {
				kid.Title = title
			}
			if state, ok := km["state"].(string); ok && state != "" {
				kid.State = state
			}
			bf.Children = append(bf.Children, kid)
		}
	}
	return bf, nil
}

var boardKnownKeys = map[string]bool{
	"schema": true, "worklog": true, "title": true, "type": true,
	"phase": true, "tier": true, "complexity": true, "branch": true,
	"session": true, "repo_path": true, "plan": true, "scorecard": true,
	"decisions": true, "code": true, "needs_you": true, "waiting_on": true,
	"links": true, "scout": true, "children": true,
	// child-entry identity keys, consumed by the caller:
	"id": true, "state": true,
}

func boardFragment(name string, raw map[string]any, boardArchived bool) (*store.Ticket, error) {
	t := &store.Ticket{BoardTracked: true, BoardArchived: boardArchived}
	if v, ok := raw["phase"].(string); ok {
		if v == "implement" { // retired alias, normalized at conversion
			v = "implementing"
		}
		t.Phase = v
	}
	switch v := raw["tier"].(type) {
	case int:
		t.Tier = v
	case int64:
		t.Tier = int(v)
	}
	if v, ok := raw["complexity"].(string); ok {
		t.Complexity = v
	}
	if v, ok := raw["branch"].(string); ok {
		t.Branch = v
	}
	if v, ok := raw["session"].(string); ok {
		t.Session = v
	}
	if v, ok := raw["repo_path"].(string); ok {
		t.RepoPath = v
	}
	if sc, ok := raw["scout"].(map[string]any); ok {
		t.Scout = &store.Scout{}
		t.Scout.Mode, _ = sc["mode"].(string)
		t.Scout.Why, _ = sc["why"].(string)
		t.Scout.When = str(sc["when"])
	}

	items(raw, "plan", func(m map[string]any, extra map[string]any) {
		t.PlanSteps = append(t.PlanSteps, store.PlanStep{
			Text: str(m["text"]), State: str(m["state"]), Extra: extra,
		})
	}, "text", "state")
	items(raw, "scorecard", func(m map[string]any, extra map[string]any) {
		t.Scorecard = append(t.Scorecard, store.ScoreItem{
			Text: str(m["text"]), Verify: str(m["verify"]), Status: str(m["status"]), Extra: extra,
		})
	}, "text", "verify", "status")
	items(raw, "decisions", func(m map[string]any, extra map[string]any) {
		what := str(m["what"])
		if what == "" {
			what = str(m["text"]) // frontend tolerates both; so do we
		}
		t.Decisions = append(t.Decisions, store.Decision{
			What: what, Why: str(m["why"]), When: str(m["when"]),
			Complexity: str(m["complexity"]), Extra: extra,
		})
	}, "what", "text", "why", "when", "complexity")
	items(raw, "code", func(m map[string]any, extra map[string]any) {
		t.CodeRefs = append(t.CodeRefs, store.CodeRef{
			File: str(m["file"]), Lines: str(m["lines"]), Lang: str(m["lang"]),
			Note: str(m["note"]), Snippet: str(m["snippet"]), Extra: extra,
		})
	}, "file", "lines", "lang", "note", "snippet")
	items(raw, "needs_you", func(m map[string]any, extra map[string]any) {
		t.NeedsYou = append(t.NeedsYou, store.NeedsItem{
			Type: str(m["type"]), Text: str(m["text"]), Detail: str(m["detail"]), Extra: extra,
		})
	}, "type", "text", "detail")
	items(raw, "waiting_on", func(m map[string]any, extra map[string]any) {
		t.WaitingOn = append(t.WaitingOn, store.WaitingItem{
			Text: str(m["text"]), Who: str(m["who"]), Asked: str(m["asked"]),
			Link: str(m["link"]), Detail: str(m["detail"]), Extra: extra,
		})
	}, "text", "who", "asked", "link", "detail")
	items(raw, "links", func(m map[string]any, extra map[string]any) {
		kind := store.LinkRef
		if strings.EqualFold(str(m["label"]), "pr") {
			kind = store.LinkPR
		}
		t.Links = append(t.Links, store.Link{
			Kind: kind, Label: str(m["label"]), URL: str(m["url"]), Extra: extra,
		})
	}, "label", "url")

	for k, v := range raw {
		if !boardKnownKeys[k] {
			if t.Extra == nil {
				t.Extra = make(map[string]any)
			}
			t.Extra[k] = v
		}
	}
	return t, nil
}

// items iterates a list-of-mappings key, splitting each element into
// known fields (handed to add) and per-item extras.
func items(raw map[string]any, key string, add func(map[string]any, map[string]any), known ...string) {
	list, ok := raw[key].([]any)
	if !ok {
		return
	}
	knownSet := make(map[string]bool, len(known))
	for _, k := range known {
		knownSet[k] = true
	}
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			// Scalar list entries are tolerated by the frontend; preserve
			// them as text-only items.
			m = map[string]any{"text": str(item)}
		}
		var extra map[string]any
		for k, v := range m {
			if !knownSet[k] {
				if extra == nil {
					extra = make(map[string]any)
				}
				extra[k] = v
			}
		}
		add(m, extra)
	}
}

func str(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", x)
	}
}
