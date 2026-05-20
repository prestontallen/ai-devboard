// Package wait implements the worklog wait/resume operations: moving a ticket
// between ## Now and ## Waiting.
package wait

import (
	"errors"
	"fmt"
	"strings"

	"github.com/prestontallen/day2day/internal/model"
	"github.com/prestontallen/day2day/internal/parse"
	"github.com/prestontallen/day2day/internal/render"
)

var (
	ErrIDNotFound     = errors.New("ticket ID not found")
	ErrNotInNow       = errors.New("ticket is not in ## Now")
	ErrAlreadyWaiting = errors.New("ticket is already in ## Waiting")
	ErrCapExceeded    = errors.New("## Now is at cap")
	ErrNotInWaiting   = errors.New("ticket is not in ## Waiting")
)

const cap = 5

// WaitOutput is the JSON wire shape for a successful Wait call.
type WaitOutput struct {
	Status       string `json:"status"`
	ID           string `json:"id"`
	WaitingSince string `json:"waitingSince"`
	WorkMD       string `json:"workMD"`
}

// ResumeOutput is the JSON wire shape for a successful Resume call.
type ResumeOutput struct {
	Status  string `json:"status"`
	ID      string `json:"id"`
	Section string `json:"section"`
	WorkMD  string `json:"workMD"`
}

// Wait moves a ticket from ## Now → ## Waiting, stamping **Waiting since**:
// with today. Creates ## Waiting before ## Next if absent.
func Wait(wd model.Workdir, id, today string) (WaitOutput, error) {
	id = strings.ToLower(strings.TrimSpace(id))

	doc, err := parse.File(wd.WorkMD())
	if err != nil {
		return WaitOutput{}, err
	}

	block := doc.BlockByID(id)
	if block == nil {
		return WaitOutput{}, fmt.Errorf("%w: %q", ErrIDNotFound, id)
	}
	if block.Section == model.SectionWaiting {
		return WaitOutput{}, fmt.Errorf("%w: %q", ErrAlreadyWaiting, id)
	}
	if block.Section != model.SectionNow {
		return WaitOutput{}, fmt.Errorf("%w: %q is in ## %s", ErrNotInNow, id, block.Section)
	}

	afterRemove, src, err := render.RemoveBlock(doc, id)
	if err != nil {
		return WaitOutput{}, err
	}

	doc2, err := parse.Bytes(doc.Path, []byte(strings.Join(afterRemove, "\n")))
	if err != nil {
		return WaitOutput{}, err
	}

	if doc2.Section(model.SectionWaiting) == nil {
		lines2, err := render.InsertSectionBefore(doc2, model.SectionWaiting, model.SectionNext)
		if err != nil {
			return WaitOutput{}, fmt.Errorf("wait: cannot create ## Waiting section: %w", err)
		}
		doc2, err = parse.Bytes(doc.Path, []byte(strings.Join(lines2, "\n")))
		if err != nil {
			return WaitOutput{}, err
		}
	}

	blockLines := render.FormatTicketBlock(render.BlockOptions{
		Title:        src.Title,
		ID:           src.ID,
		Type:         string(src.Type),
		Parent:       src.Parent,
		Repo:         src.Repo,
		Tags:         src.Tags,
		Started:      src.Started,
		PR:           src.PR,
		Files:        src.Files,
		Acceptance:   src.Acceptance,
		NotesRef:     src.NotesRef,
		Status:       src.Status,
		WaitingSince: today,
		State:        src.State,
	})

	final, err := render.AppendToSection(doc2, model.SectionWaiting, blockLines)
	if err != nil {
		return WaitOutput{}, err
	}

	if err := render.WriteAtomic(wd.WorkMD(), final); err != nil {
		return WaitOutput{}, err
	}

	return WaitOutput{
		Status:       "waiting",
		ID:           id,
		WaitingSince: today,
		WorkMD:       wd.WorkMD(),
	}, nil
}

// Resume moves a ticket from ## Waiting → ## Now (cap-checked), clearing
// **Waiting since**:.
func Resume(wd model.Workdir, id, today string) (ResumeOutput, error) {
	id = strings.ToLower(strings.TrimSpace(id))

	doc, err := parse.File(wd.WorkMD())
	if err != nil {
		return ResumeOutput{}, err
	}

	block := doc.BlockByID(id)
	if block == nil {
		return ResumeOutput{}, fmt.Errorf("%w: %q", ErrIDNotFound, id)
	}
	if block.Section != model.SectionWaiting {
		return ResumeOutput{}, fmt.Errorf("%w: %q is in ## %s", ErrNotInWaiting, id, block.Section)
	}

	nowCount, nowIDs := nowSnapshot(doc)
	if nowCount >= cap {
		return ResumeOutput{}, fmt.Errorf("%w (%d/%d); current Now: %s",
			ErrCapExceeded, nowCount, cap, strings.Join(nowIDs, ", "))
	}

	afterRemove, src, err := render.RemoveBlock(doc, id)
	if err != nil {
		return ResumeOutput{}, err
	}

	doc2, err := parse.Bytes(doc.Path, []byte(strings.Join(afterRemove, "\n")))
	if err != nil {
		return ResumeOutput{}, err
	}

	blockLines := render.FormatTicketBlock(render.BlockOptions{
		Title:        src.Title,
		ID:           src.ID,
		Type:         string(src.Type),
		Parent:       src.Parent,
		Repo:         src.Repo,
		Tags:         src.Tags,
		Started:      src.Started,
		PR:           src.PR,
		Files:        src.Files,
		Acceptance:   src.Acceptance,
		NotesRef:     src.NotesRef,
		Status:       src.Status,
		WaitingSince: "", // cleared
		State:        src.State,
	})

	final, err := render.AppendToSection(doc2, model.SectionNow, blockLines)
	if err != nil {
		return ResumeOutput{}, err
	}

	if err := render.WriteAtomic(wd.WorkMD(), final); err != nil {
		return ResumeOutput{}, err
	}

	return ResumeOutput{
		Status:  "resumed",
		ID:      id,
		Section: "Now",
		WorkMD:  wd.WorkMD(),
	}, nil
}

func nowSnapshot(doc *model.WorkDoc) (int, []string) {
	now := doc.Section(model.SectionNow)
	if now == nil {
		return 0, nil
	}
	ids := make([]string, 0, len(now.Blocks))
	for _, b := range now.Blocks {
		if b.ID != "" {
			ids = append(ids, b.ID)
		}
	}
	return len(now.Blocks), ids
}
