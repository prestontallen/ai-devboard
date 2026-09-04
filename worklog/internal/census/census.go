// Package census enumerates every file under the live roots and accounts
// for each one.
//
// It exists because every other traversal in the tree is a FILTER, not an
// enumeration: convert.ReadCorpusDir and migrate.listCorpusFiles both
// os.ReadDir specific directories and skip anything whose suffix they do
// not recognise, and projection.EditedIn inspects only the paths the store
// renders. Each is correct for its own job and each is silently blind to a
// file nobody thought about — which is the wrong property for a step that
// must promise a corpus was migrated COMPLETELY.
//
// The census is therefore total by construction: filepath.WalkDir over
// both roots, every regular file assigned a class, and an Unclassified
// bucket that is a refusal rather than a `continue`.
package census

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// Class is what a file is, from the store's point of view.
type Class string

const (
	// Canon is read by the converter and rendered back from the store.
	Canon Class = "canon"
	// Derived is generated from canon files rather than from the store:
	// INDEX.md, which projection deliberately does not render (it is
	// rebuilt by reindex over the rendered output instead).
	Derived Class = "derived"
	// Transient is scratch the tool itself creates and may remove.
	Transient Class = "transient"
	// Unclassified is the whole point. Nothing may be silently ignored,
	// so anything the rules do not recognise lands here and blocks.
	Unclassified Class = "unclassified"
)

// Entry is one accounted-for file, path relative to the root it was found
// under.
type Entry struct {
	Path  string
	Class Class
}

// Report is the full accounting for one census run.
type Report struct {
	Worklog  []Entry
	Devboard []Entry
}

// Unclassified returns every path no rule recognised, worklog paths first.
// A non-empty result is a refusal: the caller cannot claim completeness
// over a corpus containing files it cannot describe.
func (r Report) Unclassified() []string {
	var out []string
	for _, e := range r.Worklog {
		if e.Class == Unclassified {
			out = append(out, e.Path)
		}
	}
	for _, e := range r.Devboard {
		if e.Class == Unclassified {
			out = append(out, filepath.Join("devboard", e.Path))
		}
	}
	return out
}

// Walk enumerates both roots. devboardDir may sit inside worklogDir (the
// test fixtures lay it out that way); it is walked once, under its own
// rules, either way. A root that does not exist contributes nothing —
// devboard is opt-in by directory presence, and a corpus with no archive
// or notes is legitimate.
func Walk(worklogDir, devboardDir string) (Report, error) {
	var r Report
	var err error
	if r.Worklog, err = walkRoot(worklogDir, devboardDir, classifyWorklog); err != nil {
		return r, err
	}
	if r.Devboard, err = walkRoot(devboardDir, "", classifyDevboard); err != nil {
		return r, err
	}
	return r, nil
}

func walkRoot(root, skip string, classify func(rel string) Class) ([]Entry, error) {
	if root == "" {
		return nil, nil
	}
	var out []Entry
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// A directory we cannot read is exactly the silent-loss shape
			// this package exists to stop: report it, never skip it.
			return err
		}
		if d.IsDir() {
			if skip != "" && sameDir(path, skip) {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			// A symlink or device node is not something the converter can
			// read as a corpus file, and quietly treating it as absent is
			// how a notes file goes missing.
			rel, _ := filepath.Rel(root, path)
			out = append(out, Entry{Path: filepath.ToSlash(rel), Class: Unclassified})
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		out = append(out, Entry{Path: filepath.ToSlash(rel), Class: classify(filepath.ToSlash(rel))})
		return nil
	})
	return out, err
}

func sameDir(a, b string) bool {
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	return err1 == nil && err2 == nil && aa == bb
}

// classifyWorklog mirrors convert.ReadCorpusDir's reading rules exactly.
// Where it is stricter than that reader, it is deliberately so: the reader
// skipping a file is what this catches.
func classifyWorklog(rel string) Class {
	switch rel {
	case "WORK.md", "FEEDBACK.md":
		return Canon
	case "INDEX.md":
		return Derived
	}
	if isTransient(baseOf(rel)) {
		return Transient
	}
	dir, base := splitOnce(rel)
	switch dir {
	case "archive", "notes":
		// ReadCorpusDir takes only *.md here, case-sensitively, and a
		// ".MD" or a stray ".bak" is silently absent from the conversion.
		if strings.HasSuffix(base, ".md") && !strings.Contains(base, "/") {
			return Canon
		}
	}
	return Unclassified
}

// classifyDevboard mirrors ReadCorpusDir's devboard rules: <repo>/<slug>.yaml
// and <repo>/_archive/<slug>.yaml, nothing deeper.
func classifyDevboard(rel string) Class {
	base := baseOf(rel)
	if isTransient(base) {
		return Transient
	}
	parts := strings.Split(rel, "/")
	yaml := strings.HasSuffix(base, ".yaml") || strings.HasSuffix(base, ".yml")
	switch len(parts) {
	case 2:
		if yaml && !strings.HasPrefix(parts[0], ".") {
			return Canon
		}
	case 3:
		if yaml && parts[1] == "_archive" && !strings.HasPrefix(parts[0], ".") {
			return Canon
		}
	}
	return Unclassified
}

func isTransient(base string) bool {
	return base == ".freeze" ||
		strings.HasPrefix(base, ".proj-") ||
		strings.Contains(base, ".bak.") ||
		// serve takes a per-file lock beside each devboard YAML
		// (internal/serve/server.go, lockfile.Acquire) and the live corpus
		// carries one per task file while it runs.
		strings.HasSuffix(base, ".lock")
}

func splitOnce(rel string) (dir, rest string) {
	if i := strings.Index(rel, "/"); i >= 0 {
		return rel[:i], rel[i+1:]
	}
	return "", rel
}

func baseOf(rel string) string {
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		return rel[i+1:]
	}
	return rel
}
