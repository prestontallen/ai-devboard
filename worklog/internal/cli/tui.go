package cli

import (
	"errors"
	"time"

	"github.com/spf13/cobra"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/parse"
	"github.com/prestontallen/ai-devboard/worklog/internal/pr"
	"github.com/prestontallen/ai-devboard/worklog/internal/tui"
)

func newTUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Args:  cobra.NoArgs,
		Short: "Launch the interactive Bubble Tea view",
		Long: `Opens an alt-screen Bubble Tea app showing Now / Next / Someday with
keyboard navigation (tab to cycle sections, ↑/↓ within, / to filter, q to
quit, ? for full help).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			wd, err := resolveWorkdir()
			if err != nil {
				return err
			}
			doc, err := parse.File(wd.WorkMD())
			if err != nil {
				if errors.Is(err, model.ErrWorkMDMissing) {
					return errWithExit(1, "WORK.md not found at %s", wd.WorkMD())
				}
				return err
			}
			writePR := func(id, value string) (pr.Result, error) {
				return runStorePR(wd, id, value)
			}
			appendNote := func(id, body string) error {
				ss, err := openStoreForWrite(wd)
				if err != nil {
					return err
				}
				defer ss.close()
				_, err = runStoreNoteAppend(ss, id, body, time.Now())
				return err
			}
			moveToWait := func(id string) error {
				_, err := runStoreWait(wd, id, time.Now().Format("2006-01-02"))
				return err
			}
			return tui.Run(wd, doc, writePR, appendNote, moveToWait)
		},
	}
}
