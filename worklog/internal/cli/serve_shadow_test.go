package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/migrate"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/sqlitestore"
)

// TestShadowNeverCreatesDB is adb-store-serve-shadow criterion 7: on a
// machine with no adopted store, the shadow loader is a silent no-op and
// must not create the database — sqlitestore.Open creates and migrates,
// and an empty store minted by a read makes every CLI write refuse from
// then on. The stat-before-open in loadStoreSnapshot is the whole guard.
func TestShadowNeverCreatesDB(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WORKLOG_MIGRATION_DATA", dir)

	snap, err := loadStoreSnapshot()
	if err != nil {
		t.Fatalf("store-less machine: want silent no-op, got error %v", err)
	}
	if snap != nil {
		t.Fatal("store-less machine: want nil snapshot, got one")
	}
	if _, err := os.Stat(migrate.OutputPath(dir)); !os.IsNotExist(err) {
		t.Fatalf("shadow read created the database at %s", migrate.OutputPath(dir))
	}
}

// TestShadowLoaderReadsAdoptedStore is the loader's positive half: with a
// real database present it returns the store's rendering, and closes the
// handle before returning (no WAL sidecar left for migrate to refuse on).
func TestShadowLoaderReadsAdoptedStore(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WORKLOG_MIGRATION_DATA", dir)
	path := migrate.OutputPath(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	sq, err := sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	tk := &store.Ticket{Slug: "shadow-loader", Title: "T", Type: store.TypeTicket,
		State: store.StateActive, Section: store.SectionNow, Repo: "r1", BoardTracked: true}
	if err := sq.PutTicket(tk); err != nil {
		t.Fatal(err)
	}
	if err := sq.Close(); err != nil {
		t.Fatal(err)
	}

	snap, err := loadStoreSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snap == nil {
		t.Fatal("adopted store: want a snapshot, got nil")
	}
	if _, ok := snap.Files["devboard/r1/shadow-loader.yaml"]; !ok {
		t.Fatalf("snapshot missing the board file; have %d files", len(snap.Files))
	}
	for _, sidecar := range []string{path + "-wal", path + "-shm"} {
		if _, err := os.Stat(sidecar); !os.IsNotExist(err) {
			t.Fatalf("%s outlives the load — the handle was not closed", sidecar)
		}
	}
}
