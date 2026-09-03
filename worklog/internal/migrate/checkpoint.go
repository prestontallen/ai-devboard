package migrate

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// checkpoint runs PRAGMA wal_checkpoint(TRUNCATE) against path through its
// own short-lived connection, so the WAL is fully flushed into the main
// database file and truncated to zero before path is copied or renamed —
// after this call only path itself (never path-wal / path-shm) carries
// meaningful content (criterion 6).
//
// Callers must ensure no other connection holds path open first —
// sqlitestore keeps a single persistent connection per store
// (SetMaxOpenConns(1)), so the working copy's Store must be Close'd
// before this runs.
//
// This is a raw database/sql handle, not sqlitestore.SQLite, because the
// Store interface deliberately has no Exec/Checkpoint method — adding one
// would widen internal/store for a concern that belongs to this
// composition root (Decision, contracts/ai-devboard/2026-09-03-adb-worklog2-migrate.md).
// It is only ever called against the SCRATCH working copy, never
// OUTPUT_PATH — OUTPUT_PATH is never opened for writing (criterion 4).
func checkpoint(path string) error {
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	var busy, log, checkpointed int
	if err := db.QueryRow("PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &log, &checkpointed); err != nil {
		return fmt.Errorf("wal_checkpoint(%s): %w", path, err)
	}
	if busy != 0 {
		return fmt.Errorf("wal_checkpoint(TRUNCATE) on %s reported busy — another connection is holding the WAL", path)
	}
	return nil
}
