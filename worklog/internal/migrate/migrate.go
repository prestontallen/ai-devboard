package migrate

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/prestontallen/ai-devboard/worklog/internal/convert"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/sqlitestore"
)

// Options configures one migrate run. DataDir is the single knob for
// everything migrate owns: the persisted output db, its timestamped
// backups, the scratch working copy, and the staging corpus copy all live
// under it (contract Decision #8).
type Options struct {
	Sources Sources
	DataDir string
}

// backupTimestampLayout carries nanosecond precision: two Run() calls
// close enough together to land in the same wall-clock second (routine in
// tests, and not impossible from a script looping the CLI) must still get
// distinct backup filenames rather than one silently overwriting the
// other — the exact failure "genuinely timestamped, not a single
// overwritten .bak" (contract criterion 5) exists to rule out.
const backupTimestampLayout = "20060102T150405.000000000Z"

func (o Options) outputPath() string { return OutputPath(o.DataDir) }

// backupPath names this run's backup file, stamped with now. Every run
// that finds a prior generation gets its own filename — nothing rotates
// or deletes an earlier backup (no backup rotation beyond this ticket's
// single timestamped-per-run scheme, by decision).
func (o Options) backupPath(now time.Time) string {
	return o.outputPath() + ".bak." + now.UTC().Format(backupTimestampLayout)
}
func (o Options) workingPath() string { return filepath.Join(o.DataDir, "working.db") }
func (o Options) stagingDir() string  { return filepath.Join(o.DataDir, "staging") }

// OutputPath is the persisted db's path under a migrate data directory —
// exported so callers (the CLI's output) can report it without
// duplicating the filename convention.
func OutputPath(dataDir string) string { return filepath.Join(dataDir, "worklog.db") }

// DefaultDataDir is OUTPUT_PATH's parent when neither --out nor
// $WORKLOG_MIGRATION_DATA is set.
func DefaultDataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "worklog-migration"), nil
}

// Result is what one migrate run produced.
type Result struct {
	Report     *convert.Report
	Diff       IDSetDiff
	StaleRows  []string
	BackedUp   bool   // whether a prior generation was preserved this run
	BackupPath string // where it was preserved; "" when BackedUp is false
}

// Run stages a read-only copy of the live worklog + devboard dirs,
// converts it via copy-forward into the working copy seeded from any
// existing OUTPUT_PATH, and — only on success — atomically swaps the
// working copy into OUTPUT_PATH's place. OUTPUT_PATH is opened for
// reading only (the copy-forward seed) and is never opened for writing;
// on any failure it is left byte-for-byte unchanged (criterion 4).
func Run(o Options) (*Result, error) {
	if err := os.MkdirAll(o.DataDir, 0o755); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	if err := Stage(o.Sources, o.stagingDir()); err != nil {
		return nil, err // *ErrTornSnapshot, or a hard I/O error — OUTPUT_PATH untouched
	}
	corpus, err := convert.ReadCorpusDir(o.stagingDir())
	if err != nil {
		return nil, fmt.Errorf("migrate: reading staged corpus: %w", err)
	}

	if err := seedWorkingCopy(o); err != nil {
		return nil, err
	}
	// The working copy is scratch space from here on; a refusal below
	// leaves it half-converted, which is fine to discard — OUTPUT_PATH was
	// never touched to produce it.
	defer os.Remove(o.workingPath())
	defer removeSidecars(o.workingPath())

	ws, err := sqlitestore.Open(o.workingPath())
	if err != nil {
		return nil, fmt.Errorf("migrate: opening working copy: %w", err)
	}

	beforeTickets, err := ws.Tickets()
	if err != nil {
		ws.Close()
		return nil, fmt.Errorf("migrate: reading working copy before conversion: %w", err)
	}
	before := snapshotFromTickets(beforeTickets)

	report, convErr := convert.Load(ws, corpus)
	if convErr != nil {
		ws.Close()
		return nil, convErr // refusal: report it, OUTPUT_PATH is untouched, working copy discarded above
	}

	afterTickets, err := ws.Tickets()
	if err != nil {
		ws.Close()
		return nil, fmt.Errorf("migrate: reading working copy after conversion: %w", err)
	}
	after := snapshotFromTickets(afterTickets)

	convertedSlugs := make(map[string]bool, len(report.Slugs))
	for _, s := range report.Slugs {
		convertedSlugs[s] = true
	}
	stale := StaleRows(afterTickets, convertedSlugs)

	if err := ws.Close(); err != nil {
		return nil, fmt.Errorf("migrate: closing working copy: %w", err)
	}

	backedUp := fileExists(o.outputPath())
	backupPath := ""
	if backedUp {
		backupPath = o.backupPath(time.Now())
	}
	if err := swap(o.outputPath(), backupPath, o.workingPath()); err != nil {
		return nil, err
	}

	return &Result{
		Report:     report,
		Diff:       DiffIDs(before, after),
		StaleRows:  stale,
		BackedUp:   backedUp,
		BackupPath: backupPath,
	}, nil
}

// seedWorkingCopy builds the copy-forward working copy: a byte copy of
// the existing OUTPUT_PATH (opened read-only — never written), or a fresh
// empty file on the very first run. D4 depends on this: convert.Load
// resolves ULIDs by slug against whatever is already in the target store,
// so seeding from the prior generation is what makes re-runs reuse IDs
// instead of minting new ones for everything.
func seedWorkingCopy(o Options) error {
	os.Remove(o.workingPath()) // stale leftover from an interrupted prior run
	removeSidecars(o.workingPath())

	out := o.outputPath()
	if !fileExists(out) {
		return nil // sqlitestore.Open creates+migrates a fresh db at workingPath
	}
	if wal, _ := sidecarPaths(out); nonEmpty(wal) {
		return fmt.Errorf("migrate: %s has an unexpected non-empty WAL sidecar (%s) — OUTPUT_PATH should only ever be written by this tool's own checkpointed swap; refusing to seed from a possibly incomplete db", out, wal)
	}
	if err := copyFile(out, o.workingPath()); err != nil {
		return fmt.Errorf("migrate: seeding working copy from %s: %w", out, err)
	}
	return nil
}

func nonEmpty(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Size() > 0
}
