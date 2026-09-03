package cli

import (
	"fmt"
	"strings"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

// storeAddErrIDExists mirrors add.go's inline "ID %q already exists"
// error, kept as a value here so the store path can format it identically
// without depending on add.go's unexported string.
func storeAddErrIDExists(id string) error {
	return fmt.Errorf("ID %q already exists", id)
}

// runStoreAdd is add's store-backed twin (adb-cutover M3d), covering all
// three legacy branches (standalone / epic / child) as one PutTicket:
// under the store model a "child of an epic" is a full ticket row from
// the moment it's added (ParentID set, Section "" until started), not a
// notes-file checkbox line — the design panel's field-porting rationale
// for start applies here too, one step earlier. inputs.Type must already
// be validated and inputs.Section/inputs.Parent normalized by the caller
// (runAdd's existing validation, unchanged and shared by both backends).
func runStoreAdd(wd model.Workdir, inputs addInputs) (addOutput, error) {
	ss, err := openStoreForWrite(wd)
	if err != nil {
		return addOutput{}, err
	}
	defer ss.close()

	if _, err := ss.s.TicketBySlug(inputs.ID); err == nil {
		return addOutput{}, storeAddErrIDExists(inputs.ID)
	} else if !store.IsNotFound(err) {
		return addOutput{}, err
	}

	t := &store.Ticket{
		Slug:  inputs.ID,
		Title: inputs.Title,
		Type:  inputs.Type,
		Repo:  inputs.Repo,
		Tags:  inputs.Tags,
		State: store.StatePending,
	}

	if inputs.Parent != "" {
		parent, err := ss.s.TicketBySlug(inputs.Parent)
		if store.IsNotFound(err) {
			return addOutput{}, fmt.Errorf("%w: %q", ErrParentEpicNotFound, inputs.Parent)
		}
		if err != nil {
			return addOutput{}, err
		}
		if parent.Type != store.TypeEpic || parent.Archived {
			return addOutput{}, fmt.Errorf("%w: %q", ErrParentEpicNotFound, inputs.Parent)
		}
		t.ParentID = parent.ID
		// Section stays "" — a child doesn't occupy a WORK.md section
		// until `start` promotes it, matching legacy (add --parent writes
		// no metadata beyond id/title either).

		if err := ss.commit(t); err != nil {
			return addOutput{}, err
		}
		return addOutput{
			Status:    "added",
			Kind:      "child",
			ID:        t.Slug,
			Title:     t.Title,
			Parent:    parent.Slug,
			NotesPath: wd.NotesFile(parent.Slug),
		}, nil
	}

	t.Section = strings.ToLower(inputs.Section)
	t.Acceptance = inputs.Acceptance

	if inputs.Type == store.TypeEpic {
		t.NotesPreamble = epicScaffoldSeed(inputs.Title, inputs.ID)
		if err := ss.commit(t); err != nil {
			return addOutput{}, err
		}
		return addOutput{
			Status:    "added",
			Kind:      "epic",
			ID:        t.Slug,
			Title:     t.Title,
			Section:   inputs.Section,
			WorkMD:    wd.WorkMD(),
			NotesPath: wd.NotesFile(t.Slug),
		}, nil
	}

	if err := ss.commit(t); err != nil {
		return addOutput{}, err
	}
	return addOutput{
		Status:  "added",
		Kind:    "ticket",
		ID:      t.Slug,
		Title:   t.Title,
		Section: inputs.Section,
		WorkMD:  wd.WorkMD(),
	}, nil
}

// epicScaffoldSeed is epicScaffold's store-model counterpart: no
// "## Children" roster, since that list is not kept current under the
// store (children render from the ParentID relation on the board/feed,
// not from hand-maintained checkbox lines in the notes preamble — a
// roster here would just go stale).
func epicScaffoldSeed(title, id string) string {
	return fmt.Sprintf(`# %s

<!-- Notes for epic %s. Children are tracked automatically (add --parent
     %s, then worklog start <child-id>); no roster list to maintain here. -->

## Background

(Fill in: Jira link, plan reference, open questions.)`, title, id, id)
}
