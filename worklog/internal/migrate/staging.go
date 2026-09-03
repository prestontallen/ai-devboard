// Package migrate implements `worklog migrate`: a rehearsal tool that
// copies Preston's live worklog + devboard data, converts the copy into a
// persisted SQLite database via copy-forward, and reports whether entity
// identity held steady across the run. It never writes the live dirs —
// see Stage — and it is the only package outside internal/convert's own
// tests that opens a store.Store implementation directly, acting as the
// composition root the boundary test (internal/store/boundary_test.go)
// exempts by not listing it as a consumer.
package migrate

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Sources names the two live directories migrate reads. Neither is ever
// written — Stage only opens files for reading.
type Sources struct {
	WorklogDir  string // model.Workdir.Root layout: WORK.md, FEEDBACK.md, archive/, notes/
	DevboardDir string // devboard.DataDir() layout: <repo>/<slug>.yaml (+ <repo>/_archive/)
}

// ErrTornSnapshot reports that live data changed while Stage was copying
// it — a file disappeared, or a copied file's size/mtime differs from
// what the walk saw when it first read that file — and the retry also
// saw a change (criterion 7). It is a distinct type so callers can tell
// this apart from convert.Load's ordinary refusals.
type ErrTornSnapshot struct {
	Detail string
}

func (e *ErrTornSnapshot) Error() string {
	return "live data changed during copy — retry: " + e.Detail
}

type fileStat struct {
	size    int64
	modTime time.Time
}

type copyEntry struct {
	src string // absolute path in the live dir
	dst string // absolute path under the staging root
}

// testHookAfterCopy, when non-nil, runs after each file is copied during
// stageOnce — a test-only seam for deterministically simulating a
// concurrent modification landing mid-walk (adb-worklog2-migrate,
// criterion 7). Production code never sets it.
var testHookAfterCopy func(entry copyEntry)

// Stage copies the live worklog dir and devboard dir into dst, laid out
// exactly as convert.ReadCorpusDir expects to read it back
// (dst/WORK.md, dst/archive, dst/notes, dst/FEEDBACK.md,
// dst/devboard/<repo>/*.yaml). dst is cleared first; it must be a scratch
// directory migrate owns, never a live dir.
//
// If the live data changes during the copy, Stage retries once from a
// clean dst before giving up with *ErrTornSnapshot (criterion 7).
func Stage(src Sources, dst string) error {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if err := os.RemoveAll(dst); err != nil {
			return fmt.Errorf("migrate: clearing staging dir: %w", err)
		}
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return fmt.Errorf("migrate: creating staging dir: %w", err)
		}
		err := stageOnce(src, dst)
		if err == nil {
			return nil
		}
		var torn *ErrTornSnapshot
		if !errors.As(err, &torn) {
			return err
		}
		lastErr = err
	}
	return lastErr
}

func stageOnce(src Sources, dst string) error {
	files, err := listCorpusFiles(src, dst)
	if err != nil {
		return err
	}

	before := make(map[string]fileStat, len(files))
	for _, f := range files {
		fi, err := os.Lstat(f.src)
		if errors.Is(err, fs.ErrNotExist) {
			return &ErrTornSnapshot{Detail: fmt.Sprintf("%s vanished before it could be copied", f.src)}
		} else if err != nil {
			return err
		}
		before[f.src] = fileStat{size: fi.Size(), modTime: fi.ModTime()}
		if err := copyFile(f.src, f.dst); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return &ErrTornSnapshot{Detail: fmt.Sprintf("%s vanished while being copied", f.src)}
			}
			return err
		}
		if testHookAfterCopy != nil {
			testHookAfterCopy(f)
		}
	}

	// Re-stat every file we copied. A rewrite that lands anywhere in the
	// tree during the window we were copying other files shows up here
	// even though the individual copyFile call for that file succeeded —
	// this is what makes a concurrent `worklog done` (which deletes
	// nothing) detectable, not just a deletion.
	for _, f := range files {
		fi, err := os.Lstat(f.src)
		if errors.Is(err, fs.ErrNotExist) {
			return &ErrTornSnapshot{Detail: fmt.Sprintf("%s removed during the copy window", f.src)}
		} else if err != nil {
			return err
		}
		b := before[f.src]
		if fi.Size() != b.size || !fi.ModTime().Equal(b.modTime) {
			return &ErrTornSnapshot{Detail: fmt.Sprintf("%s changed during the copy window", f.src)}
		}
	}
	return nil
}

// listCorpusFiles enumerates exactly the files convert.ReadCorpusDir will
// read from a root laid out like dst — mirroring its traversal (same
// optional-directory tolerance, same extension filter) so staging copies
// neither more nor less than what conversion consumes, and so "torn"
// means exactly what that reader would see differently.
func listCorpusFiles(src Sources, dst string) ([]copyEntry, error) {
	var out []copyEntry

	workMD := filepath.Join(src.WorklogDir, "WORK.md")
	if _, err := os.Stat(workMD); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("migrate: %s: %w", workMD, err)
		}
		return nil, err
	}
	out = append(out, copyEntry{workMD, filepath.Join(dst, "WORK.md")})

	if fb := filepath.Join(src.WorklogDir, "FEEDBACK.md"); fileExists(fb) {
		out = append(out, copyEntry{fb, filepath.Join(dst, "FEEDBACK.md")})
	}

	out = append(out, listMDDir(filepath.Join(src.WorklogDir, "archive"), filepath.Join(dst, "archive"))...)
	out = append(out, listMDDir(filepath.Join(src.WorklogDir, "notes"), filepath.Join(dst, "notes"))...)

	repos, err := os.ReadDir(src.DevboardDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return out, nil // no devboard data at all is legal
		}
		return nil, err
	}
	for _, repo := range repos {
		if !repo.IsDir() || strings.HasPrefix(repo.Name(), ".") {
			continue
		}
		repoSrc := filepath.Join(src.DevboardDir, repo.Name())
		repoDst := filepath.Join(dst, "devboard", repo.Name())
		out = append(out, listYAMLDir(repoSrc, repoDst)...)
		out = append(out, listYAMLDir(filepath.Join(repoSrc, "_archive"), filepath.Join(repoDst, "_archive"))...)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].src < out[j].src })
	return out, nil
}

func listMDDir(srcDir, dstDir string) []copyEntry {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return nil // absent/unreadable archive or notes dir: no entries, matching ReadCorpusDir's tolerance
	}
	var out []copyEntry
	for _, e := range entries {
		if !e.Type().IsRegular() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		out = append(out, copyEntry{filepath.Join(srcDir, e.Name()), filepath.Join(dstDir, e.Name())})
	}
	return out
}

func listYAMLDir(srcDir, dstDir string) []copyEntry {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return nil // _archive is commonly absent
	}
	var out []copyEntry
	for _, e := range entries {
		lower := strings.ToLower(e.Name())
		if !e.Type().IsRegular() || (!strings.HasSuffix(lower, ".yaml") && !strings.HasSuffix(lower, ".yml")) {
			continue
		}
		out = append(out, copyEntry{filepath.Join(srcDir, e.Name()), filepath.Join(dstDir, e.Name())})
	}
	return out
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// copyFile copies src to dst, creating dst's parent directories. Errors
// from opening src propagate with their original sentinel (fs.ErrNotExist
// in particular) so callers can distinguish "vanished" from other I/O
// failures.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
