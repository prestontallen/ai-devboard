# Phase 2B-1.5 — Agent-friendly CLI surfaces

This plan is a handoff document for an implementing agent. Architectural
decisions are already made; your job is to execute precisely and report
back. Do not deviate without asking.

Read all the files referenced under "What you're working from" before
touching code. The work is mechanical, but it depends on understanding
the existing `add` flow and how the Go validator already structures its
output.

---

## Status of prior work

This repo (`/home/preston/go/src/github.com/prestontallen/day2day/`)
already contains:

- A working Go CLI (`./cmd/worklog/main.go` → `worklog` binary) with
  cobra subcommands: `validate`, `status`, `tui`, `add`, `sync`,
  `lint-specs`.
- A Go module + tests across `internal/{model,parse,render,validate,sync,lint,style,tui,cli}/`.
- An existing PLAN.md (this file you're reading replaces a prior plan that
  was already executed).
- Two bash scripts in `scripts/` (validate.sh, sync.sh, lint-specs.sh) that
  remain as transitional artifacts — do not modify them.

All existing tests pass. `go build ./...`, `go vet ./...`, and
`go test ./...` are clean.

## Why we're doing this

The `worklog add` command is currently **interactive only** — it presents
a Huh form and refuses to run without a TTY. The TUI (`worklog tui`) is
also interactive only, which is correct (it's the human's navigation
view). But agents (Claude Code, Cursor) running this tool can't drive a
form. They need a flag-based path.

Similarly, `worklog status` and `worklog validate` produce Lip Gloss-
styled text that agents have to scrape. JSON output would be vastly more
reliable for machine parsing.

The principle being codified: **every mutating command supports a
flag-driven path; every read command supports `--json`**. Forms are sugar
for humans, JSON is sugar for machines, TUI stays for humans.

## Goal for this slice

1. Make `worklog add` work non-interactively via flags. TTY autodetection
   + flag completeness drive the choice between form and direct execution.
2. Add `--json` to `worklog status` and `worklog validate`. JSON output
   replaces styled text entirely (no colors mixed in).
3. Add `--json` to `worklog add` so agents get a structured success
   response (id, section, path, warnings).
4. Update `skill/SKILL.md` and `skill/claude/command.md` so the agent
   knows to prefer the flag form.

## What you're NOT doing

- Do not modify the Bubble Tea TUI (`internal/tui/`, `internal/cli/tui.go`).
  The TUI is for humans; it stays interactive.
- Do not change the existing form behavior when invoked with no flags
  under a TTY. That's the human path; it must continue to work as it does
  today.
- Do not add `--non-interactive` as an explicit flag. TTY autodetection
  + flag completeness is sufficient.
- Do not implement epic or child-of-epic add flows. Those are Slice 4
  scope. This slice only extends the existing standalone-ticket flow.
- Do not update INDEX.md on add — that's still deferred. The "INDEX.md
  not updated" warning continues; just include it in the JSON output.
- Do not touch the bash scripts in `scripts/`.
- Do not modify any file under `$HOME/.cursor/`, `$HOME/.claude/`, or
  `$HOME/.local/share/`.

---

## Final architecture

### Behavior matrix for `worklog add`

| Required flags provided? | TTY? | Behavior |
|---|---|---|
| Yes (`--title` + `--id`) | yes or no | Skip form. Execute add immediately using flag values. |
| No, partial | yes | Open Huh form. Pre-populate any provided flag values. Prompt for the rest. |
| No | no | Fail fast with exit 64 and message: `add requires --title and --id when stdin is not a TTY`. |

A flag value of `""` for `--title` or `--id` counts as "not provided" — let the form open or fail-fast accordingly.

### `--json` output schemas

#### `worklog status --json`
```json
{
  "dir": "/path/to/workdir",
  "sections": [
    {
      "name": "Now",
      "count": 1,
      "cap": 5,
      "blocks": [
        {
          "id": "ent-3794",
          "state": "active",
          "title": "Migrate test cases",
          "type": "ticket",
          "parent": "ent-3634",
          "repo": "assessments-api",
          "tags": ["migration", "coding-questions"],
          "started": "2026-05-15",
          "pr": "",
          "files": null,
          "acceptance": "",
          "notesRef": "",
          "status": "",
          "activeChildren": null
        }
      ]
    },
    {"name": "Next", "count": 2, "cap": 0, "blocks": [...]},
    {"name": "Someday", "count": 3, "cap": 0, "blocks": [...]}
  ]
}
```
- `cap` is `5` for Now, `0` (no cap) for other sections.
- `state` is one of `"pending"`, `"active"`, `"done"`. Translate from `model.State` which is the single character.
- Field names use lowercase JSON keys (use struct tags).
- Omit no fields — emit explicit zero values (`""`, `null`, `0`).

#### `worklog validate --json`
```json
{
  "dir": "/path/to/workdir",
  "workMDMissing": false,
  "violations": [
    {"check": "now-cap", "message": "## Now has 6 tickets, cap is 5"}
  ],
  "infos": ["INDEX.md not present at ...; skipping index-refs-exist"],
  "violationCount": 1
}
```

#### `worklog add --json` (on success)
```json
{
  "status": "added",
  "id": "auth-1",
  "title": "Refactor auth",
  "section": "Next",
  "workMD": "/path/to/workdir/WORK.md",
  "warnings": ["INDEX.md not updated (deferred to Phase 2B)"]
}
```

#### Exit codes (unchanged from current)
- `add` success: 0
- `add` failure: 1 (mutation failed), 64 (usage error / non-TTY without required flags)
- `validate`: 0 / 1 (WORK.md missing) / 2 (violations) / 64 (usage)
- `status`: 0 / 1 (WORK.md missing) / 64 (usage)

In `--json` mode, errors should ALSO go through JSON on stdout:
```json
{"error": "WORK.md not found at /path/to/WORK.md"}
```
…and still set the right exit code. See implementation notes below.

---

## Step-by-step implementation

### Step 1: TTY-detection helper

Create `internal/cli/tty.go`:

```go
package cli

import (
	"os"

	"github.com/mattn/go-isatty"
)

// stdinIsTTY reports whether stdin is connected to a real terminal.
// Used by interactive subcommands to decide whether to launch a form.
func stdinIsTTY() bool {
	return isatty.IsTerminal(os.Stdin.Fd())
}
```

`github.com/mattn/go-isatty v0.0.20` is already a transitive dep
(confirmed in go.sum); `go mod tidy` after first import will promote it
to a direct require. No new `go get` needed.

### Step 2: Add JSON tags to `internal/model/block.go` and `section.go`

The model types currently have no JSON tags. Add lowercase JSON tags to
`Block`, `Section`, and (new) `WorkDoc`-summary fields. Example for `Block`:

```go
type Block struct {
	StartLine int    `json:"-"` // internal line range, not exposed
	EndLine   int    `json:"-"`

	Section SectionName `json:"-"` // available via parent Section.Name
	State   State       `json:"-"` // exposed via translated string in status JSON

	Title string `json:"title"`

	ID             string    `json:"id"`
	Type           BlockType `json:"type"`
	Parent         string    `json:"parent"`
	Repo           string    `json:"repo"`
	Tags           []string  `json:"tags"`
	Started        string    `json:"started"`
	PR             string    `json:"pr"`
	Files          []string  `json:"files"`
	Acceptance     string    `json:"acceptance"`
	NotesRef       string    `json:"notesRef"`
	Status         string    `json:"status"`
	ActiveChildren []string  `json:"activeChildren"`
}
```

State is `json:"-"` because the wire format uses a friendly string. Build
a small helper:

```go
// StateLabel returns the human-friendly representation of a state for
// JSON output: "pending" | "active" | "done".
func (s State) Label() string {
	switch s {
	case StateActive:
		return "active"
	case StateDone:
		return "done"
	default:
		return "pending"
	}
}
```

For `Section`, add `json:"name"`, `json:"count"`, `json:"cap"`,
`json:"blocks"`. `StartLine`/`EndLine` stay JSON-skipped.

In `status --json`, build a transient struct rather than marshaling the
raw `Section` (because you need the per-section cap and a translated
state string per block). Pattern:

```go
type jsonBlock struct {
	ID             string   `json:"id"`
	State          string   `json:"state"`
	Title          string   `json:"title"`
	Type           string   `json:"type"`
	Parent         string   `json:"parent"`
	Repo           string   `json:"repo"`
	Tags           []string `json:"tags"`
	Started        string   `json:"started"`
	PR             string   `json:"pr"`
	Files          []string `json:"files"`
	Acceptance     string   `json:"acceptance"`
	NotesRef       string   `json:"notesRef"`
	Status         string   `json:"status"`
	ActiveChildren []string `json:"activeChildren"`
}

type jsonSection struct {
	Name   string      `json:"name"`
	Count  int         `json:"count"`
	Cap    int         `json:"cap"`
	Blocks []jsonBlock `json:"blocks"`
}

type jsonStatus struct {
	Dir      string        `json:"dir"`
	Sections []jsonSection `json:"sections"`
}
```

Define these next to where they're used (e.g. inside
`internal/cli/status.go`) — they're CLI-format types, not model types.

### Step 3: Refactor `internal/cli/add.go` for testability + add flag surface

Goals:
1. Separate "collect inputs" (from form or flags) from "execute mutation"
   so the mutation can be unit-tested without Huh.
2. Add flags: `--title`, `--id`, `--repo`, `--tags`, `--acceptance`,
   `--section`, `--json`.

Refactored structure:

```go
type addInputs struct {
	Title      string
	ID         string
	Repo       string
	Tags       []string
	Acceptance string
	Section    string // "Next" or "Someday"
}

type addOutput struct {
	Status   string   `json:"status"`
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Section  string   `json:"section"`
	WorkMD   string   `json:"workMD"`
	Warnings []string `json:"warnings"`
}

func newAddCmd() *cobra.Command {
	var (
		flagTitle      string
		flagID         string
		flagRepo       string
		flagTagsCSV    string
		flagAcceptance string
		flagSection    string
		flagJSON       bool
	)

	cmd := &cobra.Command{
		Use:   "add",
		Args:  cobra.NoArgs,
		Short: "Add a new standalone ticket (form by default; flag-driven for agents)",
		Long: `Add a new standalone ticket to ## Next or ## Someday.

Without flags and under a TTY: opens an interactive Huh form.
With --title and --id provided: skips the form and executes immediately.
Without a TTY and without required flags: fails fast with exit 64.

Required when flag-driven:
  --title    Ticket title
  --id       Lowercase-kebab ID (e.g. ent-3794 or refactor-auth)

Optional:
  --repo, --tags (comma-separated), --acceptance, --section (Next|Someday), --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAdd(cmd, flagTitle, flagID, flagRepo, flagTagsCSV,
				flagAcceptance, flagSection, flagJSON)
		},
	}

	cmd.Flags().StringVar(&flagTitle, "title", "", "ticket title (required for non-interactive)")
	cmd.Flags().StringVar(&flagID, "id", "", "ticket ID, lowercase-kebab (required for non-interactive)")
	cmd.Flags().StringVar(&flagRepo, "repo", "", "repository name")
	cmd.Flags().StringVar(&flagTagsCSV, "tags", "", "comma-separated tags")
	cmd.Flags().StringVar(&flagAcceptance, "acceptance", "", "one-line acceptance criterion")
	cmd.Flags().StringVar(&flagSection, "section", "Next", "destination section: Next or Someday")
	cmd.Flags().BoolVar(&flagJSON, "json", false, "emit JSON status object instead of styled text")

	return cmd
}
```

`runAdd` logic:

```go
func runAdd(cmd *cobra.Command, flagTitle, flagID, flagRepo, flagTagsCSV,
	flagAcceptance, flagSection string, flagJSON bool) error {

	wd, err := resolveWorkdir()
	if err != nil { return err }

	doc, err := parse.File(wd.WorkMD())
	if err != nil {
		if errors.Is(err, model.ErrWorkMDMissing) {
			return errWithExit(1, "WORK.md not found at %s", wd.WorkMD())
		}
		return err
	}

	inputs := addInputs{
		Title:      strings.TrimSpace(flagTitle),
		ID:         strings.ToLower(strings.TrimSpace(flagID)),
		Repo:       strings.TrimSpace(flagRepo),
		Acceptance: strings.TrimSpace(flagAcceptance),
		Section:    flagSection,
	}
	for _, t := range strings.Split(flagTagsCSV, ",") {
		if t = strings.TrimSpace(t); t != "" {
			inputs.Tags = append(inputs.Tags, t)
		}
	}

	hasRequired := inputs.Title != "" && inputs.ID != ""
	needsForm := !hasRequired
	tty := stdinIsTTY()

	switch {
	case needsForm && !tty:
		return errWithExit(64,
			"add requires --title and --id when stdin is not a TTY")
	case needsForm && tty:
		if err := promptAddForm(doc, &inputs); err != nil {
			return err
		}
	}

	// Validate inputs after collection (whether from flags or form).
	if err := validateAddInputs(doc, inputs); err != nil {
		return errWithExit(1, "%v", err)
	}

	// Apply mutation.
	output, err := applyAdd(wd, doc, inputs)
	if err != nil {
		return errWithExit(1, "%v", err)
	}

	if flagJSON {
		return emitJSON(cmd.OutOrStdout(), output)
	}
	emitAddText(cmd.OutOrStdout(), output)
	return nil
}
```

Helpers to implement in the same file:

- `validateAddInputs(doc *model.WorkDoc, inputs addInputs) error`
  - Title must be non-empty.
  - ID must be non-empty and not already present in `doc.BlockByID(...)`.
  - Section must be `"Next"` or `"Someday"` (case-sensitive).
  - Tags: each tag should be lowercase-kebab-friendly (no validation
    required; trust the user — but trim whitespace, drop empties).

- `applyAdd(wd model.Workdir, doc *model.WorkDoc, inputs addInputs) (addOutput, error)`
  - Builds `render.BlockOptions` and calls `render.FormatTicketBlock(...)`.
  - Calls `render.AppendToSection(doc, model.SectionName(inputs.Section), lines)`.
  - Calls `render.WriteAtomic(wd.WorkMD(), updatedLines)`.
  - Returns an `addOutput` populated with `Status: "added"`, the inputs,
    and `Warnings: []string{"INDEX.md not updated (deferred to Phase 2B)"}`.

- `promptAddForm(doc *model.WorkDoc, inputs *addInputs) error`
  - Largely the existing Huh form logic, but reading initial Value pointers
    from `*inputs.Title`, `*inputs.ID`, etc., so prefilled flags appear in
    the form.
  - Keep the existing validators on Title and ID.

- `emitAddText(w io.Writer, out addOutput)` — current styled output (the
  `style.Good` "added X to ## Section" line + the `style.Warn` INDEX
  note).

- `emitJSON(w io.Writer, v any)` — `json.NewEncoder(w).SetIndent("", "  ")`
  then `Encode(v)`. Put this helper in `internal/cli/json.go` so it's
  reused by status / validate too.

### Step 4: `--json` on `worklog validate`

Edit `internal/cli/validate.go`:

1. Add a `--json` bool flag (cobra `BoolVar`).
2. If `--json`, suppress all the styled output. Build a struct:

```go
type jsonValidate struct {
	Dir            string                 `json:"dir"`
	WorkMDMissing  bool                   `json:"workMDMissing"`
	Violations     []jsonValidateViol     `json:"violations"`
	Infos          []string               `json:"infos"`
	ViolationCount int                    `json:"violationCount"`
}

type jsonValidateViol struct {
	Check   string `json:"check"`
	Message string `json:"message"`
}
```

Build it from the existing `*validate.Result`, encode via `emitJSON`,
return the same exit code (0/1/2) without printing styled output.

If `--json` AND a fatal error (e.g. parsing failure unrelated to a
missing WORK.md) is hit before validate completes, emit:
```json
{"error": "...short message..."}
```
…and return the appropriate exit code.

### Step 5: `--json` on `worklog status`

Edit `internal/cli/status.go`:

1. Add `--json` bool flag.
2. Build the jsonStatus/jsonSection/jsonBlock structs described in Step 2.
3. Populate from the parsed `*model.WorkDoc`:
   - Sections always in order Now / Next / Someday.
   - Cap is 5 for Now, 0 for others.
   - For each block, translate `State` via `State.Label()` and copy
     metadata fields verbatim.
4. Emit via `emitJSON`. Skip Lip Gloss entirely in JSON mode.
5. Exit codes unchanged (0 normally; 1 if WORK.md missing).

Error during JSON mode: same pattern as validate — `{"error": "..."}` on
stdout, correct exit code.

### Step 6: Update `skill/SKILL.md` and `skill/claude/command.md`

Add a section near the top of each (immediately after the layout block /
invocation block, respectively) titled **"Preferred CLI invocations
for agents"** that tells the agent to prefer the flag-driven path.

Suggested content for both files (mirrored — `lint-specs` will surface
the drift):

```markdown
## Preferred CLI invocations for agents

When mutating the worklog, prefer the flag-driven path. The `add` command
in particular has a Huh form for humans, but agents should always pass
flags directly so no form opens:

    worklog add --title "Refactor auth" --id auth-1 --section Next \
                --tags refactor,auth --json

For status and discovery, use `--json` so output is machine-parseable:

    worklog status --json
    worklog validate --json

Reserve `worklog tui` for humans only — it requires a real terminal.
```

Insert markers around this block? **No.** The rule-block markers
(`<!-- rules:start -->` ... `<!-- rules:end -->`) are only for the
"Hard rules" sections. This is a separate section; place it elsewhere
and leave the rule markers alone.

Where to insert:
- In `skill/SKILL.md`: right before `## Required behavior` (around line
  42, where the layout block ends).
- In `skill/claude/command.md`: right before `## Subcommands` (around
  line 33, after the invocation block).

After editing both files, manually verify they still pass the existing
`<!-- rules:start -->` / `<!-- rules:end -->` markers (don't disturb
those).

### Step 7: Tests

Add unit tests where possible (the form path can't be tested cleanly;
the flag path can).

#### `internal/cli/add_test.go` (NEW)

Test the `applyAdd` and `validateAddInputs` functions in isolation:

- `TestValidateAddInputsRequiresTitle` — empty title rejected
- `TestValidateAddInputsRequiresID` — empty ID rejected
- `TestValidateAddInputsRejectsDuplicateID` — ID present in doc rejected
- `TestValidateAddInputsRejectsBadSection` — section other than Next/Someday rejected
- `TestApplyAddInsertsIntoNext` — block lands in Next, doc on disk contains the new ticket
- `TestApplyAddProducesJSONShape` — output struct has correct fields

You may need to make some of the helpers exported (uppercase) OR put the
test in `package cli` (no `_test` suffix variant). Easiest: put the test
in `package cli` and use the lowercase helpers directly.

#### `internal/cli/json_test.go` (NEW)

- Test `emitJSON` writes valid JSON with two-space indent and a trailing
  newline.

#### Existing tests

`go test ./...` must still pass. No expected regressions.

### Step 8: Smoke tests + acceptance

After build, exercise each path manually. Use a temp fixture under `/tmp`
— do NOT touch `$HOME/.local/share/worklog/`.

```bash
fix=$(mktemp -d)
cat > "$fix/WORK.md" <<'EOF'
## Now
## Next
- [ ] **EXISTING** — first
  - **ID**: existing
## Someday
EOF

build=./worklog
go build -o "$build" ./cmd/worklog

# Non-interactive flag-driven add — must succeed without prompting.
"$build" --dir "$fix" add \
  --title "Refactor auth" --id auth-1 --section Next \
  --tags refactor,auth
echo "exit: $?"   # 0

# Confirm the block landed in Next:
grep -F '**ID**: auth-1' "$fix/WORK.md" && echo "OK: ticket in WORK.md"

# Same in JSON mode:
"$build" --dir "$fix" add \
  --title "Test JSON" --id json-test --section Someday --json
echo "---"
# Output must be valid JSON with status=added, the new id, section, warnings array.

# Missing required + no TTY → fail fast (exit 64).
"$build" --dir "$fix" add --section Next </dev/null
echo "exit: $?"   # 64
# Output must mention --title and --id requirement.

# Duplicate ID → fail.
"$build" --dir "$fix" add --title dupe --id auth-1 --section Next
echo "exit: $?"   # 1

# Status --json: structured doc, three sections, populated counts.
"$build" --dir "$fix" status --json | head -40

# Validate --json: structured violation list (may be empty here).
"$build" --dir "$fix" validate --json | head -20

rm -rf "$fix"
```

Document any surprises.

---

## Files affected

New:
- `internal/cli/tty.go`
- `internal/cli/json.go`
- `internal/cli/add_test.go`
- `internal/cli/json_test.go`

Modified:
- `internal/model/block.go` (JSON tags + `State.Label()`)
- `internal/model/section.go` (JSON tags if needed)
- `internal/cli/add.go` (flag surface + refactor)
- `internal/cli/validate.go` (--json)
- `internal/cli/status.go` (--json)
- `skill/SKILL.md` (new "Preferred CLI invocations" section)
- `skill/claude/command.md` (mirrored section)
- `go.mod` / `go.sum` (after `go mod tidy` — `go-isatty` becomes direct)

Unchanged:
- `internal/cli/tui.go` and `internal/tui/*.go` — TUI stays interactive
- `internal/cli/sync.go`, `internal/cli/lint.go`
- `scripts/*.sh`
- `README.md`

## Conventions

- All new code uses `set -e`-style early returns. No silent failures.
- `--json` emits to stdout. Errors in `--json` mode also go to stdout as
  a JSON `{"error": "..."}` object — do NOT mix JSON stdout with stderr
  text.
- All exit codes match the table at the top of this doc.
- For `--json`, never emit ANSI escape codes. Use `json.NewEncoder` with
  `SetIndent("", "  ")` for pretty output (matches the existing Lip
  Gloss output shape — both are intended to be human-glanceable).
- Don't add an interactive prompt for any field that has a flag value
  already provided. Pre-population in the form is OK; auto-confirmation
  is not.
- `cobra.NoArgs` already on the command; preserve it.

## Acceptance criteria

Run from the repo root after implementation:

```bash
go build ./...                                # clean
go vet ./...                                  # clean
go test ./...                                 # all green, including new tests

# Build the binary
go build -o ./worklog ./cmd/worklog

# Non-interactive add succeeds without prompting.
fix=$(mktemp -d)
printf '## Now\n## Next\n## Someday\n' > "$fix/WORK.md"
./worklog --dir "$fix" add --title 'Test' --id test-1 --section Next
test "$?" = "0" && echo OK

# JSON output for status validates as JSON.
./worklog --dir "$fix" status --json | python3 -c 'import json,sys; json.load(sys.stdin)' && echo OK

# JSON output for validate validates as JSON.
./worklog --dir "$fix" validate --json | python3 -c 'import json,sys; json.load(sys.stdin)' && echo OK

# Non-interactive failure mode exits 64.
./worklog --dir "$fix" add </dev/null
test "$?" = "64" && echo OK

# Skill files updated.
grep -q "Preferred CLI invocations for agents" skill/SKILL.md && echo OK
grep -q "Preferred CLI invocations for agents" skill/claude/command.md && echo OK

# Rule-block markers still intact.
grep -q '^<!-- rules:start -->$' skill/SKILL.md && echo OK
grep -q '^<!-- rules:end -->$' skill/SKILL.md && echo OK

rm -rf "$fix"
```

When all `OK` lines appear and tests pass, hand back to the user with:
- A summary of what landed.
- Anything surprising or that you had to make a judgment call on
  (especially in skill text wording, form pre-population behavior, or
  JSON schema details).
- The exit code from `./worklog lint-specs` (the new SKILL.md section
  will create new drift; this is expected and the user wants to know
  about it).

## Failure handling

If you get stuck:
- For the form pre-population — if Huh's API for setting initial Value
  via pointer doesn't behave as expected, fall back to setting the
  pointer just before each `huh.NewInput().Value(&p)` call so the form
  reads from the current pointer state. Do not skip the form-mode test
  case; report what you couldn't make work.
- For JSON output — if the schema is ambiguous, prefer including a field
  with an explicit zero value over omitting it. Stable schemas help
  agents.
- For SKILL.md placement — if you can't find a clean insertion point
  before "Required behavior", insert immediately after the existing
  "Layout" block; document where you put it.
- For tests — if a Huh-dependent test would require a TTY, skip it with
  `t.Skip("requires TTY")` rather than fail.

When in doubt about a non-mechanical decision, surface it in your final
report rather than guess.
