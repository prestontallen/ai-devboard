package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/prestontallen/day2day/internal/devboard"
	"github.com/prestontallen/day2day/internal/style"
)

// validPhases mirrors the dev-context skill's phase names plus "done".
var validPhases = map[string]bool{
	"intake": true, "clarify": true, "contract": true, "plan": true,
	"implementing": true, "verify": true, "present": true, "ship": true,
	"done": true,
}

func newTaskCmd() *cobra.Command {
	var flagID string
	var flagJSON bool

	cmd := &cobra.Command{
		Use:   "task",
		Short: "Update the devboard task file for in-flight work",
		Long: `task mutates the devboard task file rendered by the dashboard —
the in-flight detail worklog itself deliberately does not store: plan item
states, contract scorecard, decisions, code-to-know, and the needs-you
attention queue.

Target resolution: --id names the task file slug (searched across all repo
groups). Without --id, the task file whose group matches the current git
repo is used when exactly one exists.

All subcommands are silent no-ops (exit 0, notice on stderr) when the
devboard data dir does not exist. See devboard/schema.md for the file
format and field-ownership rules.`,
	}
	cmd.PersistentFlags().StringVar(&flagID, "id", "", "task file slug (default: the current repo's only task)")
	cmd.PersistentFlags().BoolVar(&flagJSON, "json", false, "emit a JSON result object instead of styled text")

	cmd.AddCommand(
		newTaskPhaseCmd(&flagID, &flagJSON),
		newTaskPlanCmd(&flagID, &flagJSON),
		newTaskScorecardCmd(&flagID, &flagJSON),
		newTaskDecisionCmd(&flagID, &flagJSON),
		newTaskNeedsYouCmd(&flagID, &flagJSON),
		newTaskCodeCmd(&flagID, &flagJSON),
		newTaskUntrackCmd(&flagID, &flagJSON),
	)
	return cmd
}

// taskDisabled reports (and handles) the data-dir-absent case: notice to
// stderr, success to the caller.
func taskDisabled(cmd *cobra.Command) bool {
	if devboard.Enabled() {
		return false
	}
	fmt.Fprintf(cmd.ErrOrStderr(),
		"devboard: data dir %s not present; no-op\n", devboard.DataDir())
	return true
}

// resolveTaskPath maps --id (or the cwd repo's single task) to a file path.
// allowCreate controls whether a missing --id target may be created.
func resolveTaskPath(id string, allowCreate bool) (string, error) {
	if id != "" {
		p, err := devboard.Find(id)
		if err != nil {
			return "", err
		}
		if p != "" {
			return p, nil
		}
		if allowCreate {
			return filepath.Join(devboard.DataDir(), devboard.RepoName(), id+".yaml"), nil
		}
		return "", errWithExit(1, "task: no task file found for id %q", id)
	}
	all, err := devboard.List()
	if err != nil {
		return "", err
	}
	repo := devboard.RepoName()
	var candidates []string
	for _, p := range all {
		if filepath.Base(filepath.Dir(p)) == repo {
			candidates = append(candidates, p)
		}
	}
	switch len(candidates) {
	case 1:
		return candidates[0], nil
	case 0:
		return "", errWithExit(64, "task: no task files for repo %q; pass --id", repo)
	default:
		names := make([]string, len(candidates))
		for i, p := range candidates {
			names[i] = strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
		}
		return "", errWithExit(64, "task: %d task files for repo %q (%s); pass --id",
			len(candidates), repo, strings.Join(names, ", "))
	}
}

type taskResult struct {
	File   string `json:"file"`
	Action string `json:"action"`
	Detail string `json:"detail,omitempty"`
}

func emitTaskResult(cmd *cobra.Command, asJSON bool, res taskResult) error {
	if asJSON {
		return emitJSON(cmd.OutOrStdout(), res)
	}
	line := res.Action
	if res.Detail != "" {
		line += ": " + res.Detail
	}
	fmt.Fprintln(cmd.OutOrStdout(), style.Good.Render(line)+
		style.Dim.Render("  ("+res.File+")"))
	return nil
}

// mutateTask is the shared body of every subcommand: resolve, mutate, emit.
func mutateTask(cmd *cobra.Command, id string, asJSON, allowCreate bool,
	action, detail string, fn func(*devboard.Task) error) error {
	if taskDisabled(cmd) {
		return nil
	}
	path, err := resolveTaskPath(id, allowCreate)
	if err != nil {
		if ec, ok := err.(exitCoder); ok {
			return jsonOrTextError(cmd, asJSON, ec.ExitCode(), "%v", err)
		}
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}
	if err := devboard.Mutate(path, fn); err != nil {
		code := 1
		if ec, ok := err.(exitCoder); ok { // e.g. bad index from inside fn
			code = ec.ExitCode()
		}
		return jsonOrTextError(cmd, asJSON, code, "%v", err)
	}
	rel, _ := filepath.Rel(devboard.DataDir(), path)
	return emitTaskResult(cmd, asJSON, taskResult{File: rel, Action: action, Detail: detail})
}

// index1 parses a 1-based list index against a length.
func index1(arg string, n int, what string) (int, error) {
	i, err := strconv.Atoi(arg)
	if err != nil || i < 1 || i > n {
		return 0, errWithExit(64, "task: %s index must be 1..%d, got %q", what, n, arg)
	}
	return i - 1, nil
}

func newTaskPhaseCmd(id *string, asJSON *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "phase <phase>",
		Args:  cobra.ExactArgs(1),
		Short: "Set the task's workflow phase",
		RunE: func(cmd *cobra.Command, args []string) error {
			p := strings.ToLower(args[0])
			if !validPhases[p] {
				return jsonOrTextError(cmd, *asJSON, 64,
					"task: unknown phase %q (intake|clarify|contract|plan|implementing|verify|present|ship|done)", p)
			}
			return mutateTask(cmd, *id, *asJSON, true, "phase set", p,
				func(t *devboard.Task) error { t.Phase = p; return nil })
		},
	}
}

func newTaskPlanCmd(id *string, asJSON *bool) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan <add <text> | start|done|block|pending <n>>",
		Args:  cobra.ExactArgs(2),
		Short: "Add a plan item or set an item's state (1-based index)",
		RunE: func(cmd *cobra.Command, args []string) error {
			verb, arg := args[0], args[1]
			states := map[string]string{
				"start": "in_progress", "done": "done",
				"block": "blocked", "pending": "pending",
			}
			switch {
			case verb == "add":
				return mutateTask(cmd, *id, *asJSON, true, "plan item added", arg,
					func(t *devboard.Task) error {
						t.Plan = append(t.Plan, devboard.PlanItem{Text: arg, State: "pending"})
						return nil
					})
			case states[verb] != "":
				return mutateTask(cmd, *id, *asJSON, false, "plan item "+states[verb], "#"+arg,
					func(t *devboard.Task) error {
						i, err := index1(arg, len(t.Plan), "plan")
						if err != nil {
							return err
						}
						t.Plan[i].State = states[verb]
						return nil
					})
			default:
				return jsonOrTextError(cmd, *asJSON, 64,
					"task plan: unknown verb %q (add|start|done|block|pending)", verb)
			}
		},
	}
	return cmd
}

func newTaskScorecardCmd(id *string, asJSON *bool) *cobra.Command {
	var flagVerify string
	cmd := &cobra.Command{
		Use:   "scorecard <add <text> | pass|fail|pending <n>>",
		Args:  cobra.ExactArgs(2),
		Short: "Add a scorecard criterion or set its status (1-based index)",
		RunE: func(cmd *cobra.Command, args []string) error {
			verb, arg := args[0], args[1]
			switch verb {
			case "add":
				return mutateTask(cmd, *id, *asJSON, true, "criterion added", arg,
					func(t *devboard.Task) error {
						t.Score = append(t.Score, devboard.ScoreItem{
							Text: arg, Verify: flagVerify, Status: "pending"})
						return nil
					})
			case "pass", "fail", "pending":
				return mutateTask(cmd, *id, *asJSON, false, "criterion "+verb, "#"+arg,
					func(t *devboard.Task) error {
						i, err := index1(arg, len(t.Score), "scorecard")
						if err != nil {
							return err
						}
						t.Score[i].Status = verb
						return nil
					})
			default:
				return jsonOrTextError(cmd, *asJSON, 64,
					"task scorecard: unknown verb %q (add|pass|fail|pending)", verb)
			}
		},
	}
	cmd.Flags().StringVar(&flagVerify, "verify", "", "verification command/check for an added criterion")
	return cmd
}

func newTaskDecisionCmd(id *string, asJSON *bool) *cobra.Command {
	var flagWhy string
	cmd := &cobra.Command{
		Use:   "decision <what>",
		Args:  cobra.ExactArgs(1),
		Short: "Record an implementation decision (or contract amendment)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return mutateTask(cmd, *id, *asJSON, true, "decision recorded", args[0],
				func(t *devboard.Task) error {
					t.Decision = append(t.Decision, devboard.Decision{
						What: args[0], Why: flagWhy,
						When: time.Now().Format("2006-01-02")})
					return nil
				})
		},
	}
	cmd.Flags().StringVar(&flagWhy, "why", "", "rationale shown under the decision")
	return cmd
}

func newTaskNeedsYouCmd(id *string, asJSON *bool) *cobra.Command {
	var flagType, flagDetail string
	cmd := &cobra.Command{
		Use:   "needs-you <add <text> | resolve <n|all>>",
		Args:  cobra.ExactArgs(2),
		Short: "Add or resolve attention-queue entries",
		Long: `needs-you manages the dashboard's attention queue. Add an entry the
moment anything waits on the human; resolve it the moment it no longer
does — stale entries poison the queue.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			verb, arg := args[0], args[1]
			switch verb {
			case "add":
				return mutateTask(cmd, *id, *asJSON, true, "needs-you added", arg,
					func(t *devboard.Task) error {
						t.NeedsYou = append(t.NeedsYou, devboard.NeedsItem{
							Type: flagType, Text: arg, Detail: flagDetail})
						return nil
					})
			case "resolve":
				return mutateTask(cmd, *id, *asJSON, false, "needs-you resolved", arg,
					func(t *devboard.Task) error {
						if arg == "all" {
							t.NeedsYou = nil
							return nil
						}
						i, err := index1(arg, len(t.NeedsYou), "needs-you")
						if err != nil {
							return err
						}
						t.NeedsYou = append(t.NeedsYou[:i], t.NeedsYou[i+1:]...)
						return nil
					})
			default:
				return jsonOrTextError(cmd, *asJSON, 64,
					"task needs-you: unknown verb %q (add|resolve)", verb)
			}
		},
	}
	cmd.Flags().StringVar(&flagType, "type", "question", "entry type: question|checkpoint")
	cmd.Flags().StringVar(&flagDetail, "detail", "", "longer body rendered under the entry")
	return cmd
}

func newTaskCodeCmd(id *string, asJSON *bool) *cobra.Command {
	var flagLines, flagLang, flagNote, flagSnippet string
	cmd := &cobra.Command{
		Use:   "code <file>",
		Args:  cobra.ExactArgs(1),
		Short: "Add a code-to-know entry (a load-bearing change the human should see)",
		RunE: func(cmd *cobra.Command, args []string) error {
			snippet := flagSnippet
			if snippet == "-" {
				raw, err := readAllStdin(cmd)
				if err != nil {
					return jsonOrTextError(cmd, *asJSON, 1, "%v", err)
				}
				snippet = raw
			}
			return mutateTask(cmd, *id, *asJSON, true, "code entry added", args[0],
				func(t *devboard.Task) error {
					t.Code = append(t.Code, devboard.CodeRef{
						File: args[0], Lines: flagLines, Lang: flagLang,
						Note: flagNote, Snippet: snippet})
					return nil
				})
		},
	}
	cmd.Flags().StringVar(&flagLines, "lines", "", "line range, e.g. 88-104")
	cmd.Flags().StringVar(&flagLang, "lang", "", "language hint for highlighting")
	cmd.Flags().StringVar(&flagNote, "note", "", "why the human should care")
	cmd.Flags().StringVar(&flagSnippet, "snippet", "", "code snippet ('-' reads stdin)")
	return cmd
}

func newTaskUntrackCmd(id *string, asJSON *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "untrack",
		Args:  cobra.NoArgs,
		Short: "Stop showing this task on the dashboard (deletes only its task file)",
		Long: `untrack removes the task's devboard YAML (and its lock file), so the
dashboard stops rendering it. Nothing else is touched: the worklog ticket,
notes, and archive entries all remain.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if taskDisabled(cmd) {
				return nil
			}
			path, err := resolveTaskPath(*id, false)
			if err != nil {
				if ec, ok := err.(exitCoder); ok {
					return jsonOrTextError(cmd, *asJSON, ec.ExitCode(), "%v", err)
				}
				return jsonOrTextError(cmd, *asJSON, 1, "%v", err)
			}
			if err := os.Remove(path); err != nil {
				return jsonOrTextError(cmd, *asJSON, 1, "%v", err)
			}
			os.Remove(path + ".lock") // best-effort
			rel, _ := filepath.Rel(devboard.DataDir(), path)
			return emitTaskResult(cmd, *asJSON, taskResult{
				File: rel, Action: "untracked", Detail: "task file removed; worklog data untouched"})
		},
	}
}

func readAllStdin(cmd *cobra.Command) (string, error) {
	raw, err := io.ReadAll(cmd.InOrStdin())
	if err != nil {
		return "", fmt.Errorf("task code: reading snippet from stdin: %w", err)
	}
	return string(raw), nil
}
