// Package edit implements `worklog edit <id>`: writing metadata fields on a
// ticket block that already exists in WORK.md.
//
// Every other writer in the corpus owns exactly one field at one moment in a
// ticket's life (start stamps Started, wait stamps Waiting since, pr owns PR).
// edit is the general setter for the fields nothing else claims, so agents
// don't have to reach for a text editor to correct a ticket after the fact.
package edit

import (
	"errors"
	"fmt"
	"strings"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/parse"
	"github.com/prestontallen/ai-devboard/worklog/internal/render"
)

// TitleField is the pseudo-field name for a block's title. It lives on the
// bullet line rather than an indented metadata line, so it takes a different
// write path from everything in render's field table.
const TitleField = "Title"

var (
	// ErrIDNotFound is returned when blockID doesn't resolve to a block.
	ErrIDNotFound = errors.New("ticket ID not found in WORK.md")
	// ErrNoFields is returned when no field was requested.
	ErrNoFields = errors.New("no fields given")
	// ErrNotEditable is returned for a field edit doesn't own.
	ErrNotEditable = errors.New("field is not editable")
	// ErrEmptyTitle is returned for an attempt to clear a title.
	ErrEmptyTitle = errors.New("title cannot be empty")
)

// Assignment is one field write. An empty Value removes the metadata line.
type Assignment struct {
	Field string
	Value string
}

// Change is one applied write, before and after.
type Change struct {
	Field string `json:"field"`
	From  string `json:"from"`
	To    string `json:"to"`
}

// Result is the JSON wire shape for the CLI success path.
type Result struct {
	ID      string   `json:"id"`
	Changes []Change `json:"changes"`
}

// Fields returns the field names edit accepts, in canonical order.
func Fields() []string {
	return append([]string{TitleField}, render.EditableFields()...)
}

// Apply writes each assignment to blockID's block and returns the before/after
// pairs. Assignments are applied in the order given; an assignment whose value
// already matches is still reported, so the caller can tell the field was
// addressed.
//
// WORK.md is written once, at the end, and only if every assignment resolved.
func Apply(wd model.Workdir, blockID string, assignments []Assignment) (Result, error) {
	if len(assignments) == 0 {
		return Result{}, ErrNoFields
	}

	path := wd.WorkMD()
	doc, err := parse.File(path)
	if err != nil {
		return Result{}, err
	}
	b := doc.BlockByID(blockID)
	if b == nil {
		return Result{}, fmt.Errorf("%w: %q", ErrIDNotFound, blockID)
	}

	res := Result{ID: b.ID}
	lines := doc.Lines

	for _, a := range assignments {
		field, err := canonicalize(a.Field)
		if err != nil {
			return Result{}, err
		}
		value := strings.TrimSpace(a.Value)
		if isCSVField(field) {
			value = normalizeCSV(value)
		}
		if field == TitleField && value == "" {
			return Result{}, ErrEmptyTitle
		}

		// Re-parse between writes: each splice shifts the line numbers the
		// next one indexes against.
		d, err := parse.Bytes(path, []byte(strings.Join(lines, "\n")+"\n"))
		if err != nil {
			return Result{}, err
		}
		cur := d.BlockByID(blockID)
		if cur == nil {
			return Result{}, fmt.Errorf("%w: %q", ErrIDNotFound, blockID)
		}
		from := currentValue(cur, field)

		if field == TitleField {
			lines, err = render.SetBlockTitle(d, blockID, value)
		} else {
			lines, err = render.SetBlockField(d, blockID, field, value)
		}
		if err != nil {
			return Result{}, err
		}
		res.Changes = append(res.Changes, Change{Field: field, From: from, To: value})
	}

	if err := render.WriteAtomic(path, lines); err != nil {
		return Result{}, fmt.Errorf("write WORK.md: %w", err)
	}
	return res, nil
}

// canonicalize maps a user-supplied field name to its canonical spelling,
// rejecting anything edit doesn't own.
func canonicalize(field string) (string, error) {
	if strings.EqualFold(field, TitleField) {
		return TitleField, nil
	}
	name, ok := render.CanonicalField(field)
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrNotEditable, field)
	}
	for _, f := range render.EditableFields() {
		if f == name {
			return name, nil
		}
	}
	return "", fmt.Errorf("%w: %q (owned by another command)", ErrNotEditable, field)
}

func isCSVField(field string) bool {
	return field == "Tags" || field == "Files"
}

// normalizeCSV re-joins a comma-separated value the way the block formatters
// render one, so a value written by edit round-trips to the same slice the
// parser would produce for a freshly rendered block.
func normalizeCSV(s string) string {
	var parts []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, ", ")
}

func currentValue(b *model.Block, field string) string {
	switch field {
	case TitleField:
		return b.Title
	case "Repo":
		return b.Repo
	case "Tags":
		return strings.Join(b.Tags, ", ")
	case "Notes":
		return b.NotesRef
	case "Files":
		return strings.Join(b.Files, ", ")
	case "Acceptance":
		return b.Acceptance
	case "Status":
		return b.Status
	}
	return ""
}
