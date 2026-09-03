package cli

import (
	"fmt"
	"strings"

	"github.com/prestontallen/ai-devboard/worklog/internal/edit"
	"github.com/prestontallen/ai-devboard/worklog/internal/model"
)

// runStoreEdit is edit.Apply's store-backed twin (adb-cutover M3d).
// Assignments apply in order against one fetched *store.Ticket, each
// producing a before/after Change, then commit once — matching
// edit.Apply's "write once, at the end" contract, just against the store
// instead of a WORK.md splice.
//
// The Notes field is refused here rather than silently dropped: under
// the store model a ticket's notes reference is fully derived
// ("notes/<slug>.md", present whenever the ticket has notes content),
// not a settable field, so honoring --notes would either be a silent
// no-op or a lie about what got written. It refuses via the same
// ErrNotEditable path edit.Apply already uses for fields it doesn't own.
func runStoreEdit(wd model.Workdir, id string, assignments []edit.Assignment) (edit.Result, error) {
	ss, err := openStoreForWrite(wd)
	if err != nil {
		return edit.Result{}, err
	}
	defer ss.close()

	t, err := ss.ticketBySlugOrErr(id, edit.ErrIDNotFound)
	if err != nil {
		return edit.Result{}, err
	}

	res := edit.Result{ID: t.Slug}
	for _, a := range assignments {
		value := strings.TrimSpace(a.Value)
		var from string
		switch a.Field {
		case edit.TitleField:
			if value == "" {
				return edit.Result{}, edit.ErrEmptyTitle
			}
			from = t.Title
			t.Title = value
		case "Repo":
			from = t.Repo
			t.Repo = value
		case "Tags":
			from = strings.Join(t.Tags, ", ")
			t.Tags = splitTags(value)
			value = strings.Join(t.Tags, ", ")
		case "Files":
			from = strings.Join(t.Files, ", ")
			t.Files = splitTags(value)
			value = strings.Join(t.Files, ", ")
		case "Acceptance":
			from = t.Acceptance
			t.Acceptance = value
		case "Status":
			from = t.Status
			t.Status = value
		case "Notes":
			return edit.Result{}, fmt.Errorf(
				"%w: %q (notes ref is derived automatically from ticket content under the store; not settable)",
				edit.ErrNotEditable, "Notes")
		default:
			return edit.Result{}, fmt.Errorf("%w: %q", edit.ErrNotEditable, a.Field)
		}
		res.Changes = append(res.Changes, edit.Change{Field: a.Field, From: from, To: value})
	}

	if err := ss.commit(t); err != nil {
		return edit.Result{}, err
	}
	return res, nil
}
