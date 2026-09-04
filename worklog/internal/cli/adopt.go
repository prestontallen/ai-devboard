package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/prestontallen/ai-devboard/worklog/internal/adopt"
	"github.com/prestontallen/ai-devboard/worklog/internal/devboard"
	"github.com/prestontallen/ai-devboard/worklog/internal/freeze"
	"github.com/prestontallen/ai-devboard/worklog/internal/migrate"
	"github.com/prestontallen/ai-devboard/worklog/internal/reindex"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/memstore"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/sqlitestore"
)

func newAdoptCmd() *cobra.Command {
	var (
		flagCommit   bool
		flagRollback string
	)

	cmd := &cobra.Command{
		Use:           "adopt",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		Short:         "Adopt a corpus that predates the store, or preview what that would do",
		Long: `adopt brings a worklog directory that predates the SQLite store into a
state the store-backed write path accepts, without losing a byte.

A dry run is the default: it prints what would change and writes nothing.
--commit performs it, behind a freeze, after taking a verbatim
digest-verified snapshot of both live directories.

Every check runs before the snapshot, and each one refuses rather than
proceeding:

  census   every file under both roots is accounted for, or the strays
           are named — the readers skip unrecognised files silently
  convert  the strict parser's own refusals, unchanged
  hazard   constructs the parsers drop WITHOUT refusing, such as content
           before WORK.md's first section, an archive entry with no
           Completed, a devboard title the reader never reads, or YAML
           comments
  stale    a ticket in the store but not in this corpus would be rendered
           back onto disk, resurrecting it

A failure after the snapshot rolls back before returning, so a
half-applied corpus is not a reachable end state. --rollback <dir> restores
an earlier snapshot at any later date.

Exit codes:
  0  adopted, or a clean dry run
  1  error
  2  refused (the message names what to fix)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagRollback != "" {
				return runAdoptRollback(cmd, flagRollback)
			}
			return runAdopt(cmd, flagCommit)
		},
	}
	cmd.Flags().BoolVar(&flagCommit, "commit", false, "perform the adoption (default is a dry run that writes nothing)")
	cmd.Flags().StringVar(&flagRollback, "rollback", "", "restore both live directories from a snapshot directory")
	return cmd
}

func adoptRoots() (adopt.Roots, string, error) {
	wd, err := resolveWorkdir()
	if err != nil {
		return adopt.Roots{}, "", err
	}
	dataDir, err := storeDataDir()
	if err != nil {
		return adopt.Roots{}, "", err
	}
	return adopt.Roots{Worklog: wd.Root, Devboard: devboard.DataDir()}, dataDir, nil
}

func runAdopt(cmd *cobra.Command, commit bool) error {
	roots, dataDir, err := adoptRoots()
	if err != nil {
		return err
	}
	wd, err := resolveWorkdir()
	if err != nil {
		return err
	}

	// The snapshot lives under the migration data dir, deliberately outside
	// both live roots: ReadCorpusDir would otherwise ingest it as corpus.
	dest := filepath.Join(dataDir, adopt.StampName(time.Now().UTC().Format("20060102T150405Z")))

	opts := adopt.Options{Roots: roots, SnapshotDir: dest, Apply: commit}
	opts.Reindex = func() error {
		_, err := reindex.Run(wd, reindex.Inputs{})
		return err
	}

	if commit {
		// Hold the freeze across the whole window, so no concurrent verb
		// writes between the conversion and the render.
		if _, err := freeze.Acquire(wd.Root, "adopt"); err != nil {
			return errWithExit(1, "adopt: %v", err)
		}
		defer freeze.Release(wd.Root)
	}

	// A dry run converts into an ephemeral store and leaves no trace. A
	// commit converts into the PRODUCTION database, at the same path every
	// write verb opens — otherwise adoption canonicalises the corpus and
	// the machine still has no store to write through, which is the state
	// this command exists to end.
	s, closeStore, err := adoptStore(dataDir, commit)
	if err != nil {
		return errWithExit(1, "adopt: %v", err)
	}
	defer closeStore()

	res, err := adopt.Run(s, opts)
	if err != nil {
		if errors.Is(err, adopt.ErrRefused) {
			return errWithExit(2, "%v", err)
		}
		return errWithExit(1, "adopt: %v", err)
	}

	counts := res.Plan.Counts()
	for _, op := range []adopt.Op{adopt.OpCreate, adopt.OpRewrite, adopt.OpDelete, adopt.OpKeep, adopt.OpProduce, adopt.OpDerived} {
		if counts[op] > 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "%8s %d\n", string(op), counts[op])
		}
	}
	if !commit {
		if res.Plan.Writes() {
			fmt.Fprintf(cmd.OutOrStdout(), "\ndry run — nothing was written. re-run with --commit to adopt.\n")
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "\nalready adopted — nothing to do.\n")
		}
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "\nsnapshot: %s (%s)\nadopted — `worklog adopt --rollback %s` restores this exactly.\n",
		dest, res.Snapshot.Describe(), dest)
	return nil
}

// adoptStore picks the store a run converts into: ephemeral for a preview,
// the real database for a commit.
func adoptStore(dataDir string, commit bool) (store.Store, func(), error) {
	if !commit {
		return memstore.New(), func() {}, nil
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, func() {}, err
	}
	s, err := sqlitestore.Open(migrate.OutputPath(dataDir))
	if err != nil {
		return nil, func() {}, err
	}
	return s, func() { s.Close() }, nil
}

func runAdoptRollback(cmd *cobra.Command, dir string) error {
	roots, _, err := adoptRoots()
	if err != nil {
		return err
	}
	wd, err := resolveWorkdir()
	if err != nil {
		return err
	}
	if _, err := freeze.Acquire(wd.Root, "adopt --rollback"); err != nil {
		return errWithExit(1, "adopt: %v", err)
	}
	defer freeze.Release(wd.Root)

	if err := adopt.Restore(dir, roots); err != nil {
		return errWithExit(1, "adopt: %v", err)
	}
	m, err := adopt.LoadManifest(dir)
	if err != nil {
		return errWithExit(1, "adopt: %v", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "restored %s from %s\n", m.Describe(), dir)
	return nil
}
