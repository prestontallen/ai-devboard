// Package storepath decides where the store's database lives.
//
// One rule: the store directory is a SIBLING of the worklog corpus
// directory, never a child of it. `~/.local/share/worklog` holds the
// markdown a human owns; `~/.local/share/worklog-store` holds the
// database, its sidecars and its lock file.
//
// Sideways rather than inside, because inside breaks adoption four
// different ways (adb-store-adapter, 2026-09-08). census refuses any file
// under the corpus root that no rule classifies, and a database plus its
// -wal and -shm are exactly that; adopt writes its pre-adoption snapshot
// under a directory it requires to be OUTSIDE both live roots; adopt's
// restore deletes every file under the root its manifest does not name,
// which would either delete the database or pair a restored one with a
// newer -wal, and that is corruption rather than data loss; and adopt's
// snapshot byte-copies the whole tree while the store is held open, which
// does not produce a valid database. Keeping the store outside the corpus
// leaves every one of those invariants true without changing them.
//
// The README also tells the human to sync the corpus directory to iCloud,
// Dropbox, Syncthing or git. A live SQLite database with a write-ahead log
// and an flock in a synced folder is a corruption risk, not a merge
// annoyance, and flock is unreliable on network and FUSE filesystems.
//
// Deriving the location instead of configuring it separately is what makes
// test isolation automatic: pointing --dir (or $WORKLOG_DIR) at a scratch
// directory moves the database with it, so a test cannot reach the real
// store by forgetting a second variable.
package storepath

import (
	"os"
	"path/filepath"
)

// DBName is the database's filename inside the store directory.
const DBName = "worklog.db"

// dirSuffix turns a corpus directory into its store sibling.
const dirSuffix = "-store"

// Dir is the store directory for a worklog corpus root.
func Dir(worklogRoot string) string {
	return filepath.Clean(worklogRoot) + dirSuffix
}

// DB is the database file for a worklog corpus root.
func DB(worklogRoot string) string {
	return filepath.Join(Dir(worklogRoot), DBName)
}

// DBIn is the database inside an explicitly named directory, for the one
// caller that still points somewhere of its own choosing (migrate's --out)
// and for tests that build a directory by hand.
func DBIn(dir string) string {
	return filepath.Join(dir, DBName)
}

// LegacyEnv is the retired environment variable that used to name the
// store's directory independently of the corpus. It is refused rather than
// honored: silently ignoring a variable the human deliberately set would
// send writes somewhere they did not intend.
const LegacyEnv = "WORKLOG_MIGRATION_DATA"

// LegacyDir is where the database lived before it had a real home, named
// for a migration that finished on 2026-09-03. Kept only so the refusal
// can look there and say something useful.
func LegacyDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "worklog-migration"), nil
}

// LegacyDB is the pre-move database path.
func LegacyDB() (string, error) {
	dir, err := LegacyDir()
	if err != nil {
		return "", err
	}
	return DBIn(dir), nil
}
