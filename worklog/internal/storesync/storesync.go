// Package storesync is adb-cutover M2's shadow-sync-and-verify hook.
//
// Real writes stay entirely on the legacy markdown/YAML path, unchanged —
// this package never writes to the live worklog or devboard directories.
// When WORKLOG_STORE_SYNC=1, AfterWrite re-derives the store from the
// disk state a write verb just produced, reusing the exact convert
// pipeline internal/migrate already proved against the live corpus, then
// parity-checks the result the same way internal/verify already does
// (render → diff against the same live files). Drift surfaces
// immediately after the write that caused it instead of accumulating
// silently.
//
// This is deliberately not a second independent writer: the legacy write
// is the single source of truth, and the store is derived from it, not
// maintained in parallel by hand-written logic that could drift from the
// first (contract decision #7, ~/.local/share/contracts/ai-devboard/
// 2026-09-03-adb-cutover.md).
package storesync

import (
	"fmt"
	"os"

	"github.com/prestontallen/ai-devboard/worklog/internal/devboard"
	"github.com/prestontallen/ai-devboard/worklog/internal/migrate"
	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/sqlitestore"
	"github.com/prestontallen/ai-devboard/worklog/internal/verify"
)

// EnvFlag gates the whole hook. Unset (or any value other than "1")
// keeps AfterWrite a zero-cost no-op — the legacy path's behavior and
// performance are unaffected until this is explicitly opted into.
const EnvFlag = "WORKLOG_STORE_SYNC"

// Enabled reports whether the shadow-sync hook should run.
func Enabled() bool { return os.Getenv(EnvFlag) == "1" }

// AfterWrite re-derives the store from wd's and devboard's current
// on-disk state and parity-checks it. Call once at the end of a write
// verb's Run, after every legacy write has already landed. A no-op
// (nil, nil) when Enabled is false.
//
// Returns a non-nil error only for a hard failure (I/O, refused
// conversion, torn snapshot) — drift is reported via the returned
// Report, not an error, so callers can warn rather than fail the user's
// command over it (shadow-sync trouble must never regress the legacy
// path's reliability during M2).
func AfterWrite(wd model.Workdir) (*verify.Report, error) {
	if !Enabled() {
		return nil, nil
	}

	dataDir, err := dataDir()
	if err != nil {
		return nil, fmt.Errorf("storesync: %w", err)
	}
	src := migrate.Sources{WorklogDir: wd.Root, DevboardDir: devboard.DataDir()}

	if _, err := migrate.Run(migrate.Options{Sources: src, DataDir: dataDir}); err != nil {
		return nil, fmt.Errorf("storesync: derive: %w", err)
	}

	s, err := sqlitestore.Open(migrate.OutputPath(dataDir))
	if err != nil {
		return nil, fmt.Errorf("storesync: open: %w", err)
	}
	defer s.Close()

	rep, err := verify.Run(s, src)
	if err != nil {
		return nil, fmt.Errorf("storesync: verify: %w", err)
	}
	return rep, nil
}

// WarnAfterWrite calls AfterWrite and prints a non-fatal notice to
// stderr on drift or a hard error. Write verbs call this at the end of
// their Run — never treating shadow-sync trouble as a reason to fail the
// user's command.
func WarnAfterWrite(wd model.Workdir) {
	rep, err := AfterWrite(wd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "storesync: %v\n", err)
		return
	}
	if rep != nil && !rep.Clean() {
		fmt.Fprintf(os.Stderr,
			"storesync: drift found after write (%d entries) -- run `worklog verify` for detail\n",
			len(rep.Drifts))
	}
}

func dataDir() (string, error) {
	if env := os.Getenv("WORKLOG_MIGRATION_DATA"); env != "" {
		return env, nil
	}
	return migrate.DefaultDataDir()
}
