package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/prestontallen/ai-devboard/worklog/internal/importer"
	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

// runStoreImport is importer.Import's store-backed twin (adb-cutover
// M3d). Each ticket is validated and, on success, PutTicket'd
// immediately — not batched — so a later ticket in the same JSON array
// that names an earlier one as --parent resolves it, matching
// importOne's per-ticket re-parse-and-see-prior-writes behavior. Unlike
// per-verb writes elsewhere, projections render once after the whole
// batch rather than once per ticket: mid-batch disk state was never
// user-visible under the legacy path either (one CLI invocation, atomic
// from the outside).
func runStoreImport(wd model.Workdir, tickets []importer.Ticket, opts importer.Options) (importer.Result, error) {
	ss, err := openStoreForWrite(wd)
	if err != nil {
		return importer.Result{}, err
	}
	defer ss.close()

	today := opts.Now
	if today.IsZero() {
		today = time.Now()
	}

	result := importer.Result{Imported: []importer.Imported{}, Failed: []importer.Failed{}}
	wrote := false

	for i, in := range tickets {
		id, section, reason := storeImportOne(ss, in, opts.SectionOverride, opts.DryRun, today.Format("2006-01-02"))
		if reason != "" {
			result.Failed = append(result.Failed, importer.Failed{Index: i, ID: in.ID, Reason: reason})
			continue
		}
		if !opts.DryRun {
			wrote = true
		}
		result.Imported = append(result.Imported, importer.Imported{ID: id, Section: section})
	}

	if wrote {
		if err := ss.render(); err != nil {
			return importer.Result{}, err
		}
	}
	return result, nil
}

// storeImportOne validates and (unless dryRun) writes a single ticket
// against the store, mirroring importOne's rules exactly: id/title
// required, type defaults to ticket, section defaults to next, parent
// (if given) must resolve to a live, non-archived epic, and the id must
// not already exist.
func storeImportOne(ss *storeSession, in importer.Ticket, sectionOverride string, dryRun bool, today string) (id, section, reason string) {
	id = strings.ToLower(strings.TrimSpace(in.ID))
	if id == "" {
		return "", "", importer.ErrIDRequired.Error()
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return id, "", importer.ErrTitleRequired.Error()
	}
	ticketType := strings.ToLower(strings.TrimSpace(in.Type))
	if ticketType == "" {
		ticketType = store.TypeTicket
	}
	switch ticketType {
	case store.TypeTicket, store.TypeEpic, store.TypeSpike, store.TypeChore:
	default:
		return id, "", importer.ErrInvalidType.Error()
	}

	section = sectionOverride
	if section == "" {
		section = strings.ToLower(strings.TrimSpace(in.Section))
	}
	if section == "" {
		section = "next"
	}
	switch section {
	case "now", "next", "someday":
	default:
		return id, section, importer.ErrInvalidSection.Error()
	}

	parentSlug := strings.ToLower(strings.TrimSpace(in.Parent))

	if _, err := ss.s.TicketBySlug(id); err == nil {
		return id, section, importer.ErrAlreadyExists.Error()
	} else if !store.IsNotFound(err) {
		return id, section, err.Error()
	}

	var parentID store.ID
	if parentSlug != "" {
		parent, err := ss.s.TicketBySlug(parentSlug)
		if store.IsNotFound(err) {
			return id, section, importer.ErrParentMissing.Error()
		}
		if err != nil {
			return id, section, err.Error()
		}
		if parent.Type != store.TypeEpic {
			return id, section, importer.ErrParentNotEpic.Error()
		}
		if parent.Archived {
			return id, section, fmt.Sprintf("%v: %q is archived; archived epics cannot take new children", importer.ErrParentMissing, parentSlug)
		}
		parentID = parent.ID
	}

	if dryRun {
		return id, section, ""
	}

	t := &store.Ticket{
		Slug:     id,
		Title:    title,
		Type:     ticketType,
		ParentID: parentID,
		Repo:     in.Repo,
		Tags:     in.Tags,
		Source:   in.Source,
		Section:  section,
		State:    store.StatePending,
	}
	if pr := strings.TrimSpace(in.PR); pr != "" {
		t.PR = &pr
	}
	if section == "now" {
		t.State = store.StateActive
		t.Started = today
	}

	if err := ss.s.PutTicket(t); err != nil {
		return id, section, err.Error()
	}
	return id, section, ""
}
