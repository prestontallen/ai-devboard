package installer

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

// The markers that delimit the block this installer owns inside a file the
// human owns. HTML comments because the file is markdown that an agent reads
// as instructions: the markers must be invisible to the reader and obvious to
// the writer.
//
// They replace a substring test for the phrase "dev-context" against the
// whole file, which was wrong twice over. It could never detect that the
// deployed block had gone STALE — and it hadn't, for weeks, on the
// maintainer's own machine — and it was a false positive against the human's
// own prose about the dev-context skill.
const (
	directiveBegin = "<!-- ai-devboard:begin"
	directiveEnd   = "<!-- ai-devboard:end -->"
)

// MarkedDirective wraps the repo's directive in the managed markers.
func MarkedDirective(src []byte) []byte {
	var b bytes.Buffer
	b.WriteString(directiveBegin + " — managed by `worklog install`. Edit the repo copy\n")
	b.WriteString("     (ai-devboard/CLAUDE.md), not this block; it is replaced on install. -->\n")
	b.Write(bytes.TrimRight(src, "\n"))
	b.WriteString("\n" + directiveEnd + "\n")
	return b.Bytes()
}

// DirectiveState describes what a file currently holds.
type DirectiveState int

const (
	// DirectiveAbsent — no managed block and no recognisable directive.
	DirectiveAbsent DirectiveState = iota
	// DirectiveCurrent — a managed block matching what we would write.
	DirectiveCurrent
	// DirectiveStale — a managed block whose content differs. This is the
	// state that was undetectable before, and the one that mattered.
	DirectiveStale
	// DirectiveUnmarked — an old-style append, exactly matching the current
	// repo body, which can safely be adopted into markers.
	DirectiveUnmarked
	// DirectiveForeign — something that looks like ours but is neither a
	// managed block nor an exact match. It is reported, never rewritten:
	// a stale unmarked block matches no known byte string, and nothing ever
	// recorded which revision was appended, so it cannot be located safely.
	DirectiveForeign
)

// InspectDirective reports what is in the file, given the block we would
// write and the bare repo body it wraps.
func InspectDirective(existing, want, bareBody []byte) DirectiveState {
	begin := bytes.Index(existing, []byte(directiveBegin))
	end := bytes.Index(existing, []byte(directiveEnd))
	if begin >= 0 && end > begin {
		got := existing[begin : end+len(directiveEnd)]
		if bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(want)) {
			return DirectiveCurrent
		}
		return DirectiveStale
	}
	trimmed := bytes.TrimSpace(bareBody)
	if len(trimmed) > 0 && bytes.Contains(existing, trimmed) {
		return DirectiveUnmarked
	}
	// A heading we plausibly wrote, but not matching anything we can place.
	if bytes.Contains(existing, []byte("# Development workflow")) {
		return DirectiveForeign
	}
	return DirectiveAbsent
}

// ReplaceDirective returns the file content with the managed block replaced,
// adopted, or appended — leaving every byte outside the block untouched.
//
// It refuses the foreign case rather than guessing. Appending a second copy
// would be worse than doing nothing, and rewriting text we cannot prove we
// wrote is worse still.
func ReplaceDirective(existing, block []byte) ([]byte, error) {
	begin := bytes.Index(existing, []byte(directiveBegin))
	end := bytes.Index(existing, []byte(directiveEnd))
	if begin >= 0 && end > begin {
		var b bytes.Buffer
		b.Write(existing[:begin])
		b.Write(block)
		b.Write(existing[end+len(directiveEnd):])
		return b.Bytes(), nil
	}
	if len(bytes.TrimSpace(existing)) == 0 {
		return block, nil
	}
	// Append, with a separator. The old code wrote straight onto the end,
	// so a file without a trailing newline had the directive glued to its
	// last line.
	var b bytes.Buffer
	b.Write(existing)
	if !bytes.HasSuffix(existing, []byte("\n")) {
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.Write(block)
	return b.Bytes(), nil
}

// AdoptDirective wraps an existing unmarked block in markers IN PLACE, so a
// later run can manage it and an uninstall can find it.
//
// Only ever called for an exact match against the current repo body. That
// restriction is the whole safety argument: a stale or hand-edited unmarked
// block is indistinguishable from the human's own prose.
func AdoptDirective(existing, bareBody, block []byte) ([]byte, bool) {
	trimmed := bytes.TrimSpace(bareBody)
	i := bytes.Index(existing, trimmed)
	if len(trimmed) == 0 || i < 0 {
		return existing, false
	}
	var b bytes.Buffer
	b.Write(existing[:i])
	b.Write(bytes.TrimRight(block, "\n"))
	b.Write(existing[i+len(trimmed):])
	return b.Bytes(), true
}

// WriteFileWithBackup writes atomically and keeps one backup, matching what
// the settings writer does. The old directive path did neither.
func WriteFileWithBackup(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if prior, err := os.ReadFile(path); err == nil {
		if err := os.WriteFile(path+".bak", prior, 0o644); err != nil {
			return fmt.Errorf("writing backup: %w", err)
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".claudemd-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// HasManagedDirective reports whether a managed block is present, asking only
// the file's own bytes.
//
// InspectDirective cannot answer this for uninstall: its signature takes the
// block we WOULD write and the bare repo body, both of which are derived from
// a checkout. Uninstall has no checkout by design — that is the whole point of
// it being a binary subcommand — so the question has to be answerable from the
// markers alone. It is, and that is exactly what the markers were introduced
// for.
func HasManagedDirective(existing []byte) bool {
	begin := bytes.Index(existing, []byte(directiveBegin))
	end := bytes.Index(existing, []byte(directiveEnd))
	return begin >= 0 && end > begin
}

// RemoveDirective cuts the managed block out, returning the remaining bytes
// and whether anything was removed.
//
// It removes ONLY what lies between and including the markers. Everything
// outside them is the human's and is copied through untouched — the same split
// install writes under, read in the other direction.
//
// An unmarked or foreign directive is deliberately NOT removed. A block with
// no markers cannot be told apart from the human's own prose about this
// workflow, nothing ever recorded which revision was appended, and guessing
// would mean deleting text we cannot prove we wrote. Install has the same rule
// and reports rather than rewriting; the asymmetry with an ADOPTED block is
// the reason adoption exists at all.
func RemoveDirective(existing []byte) ([]byte, bool) {
	begin := bytes.Index(existing, []byte(directiveBegin))
	end := bytes.Index(existing, []byte(directiveEnd))
	if begin < 0 || end <= begin {
		return existing, false
	}
	prefix := existing[:begin]
	suffix := existing[end+len(directiveEnd):]

	// Heal the seam. ReplaceDirective inserts a blank line before an
	// appended block, so removing the block without removing that blank
	// line would leave the file gaining whitespace on every install and
	// uninstall cycle.
	var b bytes.Buffer
	b.Write(bytes.TrimRight(prefix, "\n\t "))
	tail := bytes.TrimLeft(suffix, "\n")
	if b.Len() > 0 {
		b.WriteString("\n")
		if len(tail) > 0 {
			b.WriteString("\n")
		}
	}
	b.Write(tail)
	return b.Bytes(), true
}
