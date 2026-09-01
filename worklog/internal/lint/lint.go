// Package lint detects drift between the rule blocks across the worklog's
// spec files. It replaces scripts/lint-specs.sh.
//
// A rule block is the content between `<!-- rules:start -->` and
// `<!-- rules:end -->` markers, with leading and trailing blank lines
// stripped. Both markers must appear on their own lines.
package lint

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/aymanbagabas/go-udiff"
)

const (
	markerStart = "<!-- rules:start -->"
	markerEnd   = "<!-- rules:end -->"
)

// ErrMissingMarker is returned when a spec file lacks one of the boundary
// markers. The error message names the offending file and marker.
var ErrMissingMarker = errors.New("missing rule-block marker")

// DefaultSpecs returns the canonical paths of files lint scans, given the
// repo root.
func DefaultSpecs(repoRoot string) []string {
	return []string{
		filepath.Join(repoRoot, "README.md"),
		filepath.Join(repoRoot, "skill", "SKILL.md"),
		filepath.Join(repoRoot, "skill", "claude", "command.md"),
	}
}

// ExtractRules returns the content of the rule block in path, with leading
// and trailing blank lines stripped. Returns ErrMissingMarker if either
// boundary marker is missing.
func ExtractRules(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var (
		captured  []string
		seenStart bool
		seenEnd   bool
		capturing bool
	)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		switch line {
		case markerStart:
			seenStart = true
			capturing = true
			continue
		case markerEnd:
			seenEnd = true
			capturing = false
			continue
		}
		if capturing {
			captured = append(captured, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if !seenStart {
		return "", fmt.Errorf("%w: %s missing %q", ErrMissingMarker, path, markerStart)
	}
	if !seenEnd {
		return "", fmt.Errorf("%w: %s missing %q", ErrMissingMarker, path, markerEnd)
	}
	return strings.Join(stripBlankEdges(captured), "\n"), nil
}

func stripBlankEdges(lines []string) []string {
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	end := len(lines)
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return lines[start:end]
}

// Diff returns a unified diff between the rule blocks of two files. Empty
// string means identical. Both files must have the rule markers.
func Diff(aPath, bPath string) (string, error) {
	a, err := ExtractRules(aPath)
	if err != nil {
		return "", err
	}
	b, err := ExtractRules(bPath)
	if err != nil {
		return "", err
	}
	if a == b {
		return "", nil
	}
	return udiff.Unified(aPath, bPath, a+"\n", b+"\n"), nil
}

// RunCheck pairwise-diffs every file in paths, writing drift report lines to
// w. Returns (drift, err).
func RunCheck(paths []string, w io.Writer) (bool, error) {
	// Validate all files exist + have markers up front.
	blocks := make(map[string]string, len(paths))
	for _, p := range paths {
		b, err := ExtractRules(p)
		if err != nil {
			return false, err
		}
		blocks[p] = b
	}

	drift := false
	for i := 0; i < len(paths); i++ {
		for j := i + 1; j < len(paths); j++ {
			a, b := paths[i], paths[j]
			if blocks[a] == blocks[b] {
				continue
			}
			fmt.Fprintf(w, "=== DRIFT: %s  vs  %s ===\n", a, b)
			fmt.Fprint(w, udiff.Unified(a, b, blocks[a]+"\n", blocks[b]+"\n"))
			drift = true
		}
	}
	return drift, nil
}

// RunPrint emits each file's rule block under a "=== <path> ===" header.
func RunPrint(paths []string, w io.Writer) error {
	for _, p := range paths {
		b, err := ExtractRules(p)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "=== %s ===\n", p)
		fmt.Fprintln(w, b)
		fmt.Fprintln(w)
	}
	return nil
}
