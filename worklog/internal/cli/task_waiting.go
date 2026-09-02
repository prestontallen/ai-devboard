package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/prestontallen/day2day/internal/devboard"
	"github.com/prestontallen/day2day/internal/note"
)

// newTaskWaitingOnCmd manages the external-answer queue: questions blocked
// on other teams/people, expected to sit for days (vs needs-you, blocked on
// the task's own human). See devboard/schema.md.
func newTaskWaitingOnCmd(id *string, force *bool, asJSON *bool) *cobra.Command {
	var flagWho, flagLink, flagDetail, flagAsked, flagAnswer string
	cmd := &cobra.Command{
		Use:   "waiting-on <add <text> | resolve <n|all>>",
		Args:  cobra.ExactArgs(2),
		Short: "Track questions blocked on external parties (who, age, answer)",
		Long: `waiting-on tracks questions owed by someone OUTSIDE this task — other
teams, other people. Entries carry who owes the answer and when they were
asked, and the dashboard renders their age.

add requires --who: a question without an owner is the failure mode this
queue exists to prevent.

resolve <n> --answer "..." removes the entry, records the answer as a task
decision (atomic with the removal), and appends it to the worklog ticket's
notes file when reachable — answers become system-of-record, the one
sanctioned devboard→worklog write. resolve without --answer records a
"closed unanswered" decision. resolve all converts every entry to an
"unanswered at close" decision (the ticketless close-out path).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			verb, arg := args[0], args[1]
			switch verb {
			case "add":
				who := strings.TrimSpace(flagWho)
				if who == "" {
					return jsonOrTextError(cmd, *asJSON, 64,
						"task waiting-on add: --who is required (who owes this answer?)")
				}
				asked := strings.TrimSpace(flagAsked)
				if asked == "" {
					asked = time.Now().Format("2006-01-02")
				}
				return mutateTask(cmd, *id, *asJSON, true, *force, "waiting-on added", arg+" → "+who,
					func(t *devboard.Task) error {
						t.WaitingOn = append(t.WaitingOn, devboard.WaitingItem{
							Text: arg, Who: who, Asked: asked,
							Link: flagLink, Detail: flagDetail})
						return nil
					})
			case "resolve":
				return runWaitingOnResolve(cmd, *id, *asJSON, *force, arg, strings.TrimSpace(flagAnswer))
			default:
				return jsonOrTextError(cmd, *asJSON, 64,
					"task waiting-on: unknown verb %q (add|resolve)", verb)
			}
		},
	}
	cmd.Flags().StringVar(&flagWho, "who", "", "who owes the answer (required for add)")
	cmd.Flags().StringVar(&flagLink, "link", "", "where the question was asked (URL)")
	cmd.Flags().StringVar(&flagDetail, "detail", "", "context the answerer needs")
	cmd.Flags().StringVar(&flagAsked, "asked", "", "backfill the asked date (YYYY-MM-DD; default today)")
	cmd.Flags().StringVar(&flagAnswer, "answer", "", "the received answer (recorded as a decision + worklog notes)")
	return cmd
}

// runWaitingOnResolve removes entry n (or all). Ordering per contract:
// the decision recording the outcome is written inside the SAME atomic
// Mutate that removes the entry — an entry is never deleted without a
// record. The worklog-notes append runs afterward, best-effort, warn-only.
func runWaitingOnResolve(cmd *cobra.Command, id string, asJSON, force bool, arg, answer string) error {
	if taskDisabled(cmd) {
		return nil
	}
	path, err := resolveTaskPath(id, false, force)
	if err != nil {
		if ec, ok := err.(exitCoder); ok {
			return jsonOrTextError(cmd, asJSON, ec.ExitCode(), "%v", err)
		}
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}

	today := time.Now().Format("2006-01-02")
	var resolved []devboard.WaitingItem // captured for the notes append
	var worklogID string
	mutErr := devboard.Mutate(path, func(t *devboard.Task) error {
		worklogID = t.Worklog
		if arg == "all" {
			resolved = nil // "all" is the close-out path: decisions say unanswered
			devboard.CloseWaitingOn(t, today)
			return nil
		}
		i, err := index1(arg, len(t.WaitingOn), "waiting-on")
		if err != nil {
			return err
		}
		w := t.WaitingOn[i]
		t.WaitingOn = append(t.WaitingOn[:i], t.WaitingOn[i+1:]...)
		if answer != "" {
			resolved = []devboard.WaitingItem{w}
			t.Decision = append(t.Decision, devboard.Decision{
				What: w.Who + " answered: " + answer,
				Why:  "asked " + w.Asked + ": " + w.Text,
				When: today,
			})
		} else {
			t.Decision = append(t.Decision, devboard.Decision{
				What: "closed unanswered: " + w.Text + " (" + w.Who + ")",
				When: today,
			})
		}
		return nil
	})
	if mutErr != nil {
		code := 1
		if ec, ok := mutErr.(exitCoder); ok {
			code = ec.ExitCode()
		}
		return jsonOrTextError(cmd, asJSON, code, "%v", mutErr)
	}

	// Best-effort system-of-record append. Never fails the resolve: the
	// decision above is already the durable in-task record.
	detail := "resolved"
	if answer != "" && len(resolved) == 1 {
		detail = appendAnswerToWorklog(cmd, worklogID, resolved[0], answer)
	}

	rel, _ := filepath.Rel(devboard.DataDir(), path)
	return emitTaskResult(cmd, asJSON, taskResult{
		File: rel, Action: "waiting-on resolved", Detail: detail})
}

// appendAnswerToWorklog writes the answer into the ticket's notes file —
// the one sanctioned devboard→worklog write (see devboard/schema.md). It
// returns a short human status and warns (never errors) when the system of
// record is unreachable: no worklog id, no worklog dir, or an archived
// ticket with no notes file. In every such case the task-file decision
// already holds the answer.
func appendAnswerToWorklog(cmd *cobra.Command, worklogID string, w devboard.WaitingItem, answer string) string {
	if worklogID == "" {
		return "answer recorded as decision (no worklog ticket)"
	}
	wd, err := resolveWorkdir()
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(),
			"waiting-on: worklog dir unavailable (%v); answer kept as task decision only\n", err)
		return "answer recorded as decision (worklog unavailable)"
	}
	body := fmt.Sprintf("**%s answered** (asked %s: %s)\n\n%s", w.Who, w.Asked, w.Text, answer)

	// Live ticket: note.Append handles file creation + WORK.md linking.
	if _, err := note.Append(wd, worklogID, body, time.Now()); err == nil {
		return "answer recorded: decision + notes/" + worklogID + ".md"
	} else if !errors.Is(err, note.ErrUnknownID) {
		fmt.Fprintf(cmd.ErrOrStderr(),
			"waiting-on: notes append failed (%v); answer kept as task decision only\n", err)
		return "answer recorded as decision (notes append failed)"
	}

	// Archived ticket: append directly when its notes file already exists.
	notesPath := wd.NotesFile(worklogID)
	if fi, statErr := os.Stat(notesPath); statErr == nil && fi.Mode().IsRegular() {
		entry := fmt.Sprintf("\n## %s\n\n%s\n", time.Now().Format("2006-01-02 15:04"), body)
		f, err := os.OpenFile(notesPath, os.O_APPEND|os.O_WRONLY, 0o644)
		if err == nil {
			_, werr := f.WriteString(entry)
			cerr := f.Close()
			if werr == nil && cerr == nil {
				return "answer recorded: decision + notes/" + worklogID + ".md (archived ticket)"
			}
		}
		fmt.Fprintf(cmd.ErrOrStderr(),
			"waiting-on: notes append failed; answer kept as task decision only\n")
		return "answer recorded as decision (notes append failed)"
	}

	fmt.Fprintf(cmd.ErrOrStderr(),
		"waiting-on: ticket %s not in WORK.md and no notes file; answer kept as task decision only\n", worklogID)
	return "answer recorded as decision (ticket archived, no notes file)"
}
