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
	"gopkg.in/yaml.v3"

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
		newTaskAmendCmd(&flagID, &flagChild, &flagForce, &flagJSON),
		newTaskScoutCmd(&flagID, &flagChild, &flagForce, &flagJSON),
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

// resolveTaskPath maps --id (or the cwd repo's single task) to an
// EXISTING file path — every task<sub> command now resolves its target
// against the store (storeMutateTaskOrChild), which creates on first use
// itself, so the only remaining caller is untrack, which only ever
// operates on a file that's already there.
//
// devboard.Find searches every repo group by filename alone, so an --id
// that collides with an unrelated task in another repo would otherwise be
// silently adopted (or, worse, mutated). force=false refuses that case;
// force=true is the deliberate escape hatch (e.g. the same repo checked
// out under two different directory names).
func resolveTaskPath(id string, force bool) (string, error) {
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
	// Warnings carries non-fatal lint output. It rides the result rather than
	// stderr because the consumer is an agent reading --json, and stdout stays
	// exactly one JSON document.
	Warnings []string `json:"warnings,omitempty"`
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
	for _, w := range res.Warnings {
		fmt.Fprintln(cmd.OutOrStdout(), style.Warn.Render("NOTE: "+w))
	}
	return nil
}

// mutateTask is the shared body of every subcommand: resolve against the
// store, mutate, commit, emit. child routes the mutation to that child's
// own entry when --id names an epic — see storeMutateTaskOrChild.
// warn hooks run after the mutation, using the file it landed in. Variadic
// so the ten other callers stay untouched.
func mutateTask(cmd *cobra.Command, id, child string, asJSON bool,
	action, detail string, fn func(*devboard.Task) error,
	warn ...func(string) []string) error {
	if taskDisabled(cmd) {
		return nil
	}
	path, _, err := storeMutateTaskOrChild(id, child, fn)
	if err != nil {
		code := 1
		if ec, ok := err.(exitCoder); ok { // e.g. bad index, or missing/invalid --child
			code = ec.ExitCode()
		}
		return jsonOrTextError(cmd, asJSON, code, "%v", err)
	}
	var warnings []string
	for _, w := range warn {
		if w != nil {
			warnings = append(warnings, w(path)...)
		}
	}
	rel, _ := filepath.Rel(devboard.DataDir(), path)
	return emitTaskResult(cmd, asJSON,
		taskResult{File: rel, Action: action, Detail: detail, Warnings: warnings})
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
			return mutateTask(cmd, *id, *child, *asJSON, "complexity set", c,
				func(t *devboard.Task) error { t.Complexity = c; return nil },
				scoutGateHook(*child, "", true))
		},
	}
}

// resyncChecklist names the artifacts an amendment leaves stale. The CLI owns
// one of them; the rest are the human's, so the value is in naming them all at
// the moment the amendment is recorded rather than trusting recall later.
func resyncChecklist(child string) []string {
	title := "worklog ticket title: `worklog edit title <id>` (the task YAML re-mirrors from WORK.md on the next start)"
	if child != "" {
		title = "child title: edit the `- [ ] <id>: <title>` line in notes/<epic>.md — the roster sync rewrites the YAML title from it"
	}
	return []string{
		"re-sync checklist:",
		"  contract file: add a row to its Amendments table, with this complexity rating",
		"  " + title,
		"  ticket acceptance: `worklog edit <id> --acceptance ...` if the criteria moved",
		"  scorecard: `worklog task scorecard` — add, edit or remove criteria the amendment changed",
		"  plan: `worklog task plan` — steps the amendment invalidated",
		"  slug: the ticket id is structural and is NOT renamed; a retargeted amendment leaves it describing the old design",
	}
}

func newTaskAmendCmd(id, child *string, force *bool, asJSON *bool) *cobra.Command {
	var flagWhy, flagComplexity string
	cmd := &cobra.Command{
		Use:   "amend <what>",
		Args:  cobra.ExactArgs(1),
		Short: "Record a contract amendment, forcing a complexity re-rate",
		Long: `amend records a change to an agreed contract as a decisions entry that
carries the complexity rating alongside it, and prints a checklist of the
artifacts the amendment leaves stale.

  worklog task amend "<what>" --why "<why>" --complexity <unchanged|low|medium|high>

--complexity is required. In the contract corpus 16 of 23 contracts were
amended and the rating was re-decided once, while that rating is what gates
the risk scout — so an amendment is exactly the moment to ask. "unchanged"
keeps the current rating and is refused when there is no rating to keep.

The checklist is advisory and rides the result's warnings, so --json output
stays a single document.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Enforced here rather than with cobra's MarkFlagRequired, which is
			// used nowhere in this CLI: it exits 1 with a plain error and writes
			// nothing to stdout under --json, leaving an agent with no document
			// to parse. Usage refusals here are 64, like every other one.
			if !cmd.Flags().Changed("complexity") {
				return jsonOrTextError(cmd, *asJSON, 64,
					"task amend: --complexity is required (unchanged|low|medium|high); "+
						"an amendment is when the rating needs re-deciding")
			}
			c := strings.ToLower(strings.TrimSpace(flagComplexity))
			switch c {
			case "unchanged", "low", "medium", "high":
			default:
				return jsonOrTextError(cmd, *asJSON, 64,
					"task amend: --complexity must be unchanged|low|medium|high, got %q", flagComplexity)
			}
			return mutateTask(cmd, *id, *child, *asJSON, "amendment recorded", args[0],
				func(t *devboard.Task) error {
					old := strings.TrimSpace(t.Complexity)
					next := c
					if c == "unchanged" {
						// "unchanged from nothing" is the skip this verb exists to
						// prevent: only 5 of 23 contracts carried a rating at all.
						if old == "" {
							return errWithExit(64, "task amend: --complexity unchanged "+
								"needs a rating to keep, and this task has none — state low|medium|high")
						}
						next = old
					}
					transition := next
					switch {
					case old == "":
						transition = next + " (first rating)"
					case old == next:
						transition = old + " (unchanged)"
					default:
						transition = old + " → " + next
					}
					t.Complexity = next
					// An amendment means the scope moved, so a scout run against
					// the old scope no longer attests the new one.
					if next == "medium" || next == "high" {
						t.Scout = nil
					}
					t.Decision = append(t.Decision, devboard.Decision{
						What:       args[0],
						Why:        flagWhy,
						When:       time.Now().Format("2006-01-02"),
						Complexity: transition,
					})
					return nil
				},
				scoutGateHook(*child, "", true),
				func(string) []string { return resyncChecklist(*child) })
		},
	}
	cmd.Flags().StringVar(&flagWhy, "why", "", "rationale for the amendment")
	cmd.Flags().StringVar(&flagComplexity, "complexity", "",
		"required: unchanged|low|medium|high — the re-rate this amendment forces")
	return cmd
}

var scoutModes = map[string]bool{"ran": true, "inline": true, "skipped": true}

// phasesPastContract are the phases by which the scout should already have
// happened. Reaching one without an attestation is what the gate reports.
var phasesPastContract = map[string]bool{"plan": true, "implementing": true, "verify": true}

func newTaskScoutCmd(id, child *string, force *bool, asJSON *bool) *cobra.Command {
	var flagWhy string
	cmd := &cobra.Command{
		Use:   "scout <ran|inline|skipped>",
		Args:  cobra.ExactArgs(1),
		Short: "Attest what happened to the contract-phase risk scout",
		Long: `scout records whether the risk scout ran, so that afterwards you can
tell "ran" from "skipped" from "could not".

  worklog task scout ran     --why "4 lenses over the draft scope"
  worklog task scout inline  --why "subagents unavailable; walked the lenses single-pass"
  worklog task scout skipped --why "<why>"

The mode is self-reported, so this is an audit record, not enforcement.
What it makes visible is the case where nothing was recorded at all: the
gate on phase/complexity/amend warns when medium or high complexity work
has no attestation.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := strings.ToLower(strings.TrimSpace(args[0]))
			if !scoutModes[mode] {
				return jsonOrTextError(cmd, *asJSON, 64,
					"task scout: mode must be ran|inline|skipped, got %q", args[0])
			}
			if strings.TrimSpace(flagWhy) == "" {
				return jsonOrTextError(cmd, *asJSON, 64,
					"task scout: --why is required; the value is in the answer, not the record")
			}
			return mutateTask(cmd, *id, *child, *asJSON, "scout attested", mode,
				func(t *devboard.Task) error {
					t.Scout = &devboard.Scout{
						Mode: mode, Why: flagWhy,
						When: time.Now().Format("2006-01-02"),
					}
					return nil
				})
		},
	}
	cmd.Flags().StringVar(&flagWhy, "why", "", "required: what actually happened")
	return cmd
}

// scoutGateHook warns when medium/high work has reached a phase past the
// contract without an attestation. Pure-read by construction: it re-reads the
// file the mutation just wrote and does nothing else. `phase` is the most
// frequently run subcommand, so a hook that shelled out would be felt.
//
// requirePastContract distinguishes the two callers: `phase` already knows the
// phase it just set, while `complexity` and `amend` must only fire once the
// work is past the point where the scout should have run — complexity is rated
// at intake, before any scout could have happened.
func scoutGateHook(child string, phaseJustSet string, requirePastContract bool) func(string) []string {
	return func(taskPath string) []string {
		raw, err := os.ReadFile(taskPath)
		if err != nil {
			return nil
		}
		var t devboard.Task
		if err := yaml.Unmarshal(raw, &t); err != nil {
			return nil
		}
		complexity, phase, scout := t.Complexity, t.Phase, t.Scout
		if child != "" {
			var found bool
			for _, c := range t.Children {
				// EqualFold, matching findOrAppendChild: a differently-cased
				// --child must not make the warning vanish.
				if strings.EqualFold(c.ID, child) {
					complexity, phase, scout, found = c.Complexity, c.Phase, c.Scout, true
					break
				}
			}
			if !found {
				return nil
			}
		}
		if scout != nil {
			return nil
		}
		if c := strings.ToLower(complexity); c != "medium" && c != "high" {
			return nil
		}
		if phaseJustSet != "" && !phasesPastContract[phaseJustSet] {
			return nil
		}
		if requirePastContract && !phasesPastContract[strings.ToLower(phase)] {
			return nil
		}
		// Leads with the command: warnings are untyped strings shared with the
		// verify lint, so the text is what makes this one recognisable.
		return []string{"`worklog task scout ran|inline|skipped --why \"<why>\"` — " +
			complexity + "-complexity work with no scout attestation. " +
			"Run the scout, or record why it did not run."}
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
			return mutateTask(cmd, *id, *child, *asJSON, "phase set", p,
				func(t *devboard.Task) error { t.Phase = p; return nil },
				scoutGateHook(*child, p, false))
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
				return mutateTask(cmd, *id, *child, *asJSON, "plan item added", text,
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
				return mutateTask(cmd, *id, *child, *asJSON, "plan item edited", "#"+arg,
					func(t *devboard.Task) error {
						i, err := index1(arg, len(t.Plan), "plan")
						if err != nil {
							return err
						}
						t.Plan[i].Text = text
						return nil
					})
			case verb == "remove":
				return mutateTask(cmd, *id, *child, *asJSON, "plan item removed", "#"+arg,
					func(t *devboard.Task) error {
						i, err := index1(arg, len(t.Plan), "plan")
						if err != nil {
							return err
						}
						t.Plan = append(t.Plan[:i], t.Plan[i+1:]...)
						return nil
					})
			case states[verb] != "":
				return mutateTask(cmd, *id, *child, *asJSON, "plan item "+states[verb], "#"+arg,
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
criterion 2. Re-read the list before addressing one by index after a removal.

add, edit and pass warn (never refuse) about a verify cell that cannot prove
anything: hedged wording like "or manual", and cells that are neither a
command nor an explicit "manual: <procedure>". pass additionally asks the
toolchain what a 'go test -run <pattern>' cell actually matches, and warns
when that is nothing — go test exits 0 on zero matches, so such a criterion
would otherwise pass green having run no code. It stays silent whenever it
cannot answer: no toolchain, a build failure, or no repo it can identify.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			verb := args[0]
			if verb == "add" {
				if len(args) != 2 {
					return jsonOrTextError(cmd, *asJSON, 64,
						"task scorecard add: takes exactly one argument")
				}
				text := args[1]
				return mutateTask(cmd, *id, *child, *asJSON, "criterion added", text,
					func(t *devboard.Task) error {
						t.Score = append(t.Score, devboard.ScoreItem{
							Text: text, Verify: flagVerify, Status: "pending"})
						return nil
					}, verifyLintHook("add", flagVerify, "", *child))
			}

			arg, text, err := itemArgs(cmd, *asJSON, "scorecard", verb, args)
			if err != nil {
				return err
			}

			switch verb {
			case "edit":
				setVerify := cmd.Flags().Changed("verify")
				editedVerify := ""
				if setVerify {
					editedVerify = flagVerify
				}
				return mutateTask(cmd, *id, *child, *asJSON, "criterion edited", "#"+arg,
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
					}, verifyLintHook("edit", editedVerify, arg, *child))
			case "remove":
				return mutateTask(cmd, *id, *child, *asJSON, "criterion removed", "#"+arg,
					func(t *devboard.Task) error {
						i, err := index1(arg, len(t.Score), "scorecard")
						if err != nil {
							return err
						}
						t.Score = append(t.Score[:i], t.Score[i+1:]...)
						return nil
					})
			case "pass", "fail", "pending":
				return mutateTask(cmd, *id, *child, *asJSON, "criterion "+verb, "#"+arg,
					func(t *devboard.Task) error {
						i, err := index1(arg, len(t.Score), "scorecard")
						if err != nil {
							return err
						}
						t.Score[i].Status = verb
						return nil
					}, verifyLintHook(verb, "", arg, *child))
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
			return mutateTask(cmd, *id, *child, *asJSON, "decision recorded", args[0],
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
				return mutateTask(cmd, *id, *child, *asJSON, "needs-you added", arg,
					func(t *devboard.Task) error {
						t.NeedsYou = append(t.NeedsYou, devboard.NeedsItem{
							Type: flagType, Text: arg, Detail: flagDetail})
						return nil
					})
			case "resolve":
				return mutateTask(cmd, *id, *child, *asJSON, "needs-you resolved", arg,
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
			return mutateTask(cmd, *id, *child, *asJSON, "code entry added", args[0],
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
			path, err := resolveTaskPath(*id, *force)
			if err != nil {
				if ec, ok := err.(exitCoder); ok {
					return jsonOrTextError(cmd, *asJSON, ec.ExitCode(), "%v", err)
				}
				return jsonOrTextError(cmd, *asJSON, 1, "%v", err)
			}
			wd, err := resolveWorkdir()
			if err != nil {
				return jsonOrTextError(cmd, *asJSON, 1, "%v", err)
			}
			// *id may be empty (single-task-in-repo resolution), so derive
			// the slug from the file resolveTaskPath actually found.
			slug := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			if err := clearBoardTracked(wd, slug); err != nil {
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
