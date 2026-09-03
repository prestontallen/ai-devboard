package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/note"
	"github.com/prestontallen/ai-devboard/worklog/internal/storesync"
	"github.com/prestontallen/ai-devboard/worklog/internal/style"
)

func newNoteCmd() *cobra.Command {
	var (
		flagEdit   bool
		flagEditor bool
		flagJSON   bool
	)
	cmd := &cobra.Command{
		Use:   "note <id> [text]",
		Short: "Append a timestamped note to notes/<id>.md, or read existing notes",
		Long: `Each invocation with text (positional or --edit) appends one timestamped
section to the ticket's notes file. With no text and no --edit, prints
the existing notes. notes/<id>.md is lazy-created on first use; for
non-epic tickets, **Notes**: notes/<id>.md is auto-added to the WORK.md
block.

Output modes:
  default + TTY  : Glamour-rendered styled markdown (read mode)
  default + pipe : raw markdown (read mode)
  --json         : structured JSON (both modes)`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			text := ""
			hasText := len(args) == 2
			if hasText {
				text = args[1]
			}
			return runNote(cmd, id, text, hasText, flagEdit, flagEditor, flagJSON)
		},
	}
	cmd.Flags().BoolVar(&flagEdit, "edit", false, "open a Huh multi-line input to type a new note")
	cmd.Flags().BoolVar(&flagEditor, "editor", false, "open notes/<id>.md in $EDITOR (falls back to vi)")
	cmd.Flags().BoolVar(&flagJSON, "json", false, "emit structured JSON output")
	return cmd
}

func runNote(cmd *cobra.Command, id, text string, hasText, edit, editor, asJSON bool) error {
	if (text != "" && (edit || editor)) || (edit && editor) {
		return jsonOrTextError(cmd, asJSON, 64, "note: --edit, --editor, and positional text are mutually exclusive")
	}

	wd, err := resolveWorkdir()
	if err != nil {
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}

	id = strings.ToLower(strings.TrimSpace(id))

	if editor {
		return runNoteEditor(cmd, wd, id, asJSON)
	}

	// Write mode: positional text was supplied or --edit flag is set.
	if hasText || edit {
		body := text
		if edit {
			if !stdinIsTTY() {
				return jsonOrTextError(cmd, asJSON, 64, "note --edit requires a TTY on stdin")
			}
			var formVal string
			form := huh.NewForm(
				huh.NewGroup(
					huh.NewText().
						Title("New note for " + id).
						Description("Markdown body. Submit (Ctrl+D) appends; cancel discards.").
						Value(&formVal),
				),
			)
			if err := form.Run(); err != nil {
				return jsonOrTextError(cmd, asJSON, 1, "%v", err)
			}
			body = formVal
		}

		if strings.TrimSpace(body) == "" {
			return jsonOrTextError(cmd, asJSON, 64, "%v", note.ErrEmptyBody)
		}

		res, err := note.Append(wd, id, body, time.Now())
		if err != nil {
			if errors.Is(err, note.ErrEmptyBody) {
				return jsonOrTextError(cmd, asJSON, 64, "%v", err)
			}
			if errors.Is(err, note.ErrUnknownID) {
				return jsonOrTextError(cmd, asJSON, 1, "%v", err)
			}
			return jsonOrTextError(cmd, asJSON, 1, "%v", err)
		}
		storesync.WarnAfterWrite(wd)
		if asJSON {
			return emitJSON(cmd.OutOrStdout(), res)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "appended: %s %s\n", res.Appended.Timestamp, res.File)
		return nil
	}

	// Read mode.
	res, err := note.Read(wd, id)
	if err != nil {
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}
	if asJSON {
		return emitJSON(cmd.OutOrStdout(), res)
	}
	if !res.Exists {
		fmt.Fprintln(cmd.OutOrStdout(), style.Dim.Render("no notes for "+id))
		return nil
	}
	md := buildNoteMarkdown(res)
	if stdoutIsTTY() {
		if rendered, err := renderMarkdown(md); err == nil {
			fmt.Fprint(cmd.OutOrStdout(), rendered)
			return nil
		}
	}
	fmt.Fprint(cmd.OutOrStdout(), md)
	return nil
}

func runNoteEditor(cmd *cobra.Command, wd model.Workdir, id string, asJSON bool) error {
	if !stdinIsTTY() || !stdoutIsTTY() {
		return jsonOrTextError(cmd, asJSON, 64, "note --editor requires a TTY")
	}

	path, created, linked, err := note.EnsureFile(wd, id)
	if err != nil {
		if errors.Is(err, note.ErrUnknownID) {
			return jsonOrTextError(cmd, asJSON, 1, "%v", err)
		}
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}

	editorBin := os.Getenv("EDITOR")
	if editorBin == "" {
		editorBin = "vi"
	}

	proc := exec.Command(editorBin, path)
	proc.Stdin = os.Stdin
	proc.Stdout = os.Stdout
	proc.Stderr = os.Stderr
	if err := proc.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return jsonOrTextError(cmd, asJSON, 1, "editor exited with code %d", exitErr.ExitCode())
		}
		return jsonOrTextError(cmd, asJSON, 1, "launch editor: %v", err)
	}
	storesync.WarnAfterWrite(wd)

	if asJSON {
		return emitJSON(cmd.OutOrStdout(), map[string]any{
			"id":             id,
			"file":           path,
			"createdFile":    created,
			"linkedInWorkMD": linked,
			"editorExitCode": 0,
		})
	}
	fmt.Fprintf(cmd.OutOrStdout(), "edited: %s\n", path)
	return nil
}

func buildNoteMarkdown(res note.ParseResult) string {
	var sb strings.Builder
	if res.Preamble != "" {
		sb.WriteString(res.Preamble)
		sb.WriteString("\n\n")
	}
	for i, e := range res.Entries {
		fmt.Fprintf(&sb, "## %s\n%s", e.Timestamp, e.Body)
		if i < len(res.Entries)-1 {
			sb.WriteString("\n\n")
		} else {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}
