// Package adopt turns a corpus that predates the store into one the
// store-backed write path accepts, without ever losing a byte.
//
// The ordering here is deliberate and is the package's main safety
// property: the snapshot and its restore path are built and tested BEFORE
// anything that writes, so there is never a version of this code in which a
// writer exists without a proven way back.
package adopt

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ManifestName is the digest index written at a snapshot's root.
const ManifestName = "manifest.json"

// Manifest records every file a snapshot captured, with its sha256. It is
// what makes the restore checkable rather than merely hopeful: a restore
// that silently half-worked is indistinguishable from a good one without
// digests.
type Manifest struct {
	// Worklog and Devboard map slash-relative paths to lowercase hex
	// sha256 of the file's bytes at snapshot time.
	Worklog  map[string]string `json:"worklog"`
	Devboard map[string]string `json:"devboard"`
}

// Roots is the pair of live directories a snapshot covers. Devboard may be
// empty: it is opt-in by directory presence.
type Roots struct {
	Worklog  string
	Devboard string
}

func (m *Manifest) files() int { return len(m.Worklog) + len(m.Devboard) }

// Snapshot copies both roots into dest verbatim and writes the manifest.
//
// It enumerates with WalkDir rather than the suffix filters the converter
// uses, because a snapshot that captures only the files the converter
// understands cannot restore the ones it does not — which are exactly the
// files most likely to be lost.
func Snapshot(r Roots, dest string) (*Manifest, error) {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return nil, err
	}
	m := &Manifest{Worklog: map[string]string{}, Devboard: map[string]string{}}

	if err := copyTree(r.Worklog, filepath.Join(dest, "worklog"), r.Devboard, m.Worklog); err != nil {
		return nil, err
	}
	if r.Devboard != "" {
		if err := copyTree(r.Devboard, filepath.Join(dest, "devboard"), "", m.Devboard); err != nil {
			return nil, err
		}
	}

	data, err := json.MarshalIndent(m, "", " ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dest, ManifestName), data, 0o644); err != nil {
		return nil, err
	}
	// Re-read what landed rather than trusting what we wrote.
	if err := Verify(dest); err != nil {
		return nil, fmt.Errorf("snapshot did not verify immediately after writing: %w", err)
	}
	return m, nil
}

// LoadManifest reads a snapshot's manifest.
func LoadManifest(dest string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(dest, ManifestName))
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", ManifestName, err)
	}
	return &m, nil
}

// Verify re-hashes a snapshot against its own manifest, reporting the first
// file that disagrees. A snapshot nobody checked is not a backup.
func Verify(dest string) error {
	m, err := LoadManifest(dest)
	if err != nil {
		return err
	}
	for _, side := range []struct {
		dir   string
		files map[string]string
	}{
		{filepath.Join(dest, "worklog"), m.Worklog},
		{filepath.Join(dest, "devboard"), m.Devboard},
	} {
		for _, rel := range sortedKeys(side.files) {
			sum, err := hashFile(filepath.Join(side.dir, filepath.FromSlash(rel)))
			if err != nil {
				return fmt.Errorf("snapshot %s: %w", rel, err)
			}
			if sum != side.files[rel] {
				return fmt.Errorf("snapshot %s: sha256 %s, manifest says %s", rel, sum, side.files[rel])
			}
		}
	}
	return nil
}

// Restore puts both live roots back exactly as the snapshot found them:
// every captured file rewritten from the snapshot, and every file that is
// NOT in the manifest removed. Without the removal a restore leaves behind
// whatever the failed run created, which is a different tree than the one
// that was captured — and "byte-exact" would be a lie.
//
// It verifies the snapshot before touching anything, so a corrupt backup
// cannot overwrite a live tree, and re-hashes the live files afterwards.
func Restore(dest string, r Roots) error {
	if err := Verify(dest); err != nil {
		return fmt.Errorf("refusing to restore from an unverifiable snapshot: %w", err)
	}
	m, err := LoadManifest(dest)
	if err != nil {
		return err
	}

	for _, side := range []struct {
		from, to string
		files    map[string]string
	}{
		{filepath.Join(dest, "worklog"), r.Worklog, m.Worklog},
		{filepath.Join(dest, "devboard"), r.Devboard, m.Devboard},
	} {
		if side.to == "" {
			continue
		}
		for _, rel := range sortedKeys(side.files) {
			src := filepath.Join(side.from, filepath.FromSlash(rel))
			dst := filepath.Join(side.to, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			if err := copyFile(src, dst); err != nil {
				return err
			}
		}
		if err := removeUnlisted(side.to, side.files); err != nil {
			return err
		}
	}

	// The restore is only done when the live trees hash to the manifest.
	for _, side := range []struct {
		dir   string
		files map[string]string
	}{
		{r.Worklog, m.Worklog},
		{r.Devboard, m.Devboard},
	} {
		if side.dir == "" {
			continue
		}
		for _, rel := range sortedKeys(side.files) {
			sum, err := hashFile(filepath.Join(side.dir, filepath.FromSlash(rel)))
			if err != nil {
				return fmt.Errorf("restored %s: %w", rel, err)
			}
			if sum != side.files[rel] {
				return fmt.Errorf("restored %s: sha256 %s, manifest says %s", rel, sum, side.files[rel])
			}
		}
	}
	return nil
}

// removeUnlisted deletes every file under root that the manifest does not
// name, then prunes the directories left empty.
func removeUnlisted(root string, keep map[string]string) error {
	var doomed []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return relErr
		}
		if _, ok := keep[filepath.ToSlash(rel)]; !ok {
			doomed = append(doomed, p)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, p := range doomed {
		if err := os.Remove(p); err != nil {
			return err
		}
	}
	return pruneEmptyDirs(root)
}

func pruneEmptyDirs(root string) error {
	var dirs []string
	if err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && p != root {
			dirs = append(dirs, p)
		}
		return nil
	}); err != nil {
		return err
	}
	// Deepest first, so a parent emptied by its children is pruned too.
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, d := range dirs {
		entries, err := os.ReadDir(d)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			if err := os.Remove(d); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyTree(src, dst, skip string, into map[string]string) error {
	if src == "" {
		return nil
	}
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skip != "" && sameDir(p, skip) {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s: not a regular file; a snapshot cannot promise to restore it", p)
		}
		rel, relErr := filepath.Rel(src, p)
		if relErr != nil {
			return relErr
		}
		out := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		if err := copyFile(p, out); err != nil {
			return err
		}
		sum, err := hashFile(out)
		if err != nil {
			return err
		}
		into[filepath.ToSlash(rel)] = sum
		return nil
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	fi, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fi.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func hashFile(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func sameDir(a, b string) bool {
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	return err1 == nil && err2 == nil && aa == bb
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Describe is a one-line summary for the CLI.
func (m *Manifest) Describe() string {
	return fmt.Sprintf("%d files (%d worklog, %d devboard)", m.files(), len(m.Worklog), len(m.Devboard))
}

// StampName builds a snapshot directory name from a caller-supplied
// timestamp. The caller owns the clock so this stays testable.
func StampName(stamp string) string {
	return "adopt-" + strings.ReplaceAll(stamp, ":", "")
}
