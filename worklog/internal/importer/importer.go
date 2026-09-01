package importer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/prestontallen/day2day/internal/model"
	"github.com/prestontallen/day2day/internal/parse"
	"github.com/prestontallen/day2day/internal/render"
)

func decodeJSON(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

// Ticket is one ticket to import.
type Ticket struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Type    string   `json:"type,omitempty"`
	Parent  string   `json:"parent,omitempty"`
	Repo    string   `json:"repo,omitempty"`
	Tags    []string `json:"tags,omitempty"`
	PR      string   `json:"pr,omitempty"`
	Section string   `json:"section,omitempty"` // now/next/someday
	Source  string   `json:"source,omitempty"`
}

// Imported describes one successfully-imported ticket.
type Imported struct {
	ID      string `json:"id"`
	Section string `json:"section"`
}

// Failed describes one ticket that failed to import.
type Failed struct {
	Index  int    `json:"index"`
	ID     string `json:"id,omitempty"`
	Reason string `json:"reason"`
}

// Result bundles per-ticket outcomes.
type Result struct {
	Imported []Imported `json:"imported"`
	Failed   []Failed   `json:"failed"`
}

// Options bundles per-call overrides.
type Options struct {
	SectionOverride string    // overrides every ticket's Section when non-empty
	DryRun          bool      // validate without writing
	Now             time.Time // injected for deterministic tests
}

var (
	ErrIDRequired     = errors.New("id is required")
	ErrTitleRequired  = errors.New("title is required")
	ErrInvalidSection = errors.New("section must be one of: now, next, someday")
	ErrInvalidType    = errors.New("type must be one of: ticket, epic, spike, chore")
	ErrParentMissing  = errors.New("parent epic not found in WORK.md")
	ErrParentNotEpic  = errors.New("parent block is not an epic")
	ErrAlreadyExists  = errors.New("ticket id already exists in WORK.md")
)

// Decode reads JSON from r. Accepts a single object or an array.
func Decode(r io.Reader) ([]Ticket, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, nil
	}
	switch trimmed[0] {
	case '[':
		var tickets []Ticket
		if err := decodeJSON(trimmed, &tickets); err != nil {
			return nil, fmt.Errorf("expected JSON object or array: %w", err)
		}
		return tickets, nil
	case '{':
		var t Ticket
		if err := decodeJSON(trimmed, &t); err != nil {
			return nil, fmt.Errorf("expected JSON object or array: %w", err)
		}
		return []Ticket{t}, nil
	default:
		return nil, fmt.Errorf("expected JSON object or array")
	}
}

// Import validates each ticket and writes valid ones to WORK.md. Each ticket
// is its own atomic write; partial success is possible.
func Import(wd model.Workdir, tickets []Ticket, opts Options) (Result, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	today := now.Format("2006-01-02")

	result := Result{
		Imported: []Imported{},
		Failed:   []Failed{},
	}

	for i, t := range tickets {
		id, section, reason := importOne(wd, t, opts.SectionOverride, today, opts.DryRun)
		if reason != "" {
			result.Failed = append(result.Failed, Failed{
				Index:  i,
				ID:     t.ID,
				Reason: reason,
			})
			continue
		}
		result.Imported = append(result.Imported, Imported{ID: id, Section: section})
	}
	return result, nil
}

// importOne validates and writes a single ticket. Returns the canonical id and
// section on success, or a non-empty reason string on failure.
func importOne(wd model.Workdir, t Ticket, sectionOverride, today string, dryRun bool) (id, section, reason string) {
	// Normalize.
	id = strings.ToLower(strings.TrimSpace(t.ID))
	if id == "" {
		return "", "", ErrIDRequired.Error()
	}
	title := strings.TrimSpace(t.Title)
	if title == "" {
		return id, "", ErrTitleRequired.Error()
	}
	ticketType := strings.ToLower(strings.TrimSpace(t.Type))
	if ticketType == "" {
		ticketType = "ticket"
	}
	switch ticketType {
	case "ticket", "epic", "spike", "chore":
	default:
		return id, "", ErrInvalidType.Error()
	}

	section = sectionOverride
	if section == "" {
		section = strings.ToLower(strings.TrimSpace(t.Section))
	}
	if section == "" {
		section = "next"
	}
	switch section {
	case "now", "next", "someday":
	default:
		return id, section, ErrInvalidSection.Error()
	}

	parent := strings.ToLower(strings.TrimSpace(t.Parent))

	// Load WORK.md fresh per ticket.
	doc, err := parse.File(wd.WorkMD())
	if err != nil {
		return id, section, fmt.Sprintf("read WORK.md: %v", err)
	}

	if doc.BlockByID(id) != nil {
		return id, section, ErrAlreadyExists.Error()
	}

	if parent != "" {
		pb := doc.BlockByID(parent)
		if pb == nil {
			return id, section, ErrParentMissing.Error()
		}
		if !pb.IsEpic() {
			return id, section, ErrParentNotEpic.Error()
		}
	}

	if dryRun {
		return id, section, ""
	}

	// Map section to model types.
	var (
		sectionName model.SectionName
		state       model.State
		started     string
	)
	switch section {
	case "now":
		sectionName = model.SectionNow
		state = model.StateActive
		started = today
	case "next":
		sectionName = model.SectionNext
		state = model.StatePending
	case "someday":
		sectionName = model.SectionSomeday
		state = model.StatePending
	}

	// Build and append the block.
	blockLines := render.FormatTicketBlock(render.BlockOptions{
		Title:   title,
		ID:      id,
		Type:    ticketType,
		Parent:  parent,
		Repo:    t.Repo,
		Tags:    t.Tags,
		PR:      t.PR,
		Source:  t.Source,
		Started: started,
		State:   state,
	})

	newLines, err := render.AppendToSection(doc, sectionName, blockLines)
	if err != nil {
		return id, section, fmt.Sprintf("append block: %v", err)
	}
	if err := render.WriteAtomic(wd.WorkMD(), newLines); err != nil {
		return id, section, fmt.Sprintf("write WORK.md: %v", err)
	}

	// Update parent epic's Active children if applicable.
	if parent != "" {
		doc2, err := parse.File(wd.WorkMD())
		if err != nil {
			return id, section, fmt.Sprintf("re-read WORK.md: %v", err)
		}
		updated, err := render.UpdateEpicActiveChildren(doc2, parent, id)
		if err != nil {
			return id, section, fmt.Sprintf("update active children: %v", err)
		}
		if err := render.WriteAtomic(wd.WorkMD(), updated); err != nil {
			return id, section, fmt.Sprintf("write WORK.md (active children): %v", err)
		}
	}

	return id, section, ""
}
