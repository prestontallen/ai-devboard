package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/prestontallen/ai-devboard/worklog/internal/lockfile"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/sqlitestore"
	"github.com/prestontallen/ai-devboard/worklog/internal/storepath"
)

// relocateGateWait is how long relocate waits for a concurrent writer to
// finish before giving up. A variable rather than a constant so the test
// for the refusal path does not spend the full wait on every run — the
// behavior under test is "it gives up and says why", not the duration.
var relocateGateWait = 3 * time.Second

func newStoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "store",
		Args:  cobra.NoArgs,
		Short: "Inspect and maintain the store's database",
	}
	cmd.AddCommand(newStoreRelocateCmd())
	return cmd
}

func newStoreRelocateCmd() *cobra.Command {
	var flagFrom string
	cmd := &cobra.Command{
		Use:   "relocate",
		Args:  cobra.NoArgs,
		Short: "Copy a pre-move database into the store's current home",
		Long: `relocate brings a database forward from the location the store used
before it had a real home (~/.local/share/worklog-migration) to the
directory derived from the worklog directory.

It COPIES rather than moves. The original is left exactly where it was, so
a machine that hits trouble still has an untouched database to fall back
to. Delete it yourself once you are satisfied.

relocate refuses rather than risk the copy when a write-ahead log sits
beside the source, or when another process is holding the store's write
gate.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStoreRelocate(cmd, flagFrom)
		},
	}
	cmd.Flags().StringVar(&flagFrom, "from", "",
		"database to copy from (default: the pre-move location)")
	return cmd
}

func runStoreRelocate(cmd *cobra.Command, from string) error {
	if err := refuseRetiredStoreEnv(); err != nil {
		return err
	}
	wd, err := resolveWorkdir()
	if err != nil {
		return err
	}

	src := from
	if src == "" {
		src, err = storepath.LegacyDB()
		if err != nil {
			return err
		}
	}
	dst := storepath.DB(wd.Root)

	if _, err := os.Stat(dst); err == nil {
		return errWithExit(1, "there is already a database at %s — nothing to relocate", dst)
	} else if !os.IsNotExist(err) {
		return err
	}
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return errWithExit(1, "no database at the old location %s — nothing to relocate", src)
	} else if err != nil {
		return err
	}

	// A -wal beside the source holds committed transactions that are not
	// in the .db file yet. Copying the .db alone would silently lose every
	// one of them, which is exactly the failure that makes "just move the
	// file" bad advice. Refuse and let a normal command checkpoint it.
	for _, sidecar := range []string{src + "-wal", src + "-shm"} {
		if _, err := os.Stat(sidecar); err == nil {
			return errWithExit(1,
				"refusing to copy: %s exists, so the database has committed data not yet folded into the file\nrun any read command against the old location first to check it out cleanly, then retry",
				sidecar)
		}
	}

	// Somebody mid-write holds the gate. Copying underneath them would
	// capture a torn database.
	release, err := lockfile.AcquireWithin(src+".lock", relocateGateWait)
	if err != nil {
		if lockfile.IsTimeout(err) {
			return errWithExit(1,
				"refusing to copy: another process is writing to %s\nstop it (a running `worklog serve`, or another shell) and retry",
				src)
		}
		return err
	}
	defer release()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := copyFileTo(src, dst); err != nil {
		return fmt.Errorf("copying the database: %w", err)
	}

	// Prove the copy is a database, not just bytes of the right length.
	tickets, err := verifyRelocated(dst)
	if err != nil {
		os.Remove(dst)
		return errWithExit(1, "the copy at %s did not open as a store, so it was removed: %v", dst, err)
	}

	fmt.Fprintf(cmd.OutOrStdout(),
		"relocated the store\n  from: %s\n    to: %s\n  %d tickets verified\nthe original is untouched; delete it when you are satisfied\n",
		src, dst, tickets)
	return nil
}

// verifyRelocated opens the copy and reads through it, so a truncated or
// corrupt file fails here rather than on the user's next write.
func verifyRelocated(path string) (int, error) {
	s, err := sqlitestore.Open(path)
	if err != nil {
		return 0, err
	}
	defer s.Close()
	tickets, err := s.Tickets()
	if err != nil {
		return 0, err
	}
	return len(tickets), nil
}

func copyFileTo(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	// Close before returning: the verify step reopens this path, and on a
	// buffered filesystem an unflushed write would read short.
	return out.Close()
}
