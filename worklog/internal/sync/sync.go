// Package sync deploys the worklog's skill files from this repo to the
// locations both agents (Cursor, Claude Code) expect to find them. It
// replaces scripts/sync.sh.
package sync

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Pair is a single source → destination deployment.
type Pair struct {
	Src string
	Dst string
}

// Mode selects how Run treats each pair.
type Mode int

const (
	ModeDefault Mode = iota // copy and verify
	ModeCheck                // diff only, no writes
	ModeDryRun               // print intent, no writes
)

// Status enumerates per-pair outcomes for reporting.
type Status string

const (
	StatusSynced    Status = "synced"
	StatusUnchanged Status = "unchanged"
	StatusMatch     Status = "match"
	StatusDiffer    Status = "differ"
	StatusWouldCopy Status = "would-copy"
)

// Errors returned by package-level functions.
var (
	ErrSrcMissing    = errors.New("source file missing")
	ErrTargetIsDir   = errors.New("target exists as a directory")
	ErrPostCopyDiff  = errors.New("post-copy diff failed")
)

// DefaultPairs returns the canonical worklog skill deployment pairs for a
// given repo root and user home directory.
//
// New deployment targets (Cursor .mdc, CLAUDE.md snippet) belong here once
// their source files exist in the repo. See the TODOs below.
func DefaultPairs(repoRoot, home string) []Pair {
	pairs := []Pair{
		{
			Src: filepath.Join(repoRoot, "skill", "SKILL.md"),
			Dst: filepath.Join(home, ".cursor", "skills", "worklog", "SKILL.md"),
		},
		{
			Src: filepath.Join(repoRoot, "skill", "SKILL.md"),
			Dst: filepath.Join(home, ".claude", "skills", "worklog", "SKILL.md"),
		},
		{
			Src: filepath.Join(repoRoot, "skill", "claude", "command.md"),
			Dst: filepath.Join(home, ".claude", "commands", "worklog.md"),
		},
		// TODO(phase-2.x): Cursor .mdc deployment once skill/cursor/rule.mdc exists:
		//   {Src: ".../skill/cursor/rule.mdc", Dst: "~/.cursor/rules/worklog.mdc"}
		// TODO(phase-2.x): CLAUDE.md snippet merge once skill/claude/claudemd-snippet.md exists.
	}
	return pairs
}

// FindRepoRoot walks up from start until it finds a directory containing
// go.mod, returning the absolute path of that directory. Returns an error if
// the filesystem root is reached without finding one.
func FindRepoRoot(start string) (string, error) {
	cur, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(cur, "go.mod")); err == nil {
			return cur, nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", fmt.Errorf("no go.mod found walking up from %s", start)
		}
		cur = parent
	}
}

// Result describes the outcome of processing a single Pair.
type Result struct {
	Pair
	Status Status
}

// VerifySources errors out if any pair's source file is missing or is a
// directory. Callers should invoke this before any per-pair work to avoid
// partial deploys.
func VerifySources(pairs []Pair) error {
	for _, p := range pairs {
		info, err := os.Stat(p.Src)
		if err != nil {
			return fmt.Errorf("%w: %s", ErrSrcMissing, p.Src)
		}
		if info.IsDir() {
			return fmt.Errorf("source is a directory: %s", p.Src)
		}
	}
	return nil
}

// ProcessPair applies mode to a single pair and returns a structured Result
// the caller can format however they want. For ModeDryRun the result's
// Status is StatusWouldCopy and no filesystem changes occur.
func ProcessPair(mode Mode, p Pair) (Result, error) {
	switch mode {
	case ModeDryRun:
		return Result{Pair: p, Status: StatusWouldCopy}, nil
	case ModeCheck:
		return checkPair(p)
	case ModeDefault:
		return syncPair(p)
	default:
		return Result{}, fmt.Errorf("unknown mode %d", mode)
	}
}

// Run applies mode to pairs, writing default-format per-pair report lines
// to w. Used by tests and as a convenience for callers that don't want to
// format their own output. The first returned value reflects whether any
// pair differed in check mode.
func Run(mode Mode, pairs []Pair, w io.Writer) (mismatch bool, err error) {
	if err := VerifySources(pairs); err != nil {
		return false, err
	}

	for _, p := range pairs {
		if mode == ModeDryRun {
			fmt.Fprintf(w, "would: mkdir -p %s\n", filepath.Dir(p.Dst))
			fmt.Fprintf(w, "would: cp %s -> %s\n", p.Src, p.Dst)
			continue
		}
		r, err := ProcessPair(mode, p)
		if err != nil {
			return false, err
		}
		fmt.Fprintf(w, "%s: %s -> %s\n", r.Status, r.Src, r.Dst)
		if r.Status == StatusDiffer {
			mismatch = true
		}
	}
	return mismatch, nil
}

func checkPair(p Pair) (Result, error) {
	info, err := os.Stat(p.Dst)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Result{Pair: p, Status: StatusDiffer}, nil
		}
		return Result{}, err
	}
	if info.IsDir() {
		return Result{}, fmt.Errorf("%w: %s", ErrTargetIsDir, p.Dst)
	}
	same, err := filesEqual(p.Src, p.Dst)
	if err != nil {
		return Result{}, err
	}
	if same {
		return Result{Pair: p, Status: StatusMatch}, nil
	}
	return Result{Pair: p, Status: StatusDiffer}, nil
}

func syncPair(p Pair) (Result, error) {
	targetDir := filepath.Dir(p.Dst)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("mkdir %s: %w", targetDir, err)
	}

	if info, err := os.Stat(p.Dst); err == nil && info.IsDir() {
		return Result{}, fmt.Errorf("%w: %s", ErrTargetIsDir, p.Dst)
	}

	// Idempotency: skip copy when already in sync.
	if _, err := os.Stat(p.Dst); err == nil {
		same, err := filesEqual(p.Src, p.Dst)
		if err == nil && same {
			return Result{Pair: p, Status: StatusUnchanged}, nil
		}
	}

	data, err := os.ReadFile(p.Src)
	if err != nil {
		return Result{}, fmt.Errorf("read %s: %w", p.Src, err)
	}
	if err := writeAtomic(p.Dst, data); err != nil {
		return Result{}, err
	}

	same, err := filesEqual(p.Src, p.Dst)
	if err != nil {
		return Result{}, err
	}
	if !same {
		return Result{}, fmt.Errorf("%w: %s -> %s", ErrPostCopyDiff, p.Src, p.Dst)
	}
	return Result{Pair: p, Status: StatusSynced}, nil
}

func filesEqual(a, b string) (bool, error) {
	da, err := os.ReadFile(a)
	if err != nil {
		return false, err
	}
	db, err := os.ReadFile(b)
	if err != nil {
		return false, err
	}
	return bytes.Equal(da, db), nil
}

// writeAtomic writes data to path via tempfile + rename so a partial write
// cannot leave the target file corrupted. The data is written byte-exactly
// (no trailing newline insertion, unlike render.WriteAtomic).
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	f, err := os.CreateTemp(dir, "."+base+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp in %s: %w", dir, err)
	}
	tmpName := f.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		cleanup()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("rename to %s: %w", path, err)
	}
	return nil
}
