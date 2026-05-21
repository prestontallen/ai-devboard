package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/prestontallen/day2day/internal/importer"
)

func newImportCmd() *cobra.Command {
	var (
		flagFile    string
		flagSection string
		flagDryRun  bool
		flagJSON    bool
	)
	cmd := &cobra.Command{
		Use:          "import",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		Short:        "Import tickets from JSON (single object or array)",
		Long: `Reads ticket JSON from stdin (default) or --file, writes each as a
block in WORK.md.

JSON shape per ticket:
  {
    "id": "auth-1234",
    "title": "Refactor auth middleware",
    "type": "ticket",       // optional; default "ticket"
    "parent": "epic-auth",  // optional; must exist in WORK.md if set
    "repo": "api",
    "tags": ["auth", "refactor"],
    "pr": "#42",
    "section": "next",      // optional; default "next" — one of now/next/someday
    "source": "https://company.atlassian.net/browse/AUTH-1234"
  }

Either a single object or a JSON array is accepted. Each ticket is its
own atomic write; per-ticket failures don't roll back earlier successes.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runImport(cmd, flagFile, flagSection, flagDryRun, flagJSON)
		},
	}
	cmd.Flags().StringVar(&flagFile, "file", "", "read JSON from file (default: stdin)")
	cmd.Flags().StringVar(&flagSection, "section", "", "override section for all tickets (now/next/someday)")
	cmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "validate without writing")
	cmd.Flags().BoolVar(&flagJSON, "json", false, "emit structured JSON output")
	return cmd
}

func runImport(cmd *cobra.Command, flagFile, flagSection string, dryRun, asJSON bool) error {
	// Open input.
	var r *os.File
	if flagFile != "" {
		f, err := os.Open(flagFile)
		if err != nil {
			return jsonOrTextError(cmd, asJSON, 1, "import: open file: %v", err)
		}
		defer f.Close()
		r = f
	} else {
		r = os.Stdin
	}

	tickets, err := importer.Decode(r)
	if err != nil {
		return jsonOrTextError(cmd, asJSON, 64, "import: invalid JSON: %v", err)
	}

	// Validate --section override.
	if flagSection != "" {
		sec := strings.ToLower(flagSection)
		switch sec {
		case "now", "next", "someday":
		default:
			return jsonOrTextError(cmd, asJSON, 64,
				"import: --section must be one of: now, next, someday")
		}
		flagSection = sec
	}

	wd, err := resolveWorkdir()
	if err != nil {
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}

	result, err := importer.Import(wd, tickets, importer.Options{
		SectionOverride: flagSection,
		DryRun:          dryRun,
	})
	if err != nil {
		return jsonOrTextError(cmd, asJSON, 1, "import: %v", err)
	}

	if asJSON {
		if err := emitJSON(cmd.OutOrStdout(), result); err != nil {
			return err
		}
		if len(result.Failed) > 0 {
			return errWithExit(1, "")
		}
		return nil
	}

	w := cmd.OutOrStdout()
	if len(result.Imported) > 0 {
		fmt.Fprintf(w, "Imported %d ticket(s):\n", len(result.Imported))
		for _, imp := range result.Imported {
			prefix := ""
			if dryRun {
				prefix = "(dry-run) "
			}
			fmt.Fprintf(w, "  - %s%s (%s)\n", prefix, imp.ID, imp.Section)
		}
	}
	if len(result.Failed) > 0 {
		fmt.Fprintf(w, "Failed %d ticket(s):\n", len(result.Failed))
		for _, f := range result.Failed {
			id := f.ID
			if id == "" {
				id = "(unknown)"
			}
			fmt.Fprintf(w, "  - [index %d] %s: %s\n", f.Index, id, f.Reason)
		}
		return errWithExit(1, "")
	}
	return nil
}
