package cli

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/prestontallen/ai-devboard/worklog/internal/devboard"
	"github.com/prestontallen/ai-devboard/worklog/internal/storesync"
)

// newTaskWaitingOnCmd manages the external-answer queue: questions blocked
// on other teams/people, expected to sit for days (vs needs-you, blocked on
// the task's own human). See devboard/schema.md.
func newTaskWaitingOnCmd(id, child *string, force *bool, asJSON *bool) *cobra.Command {
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
				return mutateTask(cmd, *id, *child, *asJSON, true, *force, "waiting-on added", arg+" → "+who,
					func(t *devboard.Task) error {
						t.WaitingOn = append(t.WaitingOn, devboard.WaitingItem{
							Text: arg, Who: who, Asked: asked,
							Link: flagLink, Detail: flagDetail})
						return nil
					})
			case "resolve":
				return runWaitingOnResolve(cmd, *id, *child, *asJSON, *force, arg, strings.TrimSpace(flagAnswer))
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
func runWaitingOnResolve(cmd *cobra.Command, id, child string, asJSON, force bool, arg, answer string) error {
	if taskDisabled(cmd) {
		return nil
	}
	today := time.Now().Format("2006-01-02")
	var resolved []devboard.WaitingItem // captured for the notes append
	path, worklogID, mutErr := runTaskMutation(id, child, false, force, func(t *devboard.Task) error {
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
	if wd, err := resolveWorkdir(); err == nil {
		storesync.WarnAfterWrite(wd)
	}

	rel, _ := filepath.Rel(devboard.DataDir(), path)
	return emitTaskResult(cmd, asJSON, taskResult{
		File: rel, Action: "waiting-on resolved", Detail: detail})
}

// appendAnswerToWorklog writes the answer into the ticket's notes file —
// the one sanctioned devboard→worklog write (see devboard/schema.md). It
// returns a short human status and warns (never errors) when the system of
// record is unreachable: no worklog id, no worklog dir, or the append
// itself fails. In every such case the task-file decision already holds
// the answer.
//
// Store-backed lookup covers live and archived tickets uniformly — no
// separate archived-ticket fallback needed, unlike the retired legacy
// path (note.Append only ever resolved live WORK.md blocks). It also
// makes note.ErrUnknownID structurally unreachable here: worklogID comes
// from resolveStoreTarget's own t.Slug, so a task mutation only ever
// gets this far once --id has already resolved to a real store ticket —
// under the store model a devboard task's identity IS its worklog
// identity, unlike legacy's separate worklog: cross-reference field that
// could dangle.
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

	ss, err := openStoreForWrite(wd)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(),
			"waiting-on: notes append failed (%v); answer kept as task decision only\n", err)
		return "answer recorded as decision (notes append failed)"
	}
	defer ss.close()
	if _, err := runStoreNoteAppend(ss, worklogID, body, time.Now()); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(),
			"waiting-on: notes append failed (%v); answer kept as task decision only\n", err)
		return "answer recorded as decision (notes append failed)"
	}
	return "answer recorded: decision + notes/" + worklogID + ".md"
}
