package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/parse"
	"github.com/prestontallen/ai-devboard/worklog/internal/validate"
)

// Claude Code hook wire types. The harness reads one JSON document from
// stdout; unknown fields are ignored.
//
// additionalContext is emitted BOTH inside hookSpecificOutput and at the
// top level on purpose: the published schema shows it as a sibling of
// hookSpecificOutput while every working example nests it, and a hook that
// guesses wrong injects nothing at all. Duplicating costs a few bytes and
// removes the guess.
type hookSessionStartOutput struct {
	HookSpecificOutput hookSpecificOutput `json:"hookSpecificOutput"`
	AdditionalContext  string             `json:"additionalContext"`
}

type hookSpecificOutput struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext"`
}

func newHookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hook",
		Args:  cobra.NoArgs,
		Short: "Emit Claude Code hook payloads (agent-invoked, not for humans)",
		Long: `hook implements the Claude Code hook contract. Subcommands are
invoked by the harness, not by hand: each reads the hook's JSON on stdin and
writes one JSON document to stdout.

Install the SessionStart entry with an interactive ` + "`worklog install`" + `,
which offers to merge it into ~/.claude/settings.json.`,
	}
	cmd.AddCommand(newHookSessionStartCmd())
	return cmd
}

func newHookSessionStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "session-start",
		Args:  cobra.NoArgs,
		Short: "Inject the worklog orientation block at session start",
		Long: `session-start renders a compact orientation block — what is in
Now, which epics have active children, how long anything has been waiting —
as SessionStart additionalContext.

It replaces the worklog skill's prose instruction to run 'status --json' and
orient, which measurably did not fire in sessions about other projects.

It ALWAYS exits 0. A SessionStart hook that exits 2 blocks session
initialization, so a missing or unreadable WORK.md is reported inside the
context block rather than as a failure.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHookSessionStart(cmd)
		},
	}
}

func runHookSessionStart(cmd *cobra.Command) error {
	drainHookStdin()

	ctx := orientContext(time.Now())
	out := hookSessionStartOutput{
		HookSpecificOutput: hookSpecificOutput{
			HookEventName:     "SessionStart",
			AdditionalContext: ctx,
		},
		AdditionalContext: ctx,
	}
	// A write failure has nowhere to go: returning an error would exit
	// non-zero and put a notice in front of the human for no benefit.
	_ = emitJSON(cmd.OutOrStdout(), out)
	return nil
}

// drainHookStdin consumes the harness's JSON payload so the writer never
// sees EPIPE. Nothing in it is needed today — the block is the same for
// every matcher — but leaving it unread is how you get a broken pipe in the
// harness's log. Skipped on a TTY, where ReadAll would block forever.
func drainHookStdin() {
	if stdinIsTTY() {
		return
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
}

// orientContext renders the block the agent sees. Line one always prints so
// a session can tell "nothing in Now" from "the hook never ran".
func orientContext(now time.Time) string {
	wd, err := resolveWorkdir()
	if err != nil {
		return fmt.Sprintf("worklog: unavailable (%v)", err)
	}
	doc, err := parse.File(wd.WorkMD())
	if err != nil {
		if errors.Is(err, model.ErrWorkMDMissing) {
			return fmt.Sprintf("worklog: WORK.md not found at %s — the data dir is missing or uninitialized", wd.WorkMD())
		}
		return fmt.Sprintf("worklog: could not read %s (%v)", wd.WorkMD(), err)
	}

	var b strings.Builder
	nowSec := doc.Section(model.SectionNow)
	nowBlocks := []model.Block{}
	if nowSec != nil {
		nowBlocks = nowSec.Blocks
	}
	if len(nowBlocks) == 0 {
		fmt.Fprintf(&b, "worklog: nothing in Now (cap %d)", validate.NowCap)
	} else {
		fmt.Fprintf(&b, "worklog: %d in Now (cap %d)", len(nowBlocks), validate.NowCap)
	}
	for _, blk := range nowBlocks {
		fmt.Fprintf(&b, "\n  [%s] %s — %s", string(blk.State), blk.ID, blk.Title)
	}

	if next := doc.Section(model.SectionNext); next != nil {
		for _, blk := range next.Blocks {
			if blk.IsEpic() && len(blk.ActiveChildren) > 0 {
				fmt.Fprintf(&b, "\n  epic %s — active children: %s",
					blk.ID, strings.Join(blk.ActiveChildren, ", "))
			}
		}
	}

	if waiting := doc.Section(model.SectionWaiting); waiting != nil && len(waiting.Blocks) > 0 {
		maxAge := 0
		for _, blk := range waiting.Blocks {
			if age := waitingAge(blk.WaitingSince, now); age > maxAge {
				maxAge = age
			}
		}
		fmt.Fprintf(&b, "\n  waiting: %d", len(waiting.Blocks))
		if maxAge > 0 {
			fmt.Fprintf(&b, ", oldest %d days", maxAge)
		}
	}

	return b.String()
}
