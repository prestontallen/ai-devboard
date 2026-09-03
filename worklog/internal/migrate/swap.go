package migrate

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// swap checkpoints and closes-out workingPath, then atomically retires
// whatever currently sits at outputPath to backupPath — a fresh,
// timestamped name each run, so nothing here rotates or deletes an
// earlier backup — and moves workingPath into outputPath's place. Only
// the single .db file moves at each step — WAL sidecars are cleared,
// never carried over or silently adopted by the incoming file
// (criterion 6). backupPath is ignored when outputPath does not exist
// (the baseline run).
func swap(outputPath, backupPath, workingPath string) error {
	if err := checkpoint(workingPath); err != nil {
		return fmt.Errorf("migrate: checkpointing working copy before swap: %w", err)
	}
	if err := removeSidecars(workingPath); err != nil {
		return fmt.Errorf("migrate: clearing working copy WAL sidecars: %w", err)
	}

	if fileExists(outputPath) {
		os.Remove(backupPath) // defensive only: a fresh timestamp should never already exist
		if err := removeSidecars(backupPath); err != nil {
			return fmt.Errorf("migrate: clearing prior backup WAL sidecars: %w", err)
		}
		if err := os.Rename(outputPath, backupPath); err != nil {
			return fmt.Errorf("migrate: backing up output db: %w", err)
		}
	}
	// Defensive: OUTPUT_PATH is never opened for writing by this tool, so
	// it should never have accrued a WAL sidecar of its own — but don't
	// let a stray one from outside this tool get orphaned next to the
	// backup we just created, or silently adopted by the incoming file.
	if err := removeSidecars(outputPath); err != nil {
		return fmt.Errorf("migrate: clearing orphaned output WAL sidecars: %w", err)
	}

	if err := os.Rename(workingPath, outputPath); err != nil {
		return fmt.Errorf("migrate: swapping in the converted db: %w", err)
	}
	return nil
}

func sidecarPaths(dbPath string) (wal, shm string) {
	return dbPath + "-wal", dbPath + "-shm"
}

func removeSidecars(dbPath string) error {
	wal, shm := sidecarPaths(dbPath)
	for _, p := range []string{wal, shm} {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}
