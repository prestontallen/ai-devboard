package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/parse"
	"github.com/prestontallen/ai-devboard/worklog/internal/reindex"
	"github.com/prestontallen/ai-devboard/worklog/internal/render"
	"github.com/prestontallen/ai-devboard/worklog/internal/storesync"
	"github.com/prestontallen/ai-devboard/worklog/internal/style"
)

// addInputs is the collected user intent for a new ticket — populated either
// from flags, from the Huh form, or a mix of both.
type addInputs struct {
	Title      string
	ID         string
	Repo       string
	Tags       []string
	Acceptance string
	Section    string // "Next" or "Someday" (ignored for child path)
	Type       string // "ticket" (default), "epic", "spike", or "chore"
	Parent     string // <epic-id>; non-empty triggers child path
}

// addOutput is the structured success payload for `worklog add`. The new
// `Kind`, `Parent`, and `NotesPath` fields disambiguate the three branches
// (standalone / epic / child); pre-existing fields remain stable.
type addOutput struct {
	Status    string   `json:"status"`
	Kind      string   `json:"kind"` // "ticket" | "epic" | "child"
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Section   string   `json:"section,omitempty"`   // empty for child
	Parent    string   `json:"parent,omitempty"`    // populated for child
	WorkMD    string   `json:"workMD,omitempty"`    // empty for child
	NotesPath string   `json:"notesPath,omitempty"` // populated for epic + child
	Warnings  []string `json:"warnings"`
}

const indexNotUpdatedWarning = "INDEX.md not updated; run `worklog reindex` when convenient"

// Sentinel errors for the new branches.
var (
	ErrEpicHasNoParent    = errors.New("--type epic and --parent cannot be combined")
	ErrSpikeHasNoParent   = errors.New("--type spike and --parent cannot be combined: a spike is always standalone")
	ErrInvalidAddType     = errors.New("--type must be one of: ticket, epic, spike, chore")
	ErrParentEpicNotFound = errors.New("--parent must resolve to an epic block in WORK.md")
	ErrNotesAlreadyExists = errors.New("notes file for this epic ID already exists")
	ErrIDCollisionInNotes = errors.New("ID already exists as an open child in a notes file")
)

// validAddTypes mirrors model.BlockType's named values. `chore` is accepted
// but carries no behavior; `spike` puts dev-context on its research track.
var validAddTypes = map[string]bool{
	string(model.TypeTicket): true,
	string(model.TypeEpic):   true,
	string(model.TypeSpike):  true,
	string(model.TypeChore):  true,
}

func newAddCmd() *cobra.Command {
	var (
		flagTitle      string
		flagID         string
		flagRepo       string
		flagTagsCSV    string
		flagAcceptance string
		flagSection    string
		flagType       string
		flagParent     string
		flagJSON       bool
	)

	cmd := &cobra.Command{
		Use:   "add",
		Args:  cobra.NoArgs,
		Short: "Add a new ticket, epic, or child of an epic (form by default for standalone tickets)",
		Long: `Add a new entry to the worklog. Three paths:

Standalone ticket (default, has form fallback):
  worklog add --title "Refactor auth" --id auth-1 --section Next --json

Epic (flag-only, creates notes/<id>.md scaffold):
  worklog add --type epic --id epic-a --title "Cross-cutting effort" --json

Child of an existing epic (flag-only, appends to notes/<epic-id>.md):
  worklog add --parent epic-a --id child-1 --title "First sub-task" --json

Required for standalone + epic: --title, --id.
Required for child: --title, --id, --parent.

Without flags and under a TTY (standalone only): opens a Huh form.
Without a TTY and without required flags: fails fast with exit 64.

INDEX.md is not auto-updated. Run ` + "`worklog reindex`" + ` periodically.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAdd(cmd,
				flagTitle, flagID, flagRepo, flagTagsCSV,
				flagAcceptance, flagSection, flagType, flagParent, flagJSON)
		},
	}

	cmd.Flags().StringVar(&flagTitle, "title", "", "title (required for non-interactive)")
	cmd.Flags().StringVar(&flagID, "id", "", "lowercase-kebab ID (required for non-interactive)")
	cmd.Flags().StringVar(&flagRepo, "repo", "", "repository name")
	cmd.Flags().StringVar(&flagTagsCSV, "tags", "", "comma-separated tags")
	cmd.Flags().StringVar(&flagAcceptance, "acceptance", "", "one-line acceptance criterion (standalone only)")
	cmd.Flags().StringVar(&flagSection, "section", "Next", "destination section: Next or Someday")
	cmd.Flags().StringVar(&flagType, "type", "ticket", "ticket type: ticket (default), epic, spike, or chore")
	cmd.Flags().StringVar(&flagParent, "parent", "", "for child-of-epic: the parent epic's ID")
	cmd.Flags().BoolVar(&flagJSON, "json", false, "emit JSON status object instead of styled text")

	return cmd
}

func runAdd(
	cmd *cobra.Command,
	flagTitle, flagID, flagRepo, flagTagsCSV,
	flagAcceptance, flagSection, flagType, flagParent string,
	flagJSON bool,
) error {
	wd, err := resolveWorkdir()
	if err != nil {
		return jsonOrTextError(cmd, flagJSON, 1, "%v", err)
	}
	doc, err := parse.File(wd.WorkMD())
	if err != nil {
		if errors.Is(err, model.ErrWorkMDMissing) {
			return jsonOrTextError(cmd, flagJSON, 1,
				"WORK.md not found at %s", wd.WorkMD())
		}
		return jsonOrTextError(cmd, flagJSON, 1, "%v", err)
	}

	inputs := addInputs{
		Title:      strings.TrimSpace(flagTitle),
		ID:         strings.ToLower(strings.TrimSpace(flagID)),
		Repo:       strings.TrimSpace(flagRepo),
		Acceptance: strings.TrimSpace(flagAcceptance),
		Section:    strings.TrimSpace(flagSection),
		Type:       strings.ToLower(strings.TrimSpace(flagType)),
		Parent:     strings.ToLower(strings.TrimSpace(flagParent)),
	}
	if inputs.Section == "" {
		inputs.Section = "Next"
	}
	if inputs.Type == "" {
		inputs.Type = "ticket"
	}
	inputs.Tags = splitTags(flagTagsCSV)

	// An unknown --type is refused rather than silently falling through to the
	// standalone path, where it would be dropped without trace.
	if !validAddTypes[inputs.Type] {
		return jsonOrTextError(cmd, flagJSON, 64, "%v (got %q)", ErrInvalidAddType, inputs.Type)
	}

	// Determine which branch to take.
	switch {
	case inputs.Type == "epic" && inputs.Parent != "":
		return jsonOrTextError(cmd, flagJSON, 1, "%v", ErrEpicHasNoParent)
	case inputs.Type == "spike" && inputs.Parent != "":
		return jsonOrTextError(cmd, flagJSON, 1, "%v", ErrSpikeHasNoParent)
	case inputs.Parent != "":
		return runAddChild(cmd, wd, doc, inputs, flagJSON)
	case inputs.Type == "epic":
		return runAddEpic(cmd, wd, doc, inputs, flagJSON)
	default:
		return runAddStandalone(cmd, wd, doc, inputs, flagJSON)
	}
}

// --- standalone path (form fallback for humans) -----------------------------

func runAddStandalone(cmd *cobra.Command, wd model.Workdir, doc *model.WorkDoc, inputs addInputs, flagJSON bool) error {
	hasRequired := inputs.Title != "" && inputs.ID != ""
	needsForm := !hasRequired
	tty := stdinIsTTY()

	switch {
	case needsForm && !tty:
		return jsonOrTextError(cmd, flagJSON, 64,
			"add requires --title and --id when stdin is not a TTY")
	case needsForm && tty:
		if err := promptAddForm(doc, &inputs); err != nil {
			return jsonOrTextError(cmd, flagJSON, 1, "%v", err)
		}
	}

	if err := validateStandaloneInputs(wd, doc, inputs); err != nil {
		return jsonOrTextError(cmd, flagJSON, 1, "%v", err)
	}

	var out addOutput
	var err error
	if storeWriteEnabled() {
		out, err = runStoreAdd(wd, inputs)
	} else {
		out, err = applyStandalone(wd, doc, inputs)
		if err == nil {
			storesync.WarnAfterWrite(wd)
		}
	}
	if err != nil {
		return jsonOrTextError(cmd, flagJSON, 1, "%v", err)
	}

	if flagJSON {
		return emitJSON(cmd.OutOrStdout(), out)
	}
	emitAddText(cmd.OutOrStdout(), out)
	return nil
}

func validateStandaloneInputs(wd model.Workdir, doc *model.WorkDoc, inputs addInputs) error {
	if inputs.Title == "" {
		return fmt.Errorf("title is required")
	}
	if inputs.ID == "" {
		return fmt.Errorf("ID is required")
	}
	if doc.BlockByID(inputs.ID) != nil {
		return fmt.Errorf("ID %q already exists in WORK.md", inputs.ID)
	}
	if path, ok := idExistsInNotes(wd, inputs.ID); ok {
		return fmt.Errorf("%w: %q already in %s", ErrIDCollisionInNotes, inputs.ID, path)
	}
	switch inputs.Section {
	case "Next", "Someday":
		// OK
	default:
		return fmt.Errorf("section %q is invalid (use Next or Someday)", inputs.Section)
	}
	return nil
}

// Kept for backward-compat with existing tests that call validateAddInputs directly.
// Wraps validateStandaloneInputs without the notes-file check.
func validateAddInputs(doc *model.WorkDoc, inputs addInputs) error {
	if inputs.Title == "" {
		return fmt.Errorf("title is required")
	}
	if inputs.ID == "" {
		return fmt.Errorf("ID is required")
	}
	if doc.BlockByID(inputs.ID) != nil {
		return fmt.Errorf("ID %q already exists in WORK.md", inputs.ID)
	}
	switch inputs.Section {
	case "Next", "Someday":
		// OK
	default:
		return fmt.Errorf("section %q is invalid (use Next or Someday)", inputs.Section)
	}
	return nil
}

func applyStandalone(wd model.Workdir, doc *model.WorkDoc, inputs addInputs) (addOutput, error) {
	// `ticket` is the implied default and stays off the block, so ordinary
	// tickets render exactly as before; spike/chore are written explicitly.
	blockType := inputs.Type
	if blockType == string(model.TypeTicket) {
		blockType = ""
	}
	blockLines := render.FormatTicketBlock(render.BlockOptions{
		Title:      inputs.Title,
		ID:         inputs.ID,
		Type:       blockType,
		Repo:       inputs.Repo,
		Tags:       inputs.Tags,
		Acceptance: inputs.Acceptance,
		State:      model.StatePending,
	})

	out, err := render.AppendToSection(doc, model.SectionName(inputs.Section), blockLines)
	if err != nil {
		return addOutput{}, err
	}
	if err := render.WriteAtomic(wd.WorkMD(), out); err != nil {
		return addOutput{}, err
	}
	return addOutput{
		Status:   "added",
		Kind:     "ticket",
		ID:       inputs.ID,
		Title:    inputs.Title,
		Section:  inputs.Section,
		WorkMD:   wd.WorkMD(),
		Warnings: []string{indexNotUpdatedWarning},
	}, nil
}

// applyAdd is retained as an alias for tests that target the standalone path.
func applyAdd(wd model.Workdir, doc *model.WorkDoc, inputs addInputs) (addOutput, error) {
	return applyStandalone(wd, doc, inputs)
}

// --- epic path --------------------------------------------------------------

func runAddEpic(cmd *cobra.Command, wd model.Workdir, doc *model.WorkDoc, inputs addInputs, flagJSON bool) error {
	if inputs.Title == "" || inputs.ID == "" {
		return jsonOrTextError(cmd, flagJSON, 64,
			"epic add requires --title and --id (no form fallback for epics)")
	}
	if doc.BlockByID(inputs.ID) != nil {
		return jsonOrTextError(cmd, flagJSON, 1,
			"ID %q already exists in WORK.md", inputs.ID)
	}
	if path, ok := idExistsInNotes(wd, inputs.ID); ok {
		return jsonOrTextError(cmd, flagJSON, 1,
			"%v: %q already in %s", ErrIDCollisionInNotes, inputs.ID, path)
	}
	switch inputs.Section {
	case "Next", "Someday":
		// OK
	default:
		return jsonOrTextError(cmd, flagJSON, 1,
			"section %q is invalid (use Next or Someday)", inputs.Section)
	}
	notesPath := wd.NotesFile(inputs.ID)
	if _, err := os.Stat(notesPath); err == nil {
		return jsonOrTextError(cmd, flagJSON, 1,
			"%v: %s", ErrNotesAlreadyExists, notesPath)
	}

	var out addOutput
	if storeWriteEnabled() {
		var err error
		out, err = runStoreAdd(wd, inputs)
		if err != nil {
			return jsonOrTextError(cmd, flagJSON, 1, "%v", err)
		}
	} else {
		// Build epic block + splice into WORK.md
		notesRef := "notes/" + inputs.ID + ".md"
		blockLines := render.FormatEpicBlock(render.EpicBlockOptions{
			ID:       inputs.ID,
			Title:    inputs.Title,
			Repo:     inputs.Repo,
			Tags:     inputs.Tags,
			NotesRef: notesRef,
		})
		newWork, err := render.AppendToSection(doc, model.SectionName(inputs.Section), blockLines)
		if err != nil {
			return jsonOrTextError(cmd, flagJSON, 1, "%v", err)
		}

		// Create notes scaffold
		scaffold := epicScaffold(inputs.Title, inputs.ID)
		if err := os.MkdirAll(wd.NotesDir(), 0o755); err != nil {
			return jsonOrTextError(cmd, flagJSON, 1, "mkdir notes: %v", err)
		}
		if err := os.WriteFile(notesPath, []byte(scaffold), 0o644); err != nil {
			return jsonOrTextError(cmd, flagJSON, 1, "write notes: %v", err)
		}

		if err := render.WriteAtomic(wd.WorkMD(), newWork); err != nil {
			return jsonOrTextError(cmd, flagJSON, 1, "write WORK.md: %v", err)
		}
		storesync.WarnAfterWrite(wd)

		out = addOutput{
			Status:    "added",
			Kind:      "epic",
			ID:        inputs.ID,
			Title:     inputs.Title,
			Section:   inputs.Section,
			WorkMD:    wd.WorkMD(),
			NotesPath: notesPath,
			Warnings:  []string{indexNotUpdatedWarning},
		}
	}

	if flagJSON {
		return emitJSON(cmd.OutOrStdout(), out)
	}
	w := cmd.OutOrStdout()
	fmt.Fprintln(w, style.Good.Render(fmt.Sprintf("added epic %s to ## %s",
		strings.ToUpper(out.ID), out.Section)))
	fmt.Fprintln(w, style.Dim.Render("  notes: "+out.NotesPath))
	emitWarnings(w, out.Warnings)
	return nil
}

func epicScaffold(title, id string) string {
	return fmt.Sprintf(`# %s

<!-- Notes for epic %s. Children added via `+"`worklog add --parent %s`"+`
     appear under ## Children as `+"`- [ ] <child-id>: <title>`"+` lines.
     Promote to ## Now via `+"`worklog start <child-id>`"+`. Done flips
     [ ] -> [x] automatically. -->

## Background

(Fill in: Jira link, plan reference, open questions.)

## Children
`, title, id, id)
}

// --- child path -------------------------------------------------------------

func runAddChild(cmd *cobra.Command, wd model.Workdir, doc *model.WorkDoc, inputs addInputs, flagJSON bool) error {
	if inputs.Title == "" || inputs.ID == "" {
		return jsonOrTextError(cmd, flagJSON, 64,
			"child add requires --title, --id, and --parent (no form fallback)")
	}
	parent := doc.BlockByID(inputs.Parent)
	if parent == nil || !parent.IsEpic() {
		if hint := reindex.ArchivedHint(wd.ArchiveDir(), inputs.Parent); hint != "" {
			return jsonOrTextError(cmd, flagJSON, 1,
				"%v: epic %q was %s; archived epics cannot take new children",
				ErrParentEpicNotFound, inputs.Parent, hint)
		}
		return jsonOrTextError(cmd, flagJSON, 1,
			"%v: %q", ErrParentEpicNotFound, inputs.Parent)
	}
	if doc.BlockByID(inputs.ID) != nil {
		return jsonOrTextError(cmd, flagJSON, 1,
			"ID %q already exists in WORK.md", inputs.ID)
	}
	if path, ok := idExistsInNotes(wd, inputs.ID); ok {
		return jsonOrTextError(cmd, flagJSON, 1,
			"%v: %q already in %s", ErrIDCollisionInNotes, inputs.ID, path)
	}

	var out addOutput
	if storeWriteEnabled() {
		var err error
		out, err = runStoreAdd(wd, inputs)
		if err != nil {
			return jsonOrTextError(cmd, flagJSON, 1, "%v", err)
		}
	} else {
		notesPath := wd.NotesFile(inputs.Parent)
		var notesBytes []byte
		if data, err := os.ReadFile(notesPath); err == nil {
			notesBytes = data
		} else if !errors.Is(err, os.ErrNotExist) {
			return jsonOrTextError(cmd, flagJSON, 1, "read notes: %v", err)
		}

		newBytes := render.AppendChildToNotes(notesBytes, inputs.ID, inputs.Title)

		if err := os.MkdirAll(filepath.Dir(notesPath), 0o755); err != nil {
			return jsonOrTextError(cmd, flagJSON, 1, "mkdir notes: %v", err)
		}
		if err := os.WriteFile(notesPath, newBytes, 0o644); err != nil {
			return jsonOrTextError(cmd, flagJSON, 1, "write notes: %v", err)
		}
		storesync.WarnAfterWrite(wd)

		out = addOutput{
			Status:    "added",
			Kind:      "child",
			ID:        inputs.ID,
			Title:     inputs.Title,
			Parent:    inputs.Parent,
			NotesPath: notesPath,
			Warnings:  []string{indexNotUpdatedWarning},
		}
	}

	if flagJSON {
		return emitJSON(cmd.OutOrStdout(), out)
	}
	w := cmd.OutOrStdout()
	fmt.Fprintln(w, style.Good.Render(fmt.Sprintf("added child %s under epic %s",
		strings.ToUpper(out.ID), strings.ToUpper(out.Parent))))
	fmt.Fprintln(w, style.Dim.Render("  notes: "+out.NotesPath))
	emitWarnings(w, out.Warnings)
	return nil
}

// --- shared helpers ---------------------------------------------------------

func emitWarnings(w io.Writer, warnings []string) {
	for _, warning := range warnings {
		fmt.Fprintln(w,
			style.Warn.Render("NOTE: "+warning)+
				" Run "+style.SubHeading.Render("worklog validate")+
				" to check the rest of the worklog.")
	}
}

func emitAddText(w io.Writer, out addOutput) {
	fmt.Fprintln(w,
		style.Good.Render(fmt.Sprintf("added %s to ## %s",
			strings.ToUpper(out.ID), out.Section)))
	emitWarnings(w, out.Warnings)
}

// idExistsInNotes scans every notes/*.md for a `- [ ]` or `- [x]` line
// whose first token matches id (case-insensitive). Returns the notes file
// path and true on the first match.
func idExistsInNotes(wd model.Workdir, id string) (string, bool) {
	entries, err := os.ReadDir(wd.NotesDir())
	if err != nil {
		return "", false
	}
	re := regexp.MustCompile(`^- \[[ ~x]\]\s+([a-zA-Z0-9_-]+)`)
	target := strings.ToLower(id)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(wd.NotesDir(), e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			m := re.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			if strings.EqualFold(m[1], target) {
				return path, true
			}
		}
	}
	return "", false
}

// promptAddForm runs the interactive Huh form (standalone tickets only).
// Each field's Value is wired to the corresponding pointer on inputs.
func promptAddForm(doc *model.WorkDoc, inputs *addInputs) error {
	tagsTemp := strings.Join(inputs.Tags, ", ")

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Title").
				Description("Short description of the ticket.").
				Value(&inputs.Title).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("title is required")
					}
					return nil
				}),
			huh.NewInput().
				Title("ID").
				Description("Lowercase kebab-case (e.g. ent-3794 or refactor-auth).").
				Value(&inputs.ID).
				Validate(func(s string) error {
					s = strings.ToLower(strings.TrimSpace(s))
					if s == "" {
						return fmt.Errorf("ID is required")
					}
					if doc.BlockByID(s) != nil {
						return fmt.Errorf("ID %q already exists in WORK.md", s)
					}
					return nil
				}),
			huh.NewInput().
				Title("Repo (optional)").
				Value(&inputs.Repo),
			huh.NewInput().
				Title("Tags (optional, comma-separated)").
				Value(&tagsTemp),
			huh.NewInput().
				Title("Acceptance (optional, one-line)").
				Value(&inputs.Acceptance),
			huh.NewSelect[string]().
				Title("Section").
				Options(
					huh.NewOption("Next (default)", "Next"),
					huh.NewOption("Someday", "Someday"),
				).
				Value(&inputs.Section),
		),
	)
	if err := form.Run(); err != nil {
		return err
	}
	inputs.Title = strings.TrimSpace(inputs.Title)
	inputs.ID = strings.ToLower(strings.TrimSpace(inputs.ID))
	inputs.Repo = strings.TrimSpace(inputs.Repo)
	inputs.Acceptance = strings.TrimSpace(inputs.Acceptance)
	inputs.Tags = splitTags(tagsTemp)
	return nil
}

func jsonOrTextError(cmd *cobra.Command, asJSON bool, code int, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	if asJSON {
		_ = emitJSON(cmd.OutOrStdout(), jsonError{Error: msg})
		return errWithExit(code, "")
	}
	return errWithExit(code, "%s", msg)
}

func splitTags(csv string) []string {
	if strings.TrimSpace(csv) == "" {
		return nil
	}
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
