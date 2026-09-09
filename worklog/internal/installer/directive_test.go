package installer

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var body = []byte("# Development workflow\n\nDo the thing.\n")

func TestStaleBlockIsDetected(t *testing.T) {
	want := MarkedDirective(body)
	stale := MarkedDirective([]byte("# Development workflow\n\nDo the OLD thing.\n"))

	if got := InspectDirective(stale, want, body); got != DirectiveStale {
		t.Errorf("state = %v, want DirectiveStale — this is the case that went unnoticed for weeks", got)
	}
	if got := InspectDirective(want, want, body); got != DirectiveCurrent {
		t.Errorf("state = %v, want DirectiveCurrent", got)
	}
}

// TestOutsideTheMarkersIsUntouched is the rule split: inside the block the
// repo wins, outside it we never touch a byte we did not write.
func TestOutsideTheMarkersIsUntouched(t *testing.T) {
	mine := []byte("# My own notes\n\nKeep me.\n\n")
	after := []byte("\n# More of mine\n\nAlso keep me.\n")
	existing := append(append(append([]byte{}, mine...), MarkedDirective([]byte("old body\n"))...), after...)

	next, err := ReplaceDirective(existing, MarkedDirective(body))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(next, mine) {
		t.Error("content before the block was lost")
	}
	if !bytes.HasSuffix(next, after) {
		t.Error("content after the block was lost")
	}
	if !bytes.Contains(next, []byte("Do the thing.")) {
		t.Error("the block was not replaced")
	}
	if bytes.Contains(next, []byte("old body")) {
		t.Error("the old block survived")
	}
}

// TestUnmarkedExactMatchIsAdoptedInPlace: the migration case. Every machine
// except the one repaired by hand has an unmarked directive.
func TestUnmarkedExactMatchIsAdoptedInPlace(t *testing.T) {
	existing := append([]byte("# Mine\n\n"), body...)
	if got := InspectDirective(existing, MarkedDirective(body), body); got != DirectiveUnmarked {
		t.Fatalf("state = %v, want DirectiveUnmarked", got)
	}
	next, ok := AdoptDirective(existing, body, MarkedDirective(body))
	if !ok {
		t.Fatal("an exact match was not adopted")
	}
	if !bytes.HasPrefix(next, []byte("# Mine\n\n")) {
		t.Error("the human's content moved")
	}
	if n := bytes.Count(next, []byte("Do the thing.")); n != 1 {
		t.Errorf("directive appears %d times — adoption duplicated it", n)
	}
	if !bytes.Contains(next, []byte(directiveBegin)) {
		t.Error("no markers after adoption")
	}
}

// TestUnknownBlockIsNotRewritten: a stale UNMARKED block matches no known
// byte string and nothing recorded which revision was appended, so it cannot
// be located safely. Reported, never rewritten, never duplicated.
func TestUnknownBlockIsNotRewritten(t *testing.T) {
	existing := []byte("# Development workflow\n\nSomething nobody can place.\n")
	if got := InspectDirective(existing, MarkedDirective(body), body); got != DirectiveForeign {
		t.Errorf("state = %v, want DirectiveForeign", got)
	}
	if _, ok := AdoptDirective(existing, body, MarkedDirective(body)); ok {
		t.Error("a block we cannot place was adopted anyway")
	}
}

// TestAppendSeparatesAndBacksUp: the old path was an O_APPEND write with an
// unchecked error, no backup and no separator, so a file with no trailing
// newline had the directive glued onto its last line.
func TestAppendSeparatesAndBacksUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(path, []byte("no trailing newline"), 0o644); err != nil {
		t.Fatal(err)
	}
	existing, _ := os.ReadFile(path)
	next, err := ReplaceDirective(existing, MarkedDirective(body))
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteFileWithBackup(path, next); err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(path)
	if strings.Contains(string(got), "no trailing newline<!--") {
		t.Error("the directive was glued onto the last line")
	}
	if !strings.HasPrefix(string(got), "no trailing newline\n") {
		t.Errorf("the human's content changed: %q", string(got)[:40])
	}
	bak, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("no backup was written: %v", err)
	}
	if string(bak) != "no trailing newline" {
		t.Errorf("backup holds %q", bak)
	}
}

func TestEmptyFileGetsJustTheBlock(t *testing.T) {
	next, err := ReplaceDirective(nil, MarkedDirective(body))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(next, []byte(directiveBegin)) {
		t.Error("an empty file did not start with the block")
	}
}
