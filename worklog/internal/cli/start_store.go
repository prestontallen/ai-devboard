package cli

import (
	"fmt"
	"strings"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/start"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
	"github.com/prestontallen/ai-devboard/worklog/internal/wait"
)

const startCap = 5

// runStoreStart is start's store-backed twin (adb-cutover M3d): field-
// assignment porting of start.Run plus the resume-from-Waiting fast path
// cli/start.go otherwise handles separately via wait.Resume. Both share
// one *store.Ticket fetch here since, under the store model, a "child of
// an epic" is already its own row (ParentID set, Section "") rather than
// a notes-file checkbox scan target — the ResChildOfEpic/ResStandalone
// split in start.Run collapses into one path, matching the M3 design
// panel's finding that this promotion becomes the same path as
// standalone once Active-children is derived rather than stored.
//
// Returns either a start.Output (normal promotion) or a
// wait.ResumeOutput (resume-from-Waiting fast path) so the CLI's existing
// JSON/text emission code — built against those exact types — works
// unchanged for both backends.
func runStoreStart(wd model.Workdir, id, flagRepo, flagTagsCSV, flagAcceptance, today string) (any, error) {
	normID := strings.ToLower(strings.TrimSpace(id))

	ss, err := openStoreForWrite(wd)
	if err != nil {
		return nil, err
	}
	defer ss.close()

	t, err := ss.ticketBySlugOrErr(normID, start.ErrIDNotFound)
	if err != nil {
		return nil, err
	}

	if t.Section == store.SectionWaiting {
		nowCount, nowSlugs, err := storeNowSnapshot(ss)
		if err != nil {
			return nil, err
		}
		if nowCount >= startCap {
			return nil, fmt.Errorf("%w (%d/%d); current Now: %s",
				wait.ErrCapExceeded, nowCount, startCap, strings.Join(nowSlugs, ", "))
		}
		t.Section = store.SectionNow
		t.WaitingSince = ""
		ensureBoardTracked(t, t.Repo)
		if err := ensureParentBoardTracked(ss, t); err != nil {
			return nil, err
		}
		if err := ss.commit(t); err != nil {
			return nil, err
		}
		return wait.ResumeOutput{Status: "resumed", ID: t.Slug, Section: "Now", WorkMD: wd.WorkMD()}, nil
	}

	if t.Type == store.TypeEpic {
		startable, err := storeStartableChildren(ss, t.ID)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%w; epic %q %s", start.ErrEpicCannotStart, normID, describeStartable(startable))
	}
	if t.Section == store.SectionNow && t.State == store.StateActive {
		return nil, fmt.Errorf("%w: %q is already in ## Now", start.ErrAlreadyStarted, normID)
	}

	if t.ParentID != "" {
		parent, err := ss.s.Ticket(t.ParentID)
		if err != nil || parent.Archived {
			return nil, fmt.Errorf("%w: %q not found and not in the archive; archived epics cannot take new work",
				start.ErrParentEpicGone, t.ParentID)
		}
	}

	movingFromNow := t.Section == store.SectionNow
	if !movingFromNow {
		nowCount, nowSlugs, err := storeNowSnapshot(ss)
		if err != nil {
			return nil, err
		}
		if nowCount >= startCap {
			return nil, fmt.Errorf("%w (%d/%d); current Now: %s",
				start.ErrCapExceeded, nowCount, startCap, strings.Join(nowSlugs, ", "))
		}
	}

	if repo := strings.TrimSpace(flagRepo); repo != "" {
		t.Repo = repo
	}
	if tags := splitTags(flagTagsCSV); len(tags) > 0 {
		t.Tags = tags
	}
	if acc := strings.TrimSpace(flagAcceptance); acc != "" {
		t.Acceptance = acc
	}
	t.Section = store.SectionNow
	t.State = store.StateActive
	t.Started = today
	ensureBoardTracked(t, t.Repo)
	if err := ensureParentBoardTracked(ss, t); err != nil {
		return nil, err
	}

	if err := ss.commit(t); err != nil {
		return nil, err
	}

	var parentSlug string
	if t.ParentID != "" {
		if p, err := ss.s.Ticket(t.ParentID); err == nil {
			parentSlug = p.Slug
		}
	}

	return start.Output{
		Status:  "started",
		ID:      t.Slug,
		Title:   t.Title,
		Section: "Now",
		Parent:  parentSlug,
		Type:    ticketTypeForOutput(t.Type),
		Repo:    t.Repo,
		Started: today,
		WorkMD:  wd.WorkMD(),
	}, nil
}

// ticketTypeForOutput mirrors the legacy "ticket" default being omitted
// from JSON output (Output.Type is `omitempty`, and an ordinary ticket
// is meant to render as absent, not as the literal string "ticket").
func ticketTypeForOutput(t string) string {
	if t == store.TypeTicket {
		return ""
	}
	return t
}

// storeNowSnapshot returns the count and slugs of every live, non-archived
// ticket currently in ## Now, for cap enforcement and error messages.
func storeNowSnapshot(ss *storeSession) (int, []string, error) {
	tickets, err := ss.s.Tickets()
	if err != nil {
		return 0, nil, err
	}
	var slugs []string
	for _, t := range tickets {
		if !t.Archived && t.Section == store.SectionNow {
			slugs = append(slugs, t.Slug)
		}
	}
	return len(slugs), slugs, nil
}

// storeStartableChildren returns an epic's open (non-archived, non-done)
// children that are not already in ## Now, for the ErrEpicCannotStart
// message.
func storeStartableChildren(ss *storeSession, epicID store.ID) ([]string, error) {
	kids, err := ss.s.Children(epicID)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, k := range kids {
		if k.Open() && k.Section != store.SectionNow {
			out = append(out, k.Slug)
		}
	}
	return out, nil
}

func describeStartable(startable []string) string {
	if len(startable) == 0 {
		return "has no startable children (none open, or all already in ## Now)"
	}
	return "has startable children: " + strings.Join(startable, ", ")
}
