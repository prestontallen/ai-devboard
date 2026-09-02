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

	"github.com/prestontallen/ai-devboard/worklog/internal/devboard"
	"github.com/prestontallen/ai-devboard/worklog/internal/style"
)

// phaseOrder mirrors the dev-context skill's phases in workflow order, plus
// "done". A spike uses the subset intake → research → present → done.
// It is the single source for both the lookup map and the error message, so
// the two cannot drift.
var phaseOrder = []string{
	"intake", "clarify", "research", "contract", "plan",
	"implementing", "verify", "present", "ship", "done",
}

// phaseAliases accepts the spelling the dev-context skill actually prescribes
// where it differs from the stored name. The stored value stays canonical, so
// existing task files and the board render unchanged.
var phaseAliases = map[string]string{"implement": "implementing"}

var validPhases = func() map[string]bool {
	m := make(map[string]bool, len(phaseOrder))
	for _, p := range phaseOrder {
		m[p] = true
	}
	return m
}()

func newTaskCmd() *cobra.Command {
	var flagID string
	var flagChild string
	var flagJSON bool
	var flagForce bool

	cmd := &cobra.Command{
		Use:   "task",
		Short: "Update the devboard task file for in-flight work",
		Long: `task mutates the devboard task file rendered by the dashboard —
the in-flight detail worklog itself deliberately does not store: plan item
states, contract scorecard, decisions, code-to-know, and the needs-you
attention queue.

Target resolution: --id names the task file slug (searched across all repo
groups). Without --id, the task file whose group matches the current git
repo is used when exactly one exists. An --id that resolves to a file in a
DIFFERENT repo than the current one is refused (the file likely belongs to
an unrelated task that happens to share the slug) — pass --force to adopt
it anyway.

When --id names an epic, --child <child-id> is required — every in-flight
subcommand below then targets that child's own entry in the epic's
children list, never the epic file's (unused) top-level fields. --child is
rejected when --id names a plain ticket. An --id that is ITSELF a child of
an epic (has a Parent in WORK.md) is refused outright: pass --id <epic>
--child <that-id> instead, so a stray per-child file is never recreated.

All subcommands are silent no-ops (exit 0, notice on stderr) when the
devboard data dir does not exist. See devboard/schema.md for the file
format and field-ownership rules.`,
	}
	cmd.PersistentFlags().StringVar(&flagID, "id", "", "task file slug (default: the current repo's only task)")
	cmd.PersistentFlags().StringVar(&flagChild, "child", "",
		"child ticket id (required when --id names an epic; rejected otherwise)")
	cmd.PersistentFlags().BoolVar(&flagJSON, "json", false, "emit a JSON result object instead of styled text")
	cmd.PersistentFlags().BoolVar(&flagForce, "force", false,
		"reuse an --id that already names a task file in a different repo")

	cmd.AddCommand(
		newTaskComplexityCmd(&flagID, &flagChild, &flagForce, &flagJSON),
		newTaskPhaseCmd(&flagID, &flagChild, &flagForce, &flagJSON),
		newTaskPlanCmd(&flagID, &flagChild, &flagForce, &flagJSON),
		newTaskScorecardCmd(&flagID, &flagChild, &flagForce, &flagJSON),
		newTaskDecisionCmd(&flagID, &flagChild, &flagForce, &flagJSON),
		newTaskNeedsYouCmd(&flagID, &flagChild, &flagForce, &flagJSON),
		newTaskWaitingOnCmd(&flagID, &flagChild, &flagForce, &flagJSON),
		newTaskCodeCmd(&flagID, &flagChild, &flagForce, &flagJSON),
		newTaskUntrackCmd(&flagID, &flagForce, &flagJSON),
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
//
// devboard.Find searches every repo group by filename alone, so an --id
// that collides with an unrelated task in another repo would otherwise be
// silently adopted (or, worse, mutated). force=false refuses that case;
// force=true is the deliberate escape hatch (e.g. the same repo checked
// out under two different directory names).
func resolveTaskPath(id string, allowCreate, force bool) (string, error) {
	if id != "" {
		p, err := devboard.Find(id)
		if err != nil {
			return "", err
		}
		if p != "" {
			if !force && filepath.Base(filepath.Dir(p)) != devboard.RepoName() {
				rel, relErr := filepath.Rel(devboard.DataDir(), p)
				if relErr != nil {
					rel = p
				}
				return "", errWithExit(1,
					"task: id %q already used by %s (different repo); pass --force to reuse it there, or choose a different --id",
					id, rel)
			}
			return p, nil
		}
		if allowCreate {
			if parent := childOfEpicParent(id); parent != "" {
				return "", errWithExit(64,
					"task: %q is a child of epic %q; pass --id %s --child %s instead of creating a standalone file",
					id, parent, parent, id)
			}
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
// child routes the mutation to that child's own entry when --id names an
// epic — see mutateTaskOrChild.
func mutateTask(cmd *cobra.Command, id, child string, asJSON, allowCreate, force bool,
	action, detail string, fn func(*devboard.Task) error) error {
	if taskDisabled(cmd) {
		return nil
	}
	path, err := resolveTaskPath(id, allowCreate, force)
	if err != nil {
		if ec, ok := err.(exitCoder); ok {
			return jsonOrTextError(cmd, asJSON, ec.ExitCode(), "%v", err)
		}
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}
	if _, err := mutateTaskOrChild(path, child, fn); err != nil {
		code := 1
		if ec, ok := err.(exitCoder); ok { // e.g. bad index, or missing/invalid --child
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

func newTaskComplexityCmd(id, child *string, force *bool, asJSON *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "complexity <low|medium|high>",
		Args:  cobra.ExactArgs(1),
		Short: "Rate the task's complexity (throttles fan-out depth)",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := strings.ToLower(args[0])
			if c != "low" && c != "medium" && c != "high" {
				return jsonOrTextError(cmd, *asJSON, 64,
					"task: complexity must be low|medium|high, got %q", c)
			}
			return mutateTask(cmd, *id, *child, *asJSON, true, *force, "complexity set", c,
				func(t *devboard.Task) error { t.Complexity = c; return nil })
		},
	}
}

func newTaskPhaseCmd(id, child *string, force *bool, asJSON *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "phase <phase>",
		Args:  cobra.ExactArgs(1),
		Short: "Set the task's workflow phase",
		RunE: func(cmd *cobra.Command, args []string) error {
			p := strings.ToLower(args[0])
			if canonical, ok := phaseAliases[p]; ok {
				p = canonical
			}
			if !validPhases[p] {
				return jsonOrTextError(cmd, *asJSON, 64,
					"task: unknown phase %q (%s)", p, strings.Join(phaseOrder, "|"))
			}
			return mutateTask(cmd, *id, *child, *asJSON, true, *force, "phase set", p,
				func(t *devboard.Task) error { t.Phase = p; return nil })
		},
	}
}

// itemArgs validates the positional arity of a list subcommand's verb and
// returns the index argument plus the replacement text (empty unless the verb
// is "edit"). Both list subcommands accept 2 or 3 args, so the per-verb rule
// has to be enforced here rather than by cobra.
func itemArgs(cmd *cobra.Command, asJSON bool, what, verb string, args []string) (string, string, error) {
	if verb == "edit" {
		if len(args) != 3 {
			return "", "", jsonOrTextError(cmd, asJSON, 64,
				"task %s edit: want <n> <text>", what)
		}
		return args[1], args[2], nil
	}
	if len(args) != 2 {
		return "", "", jsonOrTextError(cmd, asJSON, 64,
			"task %s %s: takes exactly one argument", what, verb)
	}
	return args[1], "", nil
}

func newTaskPlanCmd(id, child *string, force *bool, asJSON *bool) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan <add <text> | edit <n> <text> | remove <n> | start|done|block|pending <n>>",
		Args:  cobra.RangeArgs(2, 3),
		Short: "Add, reword, remove, or re-state a plan item (1-based index)",
		Long: `plan maintains the task file's ordered plan.

  worklog task plan add "<text>"        # append an item
  worklog task plan edit <n> "<text>"   # reword item n, keeping its state
  worklog task plan remove <n>          # delete item n
  worklog task plan start|done|block|pending <n>

remove renumbers: dropping item 2 makes the old item 3 the new item 2. Re-read
the list before addressing an item by index after a removal.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			verb := args[0]
			states := map[string]string{
				"start": "in_progress", "done": "done",
				"block": "blocked", "pending": "pending",
			}
			if verb == "add" {
				if len(args) != 2 {
					return jsonOrTextError(cmd, *asJSON, 64,
						"task plan add: takes exactly one argument")
				}
				text := args[1]
				return mutateTask(cmd, *id, *child, *asJSON, true, *force, "plan item added", text,
					func(t *devboard.Task) error {
						t.Plan = append(t.Plan, devboard.PlanItem{Text: text, State: "pending"})
						return nil
					})
			}

			arg, text, err := itemArgs(cmd, *asJSON, "plan", verb, args)
			if err != nil {
				return err
			}

			switch {
			case verb == "edit":
				return mutateTask(cmd, *id, *child, *asJSON, false, *force, "plan item edited", "#"+arg,
					func(t *devboard.Task) error {
						i, err := index1(arg, len(t.Plan), "plan")
						if err != nil {
							return err
						}
						t.Plan[i].Text = text
						return nil
					})
			case verb == "remove":
				return mutateTask(cmd, *id, *child, *asJSON, false, *force, "plan item removed", "#"+arg,
					func(t *devboard.Task) error {
						i, err := index1(arg, len(t.Plan), "plan")
						if err != nil {
							return err
						}
						t.Plan = append(t.Plan[:i], t.Plan[i+1:]...)
						return nil
					})
			case states[verb] != "":
				return mutateTask(cmd, *id, *child, *asJSON, false, *force, "plan item "+states[verb], "#"+arg,
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
					"task plan: unknown verb %q (add|edit|remove|start|done|block|pending)", verb)
			}
		},
	}
	return cmd
}

func newTaskScorecardCmd(id, child *string, force *bool, asJSON *bool) *cobra.Command {
	var flagVerify string
	cmd := &cobra.Command{
		Use:   "scorecard <add <text> | edit <n> <text> | remove <n> | pass|fail|pending <n>>",
		Args:  cobra.RangeArgs(2, 3),
		Short: "Add, reword, remove, or set the status of a scorecard criterion (1-based index)",
		Long: `scorecard maintains the task file's acceptance criteria.

  worklog task scorecard add "<text>" --verify "<check>"
  worklog task scorecard edit <n> "<text>" [--verify "<check>"]
  worklog task scorecard remove <n>
  worklog task scorecard pass|fail|pending <n>

edit keeps the criterion's status; --verify is only rewritten when passed.
remove renumbers: dropping criterion 2 makes the old criterion 3 the new
criterion 2. Re-read the list before addressing one by index after a removal.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			verb := args[0]
			if verb == "add" {
				if len(args) != 2 {
					return jsonOrTextError(cmd, *asJSON, 64,
						"task scorecard add: takes exactly one argument")
				}
				text := args[1]
				return mutateTask(cmd, *id, *child, *asJSON, true, *force, "criterion added", text,
					func(t *devboard.Task) error {
						t.Score = append(t.Score, devboard.ScoreItem{
							Text: text, Verify: flagVerify, Status: "pending"})
						return nil
					})
			}

			arg, text, err := itemArgs(cmd, *asJSON, "scorecard", verb, args)
			if err != nil {
				return err
			}

			switch verb {
			case "edit":
				setVerify := cmd.Flags().Changed("verify")
				return mutateTask(cmd, *id, *child, *asJSON, false, *force, "criterion edited", "#"+arg,
					func(t *devboard.Task) error {
						i, err := index1(arg, len(t.Score), "scorecard")
						if err != nil {
							return err
						}
						t.Score[i].Text = text
						if setVerify {
							t.Score[i].Verify = flagVerify
						}
						return nil
					})
			case "remove":
				return mutateTask(cmd, *id, *child, *asJSON, false, *force, "criterion removed", "#"+arg,
					func(t *devboard.Task) error {
						i, err := index1(arg, len(t.Score), "scorecard")
						if err != nil {
							return err
						}
						t.Score = append(t.Score[:i], t.Score[i+1:]...)
						return nil
					})
			case "pass", "fail", "pending":
				return mutateTask(cmd, *id, *child, *asJSON, false, *force, "criterion "+verb, "#"+arg,
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
					"task scorecard: unknown verb %q (add|edit|remove|pass|fail|pending)", verb)
			}
		},
	}
	cmd.Flags().StringVar(&flagVerify, "verify", "",
		"verification command/check; set on add, rewritten on edit only when passed")
	return cmd
}

func newTaskDecisionCmd(id, child *string, force *bool, asJSON *bool) *cobra.Command {
	var flagWhy string
	cmd := &cobra.Command{
		Use:   "decision <what>",
		Args:  cobra.ExactArgs(1),
		Short: "Record an implementation decision (or contract amendment)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return mutateTask(cmd, *id, *child, *asJSON, true, *force, "decision recorded", args[0],
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

func newTaskNeedsYouCmd(id, child *string, force *bool, asJSON *bool) *cobra.Command {
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
				return mutateTask(cmd, *id, *child, *asJSON, true, *force, "needs-you added", arg,
					func(t *devboard.Task) error {
						t.NeedsYou = append(t.NeedsYou, devboard.NeedsItem{
							Type: flagType, Text: arg, Detail: flagDetail})
						return nil
					})
			case "resolve":
				return mutateTask(cmd, *id, *child, *asJSON, false, *force, "needs-you resolved", arg,
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

func newTaskCodeCmd(id, child *string, force *bool, asJSON *bool) *cobra.Command {
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
			return mutateTask(cmd, *id, *child, *asJSON, true, *force, "code entry added", args[0],
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

func newTaskUntrackCmd(id *string, force *bool, asJSON *bool) *cobra.Command {
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
			path, err := resolveTaskPath(*id, false, *force)
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
