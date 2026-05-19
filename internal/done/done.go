// Package done implements the `worklog done <id>` operation: archiving a
// completed ticket. Writes an archive entry, flips the parent's notes-file
// checkbox (for children), removes the ticket from WORK.md, and surfaces
// whether the parent epic is now ready to archive.
package done

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/prestontallen/day2day/internal/model"
	"github.com/prestontallen/day2day/internal/parse"
	"github.com/prestontallen/day2day/internal/render"
)

// Inputs captures the user-supplied data for a `done` call.
type Inputs struct {
	ID        string
	Summary   string
	Feedback  []string
	Time      string
	PR        string
	Completed string // YYYY-MM-DD; empty = today
}

// Output is the JSON wire shape for the CLI success path.
type Output struct {
	Status          string   `json:"status"`
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	ArchivePath     string   `json:"archivePath"`
	Completed       string   `json:"completed"`
	Parent          string   `json:"parent"`
	EpicCompletable bool     `json:"epicCompletable"`
	Warnings        []string `json:"warnings"`
}

// Sentinel errors. The CLI wrapper maps these to specific exit codes.
var (
	ErrIDNotFound      = errors.New("ticket ID not found in WORK.md")
	ErrCannotDoneEpic  = errors.New("cannot done an epic (epic archival not yet supported)")
	ErrSummaryRequired = errors.New("summary is required")
	ErrInvalidDate     = errors.New("invalid date (expected YYYY-MM-DD)")
)

const indexNotUpdatedWarning = "INDEX.md not updated (deferred to Phase 2B-4 reindex)"

var (
	dateRe         = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	openChildRe    = regexp.MustCompile(`^- \[ \]\s+[a-zA-Z0-9_-]+`)
)

// Run performs the archive operation. Sequence:
//
//  1. Validate inputs (summary required, date format).
//  2. Parse WORK.md and locate the ticket. Refuse if epic or not found.
//  3. Format the archive entry and append it to archive/YYYY-MM.md.
//  4. If the ticket has a Parent: flip the child's checkbox in
//     notes/<parent>.md, then remove the child from the epic's
//     Active children field in WORK.md, then count remaining `[ ]`
//     children for epicCompletable.
//  5. Remove the ticket block from WORK.md.
//
// Each write is atomic via render.WriteAtomic; cross-file atomicity is
// not enforced (a crash mid-sequence leaves a recoverable state — re-run
// converges). Order is chosen so a duplicate is the worst outcome,
// never a lost record.
func Run(wd model.Workdir, in Inputs, today string) (Output, error) {
	if strings.TrimSpace(in.Summary) == "" {
		return Output{}, ErrSummaryRequired
	}
	completed := strings.TrimSpace(in.Completed)
	if completed == "" {
		completed = today
	}
	if !dateRe.MatchString(completed) {
		return Output{}, fmt.Errorf("%w: %q", ErrInvalidDate, completed)
	}
	month := completed[:7]

	doc, err := parse.File(wd.WorkMD())
	if err != nil {
		return Output{}, err
	}

	block := doc.BlockByID(in.ID)
	if block == nil {
		return Output{}, fmt.Errorf("%w: %q", ErrIDNotFound, in.ID)
	}
	if block.IsEpic() {
		return Output{}, fmt.Errorf("%w: %q is an epic", ErrCannotDoneEpic, in.ID)
	}

	var warnings []string

	pr := strings.TrimSpace(in.PR)
	if pr == "" {
		pr = block.PR
	}

	entry := render.FormatArchiveEntry(render.ArchiveOpts{
		ID:        block.ID,
		Title:     block.Title,
		Repo:      block.Repo,
		Tags:      block.Tags,
		PR:        pr,
		Files:     block.Files,
		Parent:    block.Parent,
		Started:   block.Started,
		Completed: completed,
		Summary:   strings.TrimSpace(in.Summary),
		Feedback:  trimEachAndDropEmpty(in.Feedback),
		Time:      strings.TrimSpace(in.Time),
	})

	archivePath := wd.ArchiveFile(month)
	if err := render.AppendToArchive(archivePath, entry, completed, month); err != nil {
		return Output{}, fmt.Errorf("archive write: %w", err)
	}

	var epicCompletable bool
	if block.Parent != "" {
		// Flip the child's notes-file checkbox.
		notesPath := wd.NotesFile(block.Parent)
		notesBytes, readErr := os.ReadFile(notesPath)
		switch {
		case errors.Is(readErr, os.ErrNotExist):
			warnings = append(warnings,
				fmt.Sprintf("notes file %s missing; skipped checkbox flip", notesPath))
		case readErr != nil:
			return Output{}, fmt.Errorf("notes read: %w", readErr)
		default:
			newBytes, found, err := render.FlipChildCheckbox(notesBytes, block.ID)
			if err != nil {
				return Output{}, fmt.Errorf("notes flip: %w", err)
			}
			if !found {
				warnings = append(warnings,
					fmt.Sprintf("notes file %s has no `- [ ]` line for %s; skipped flip",
						notesPath, block.ID))
			} else if err := os.WriteFile(notesPath, newBytes, 0o644); err != nil {
				return Output{}, fmt.Errorf("notes write: %w", err)
			}
		}

		// Remove the child from the epic's Active children. Re-parse so
		// line numbers are fresh.
		freshDoc, err := parse.File(wd.WorkMD())
		if err != nil {
			return Output{}, err
		}
		newLines, err := render.RemoveFromEpicActiveChildren(freshDoc, block.Parent, block.ID)
		switch {
		case errors.Is(err, render.ErrBlockNotFound):
			warnings = append(warnings,
				fmt.Sprintf("parent epic %s not found in WORK.md; skipped Active children update",
					block.Parent))
		case err != nil:
			return Output{}, fmt.Errorf("active children update: %w", err)
		default:
			if writeErr := render.WriteAtomic(wd.WorkMD(), newLines); writeErr != nil {
				return Output{}, fmt.Errorf("WORK.md write: %w", writeErr)
			}
		}

		// Count remaining open children to compute epicCompletable.
		if data, err := os.ReadFile(wd.NotesFile(block.Parent)); err == nil {
			openCount := 0
			for _, line := range strings.Split(string(data), "\n") {
				if openChildRe.MatchString(line) {
					openCount++
				}
			}
			epicCompletable = openCount == 0
		}
	}

	// Final step: remove the ticket block from WORK.md. Re-parse since
	// the file may have been mutated by the active-children update.
	finalDoc, err := parse.File(wd.WorkMD())
	if err != nil {
		return Output{}, err
	}
	afterRemove, _, err := render.RemoveBlock(finalDoc, in.ID)
	if err != nil {
		return Output{}, fmt.Errorf("remove block: %w", err)
	}
	if err := render.WriteAtomic(wd.WorkMD(), afterRemove); err != nil {
		return Output{}, fmt.Errorf("WORK.md final write: %w", err)
	}

	warnings = append(warnings, indexNotUpdatedWarning)

	return Output{
		Status:          "archived",
		ID:              block.ID,
		Title:           block.Title,
		ArchivePath:     archivePath,
		Completed:       completed,
		Parent:          block.Parent,
		EpicCompletable: epicCompletable,
		Warnings:        warnings,
	}, nil
}

func trimEachAndDropEmpty(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
