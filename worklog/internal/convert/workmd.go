// Package convert holds the full-fidelity corpus parsers and the
// converter that loads today's seven representations into a Store.
//
// These parsers deliberately differ from the CLI's splice-oriented ones:
// internal/parse drops unknown fields, free lines, and most archive
// fields because the splice renderer preserves them positionally; here
// nothing positional survives, so anything the parser doesn't model is
// either captured (ExtraFields) or REFUSED (criterion 12 — the converter
// never imports garbage silently).
package convert

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

var (
	sectionRe = regexp.MustCompile(`^## +(.+?)\s*$`)
	blockRe   = regexp.MustCompile(`^- \[([ ~x])\]\s*(.*)$`)
	metaRe    = regexp.MustCompile(`^  - \*\*(.+?)\*\*:(?:\s(.*))?$`)
	titleRe   = regexp.MustCompile(`^\*\*(.+?)\*\*\s*(?:—|--)\s*(.*)$`)
	linkRe    = regexp.MustCompile(`^(.+?)\s*(?:—|--)\s*(.*)$`)
)

var sectionNames = map[string]string{
	"now": store.SectionNow, "waiting": store.SectionWaiting,
	"next": store.SectionNext, "someday": store.SectionSomeday,
	"blocked": store.SectionBlocked,
}

var stateChars = map[string]string{
	" ": store.StatePending, "~": store.StateActive, "x": store.StateDone,
}

// WorkMD parses a WORK.md into ticket fragments (the WORK.md-side fields
// only; notes/archive/devboard merge in later). Unmodeled non-blank lines
// inside a section refuse with file:line.
func WorkMD(data []byte) ([]*store.Ticket, error) {
	lines := splitNorm(data)
	var out []*store.Ticket
	var cur *store.Ticket
	inSection := ""
	sawSection := false

	for i, line := range lines {
		lineNo := i + 1
		if m := sectionRe.FindStringSubmatch(line); m != nil {
			name := strings.ToLower(strings.TrimSpace(m[1]))
			sec, ok := sectionNames[name]
			if !ok {
				return nil, fmt.Errorf("WORK.md:%d: unknown section %q", lineNo, m[1])
			}
			inSection = sec
			sawSection = true
			cur = nil
			continue
		}
		if !sawSection {
			continue // document preamble is template-owned
		}
		if m := blockRe.FindStringSubmatch(line); m != nil {
			cur = &store.Ticket{
				State:   stateChars[m[1]],
				Section: inSection,
				Type:    store.TypeTicket,
				// Document position becomes data here. This is the one
				// place WORK.md's ordering exists — ## Next is a
				// hand-ordered priority queue — so it is captured on the
				// way in rather than reconstructed by sorting later.
				Rank: len(out),
			}
			body := strings.TrimSpace(m[2])
			if t := titleRe.FindStringSubmatch(body); t != nil {
				cur.Slug = store.NormalizeSlug(t[1])
				cur.Title = strings.TrimSpace(t[2])
			} else {
				cur.Title = body
			}
			out = append(out, cur)
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		if cur != nil {
			if m := metaRe.FindStringSubmatch(line); m != nil {
				applyWorkField(cur, strings.TrimSpace(m[1]), strings.TrimSpace(m[2]))
				continue
			}
		}
		return nil, fmt.Errorf("WORK.md:%d: unmodeled content %q — refusing to convert (fix the file or the model)", lineNo, line)
	}
	return out, nil
}

func applyWorkField(t *store.Ticket, label, value string) {
	switch strings.ToLower(label) {
	case "id":
		t.Slug = store.NormalizeSlug(value)
	case "type":
		t.Type = strings.ToLower(value)
	case "parent":
		t.ExtraFields = addField(t.ExtraFields, "__parent_slug", store.NormalizeSlug(value))
	case "repo":
		t.Repo = value
	case "tags":
		t.Tags = splitCSV(value)
	case "started":
		t.Started = value
	case "pr":
		v := value
		t.PR = &v
	case "source":
		t.Source = value
	case "link":
		if m := linkRe.FindStringSubmatch(value); m != nil {
			t.Links = append(t.Links, store.Link{
				Kind: store.LinkRef, Label: strings.TrimSpace(m[1]), URL: strings.TrimSpace(m[2]),
			})
		} else {
			t.ExtraFields = addField(t.ExtraFields, "Link", value)
		}
	case "waiting since":
		t.WaitingSince = value
	case "files":
		t.Files = splitCSV(value)
	case "acceptance":
		t.Acceptance = value
	case "notes":
		t.ExtraFields = addField(t.ExtraFields, "__notes_ref", value)
	case "status":
		t.Status = value
	case "plan":
		t.PlanText = value
	case "active children":
		// Derived under the new model: the child relation is the source.
		// Kept for the converter's cross-check, not stored as a field.
		t.ExtraFields = addField(t.ExtraFields, "__active_children", strings.ToLower(value))
	default:
		t.ExtraFields = addField(t.ExtraFields, label, value)
	}
}

func addField(m map[string]string, k, v string) map[string]string {
	if m == nil {
		m = make(map[string]string)
	}
	m[k] = v
	return m
}

func splitNorm(data []byte) []string {
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
