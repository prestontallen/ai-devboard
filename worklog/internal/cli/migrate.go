package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/prestontallen/ai-devboard/worklog/internal/devboard"
	"github.com/prestontallen/ai-devboard/worklog/internal/migrate"
)

func newMigrateCmd() *cobra.Command {
	var (
		flagOut  string
		flagJSON bool
	)

	cmd := &cobra.Command{
		Use:   "migrate",
		Args:  cobra.NoArgs,
		Short: "Rehearse the worklog2 storage migration against a copy of live data",
		Long: `migrate is a rehearsal tool for the worklog rewrite (adb-worklog-rewrite):
it copies your live worklog dir and devboard dir (read-only — the live dirs
are never written), converts the copy into a persisted SQLite database via
copy-forward, and reports whether entity identity held steady across the
run: an id-set diff of every ticket and sub-item ULID, plus any rows left
over from a prior run that no longer exist in live data.

This is a rehearsal, not the migration itself — it builds confidence
before the eventual one-way cutover, and can be run as many times as you
like.

The output database, its one-generation backup, and migrate's own scratch
copies live under one directory: --out, or $WORKLOG_MIGRATION_DATA, or
~/.local/share/worklog-migration by default.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMigrate(cmd, flagOut, flagJSON)
		},
	}
	cmd.Flags().StringVar(&flagOut, "out", "",
		"migrate's data directory (default $WORKLOG_MIGRATION_DATA or ~/.local/share/worklog-migration)")
	cmd.Flags().BoolVar(&flagJSON, "json", false, "emit a single JSON document (report + id-set diff) instead of styled text")
	return cmd
}

func resolveMigrateDataDir(flagOut string) (string, error) {
	if flagOut != "" {
		return flagOut, nil
	}
	if env := os.Getenv("WORKLOG_MIGRATION_DATA"); env != "" {
		return env, nil
	}
	return migrate.DefaultDataDir()
}

func runMigrate(cmd *cobra.Command, flagOut string, asJSON bool) error {
	wd, err := resolveWorkdir()
	if err != nil {
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}
	dataDir, err := resolveMigrateDataDir(flagOut)
	if err != nil {
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}

	res, err := migrate.Run(migrate.Options{
		Sources: migrate.Sources{
			WorklogDir:  wd.Root,
			DevboardDir: devboard.DataDir(),
		},
		DataDir: dataDir,
	})
	if err != nil {
		return jsonOrTextError(cmd, asJSON, 1, "migrate: %v", err)
	}

	outputPath := migrate.OutputPath(dataDir)
	if asJSON {
		return emitJSON(cmd.OutOrStdout(), migrateJSON{
			Tickets:  res.Report.Tickets,
			Feedback: res.Report.Feedback,
			Skipped:  orEmpty(res.Report.Skipped),
			Warnings: orEmpty(res.Report.Warnings),
			Diff: migrate.IDSetDiff{
				Added:     orEmpty(res.Diff.Added),
				Removed:   orEmpty(res.Diff.Removed),
				Unchanged: res.Diff.Unchanged,
				Changed:   orEmptyChanged(res.Diff.Changed),
			},
			StaleRows:  orEmpty(res.StaleRows),
			BackedUp:   res.BackedUp,
			OutputPath: outputPath,
		})
	}

	fmt.Fprint(cmd.OutOrStdout(), migrateSummary(res, outputPath))
	return nil
}

// migrateJSON is the single JSON document criterion 9 requires: the
// conversion report and the id-set diff together, success or refusal.
type migrateJSON struct {
	Tickets    int               `json:"tickets"`
	Feedback   int               `json:"feedback"`
	Skipped    []string          `json:"skipped"`
	Warnings   []string          `json:"warnings"`
	Diff       migrate.IDSetDiff `json:"diff"`
	StaleRows  []string          `json:"staleRows"`
	BackedUp   bool              `json:"backedUp"`
	OutputPath string            `json:"outputPath"`
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func orEmptyChanged(c []migrate.ChangedID) []migrate.ChangedID {
	if c == nil {
		return []migrate.ChangedID{}
	}
	return c
}

// migrateSummary renders the human-readable verdict (criterion 14): the
// trustworthiness of the run should be legible without --json.
func migrateSummary(res *migrate.Result, outputPath string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "converted %d tickets, %d feedback entries -> %s\n", res.Report.Tickets, res.Report.Feedback, outputPath)
	if res.BackedUp {
		fmt.Fprintf(&sb, "previous generation backed up to %s.bak\n", outputPath)
	} else {
		fmt.Fprintln(&sb, "no prior output db — this is the baseline run")
	}

	d := res.Diff
	fmt.Fprintf(&sb, "id-set diff: %d added, %d removed, %d unchanged, %d changed\n",
		len(d.Added), len(d.Removed), d.Unchanged, len(d.Changed))
	if len(d.Changed) > 0 {
		fmt.Fprintln(&sb, "  CHANGED — identity did not hold steady across this run:")
		for _, c := range d.Changed {
			fmt.Fprintf(&sb, "    %s: %s -> %s\n", c.Key, c.Old, c.New)
		}
	}

	if len(res.StaleRows) > 0 {
		fmt.Fprintf(&sb, "stale rows (in the db, not in this run's live data): %s\n", strings.Join(res.StaleRows, ", "))
	}
	if len(res.Report.Skipped) > 0 {
		fmt.Fprintf(&sb, "skipped producer files: %s\n", strings.Join(res.Report.Skipped, ", "))
	}
	for _, w := range res.Report.Warnings {
		fmt.Fprintf(&sb, "warning: %s\n", w)
	}
	return sb.String()
}
