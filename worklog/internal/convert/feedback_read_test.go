package convert

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReadCorpusDirRefusesUnreadableFeedback pins the destructive case.
//
// ReadCorpusDir used to accept any FEEDBACK.md read error as "no feedback",
// which is silently destructive rather than merely lossy: the conversion
// carries zero entries, and the next `feedback` write renders the file back
// from those zero entries — replacing a friction log that was only
// unreadable at that moment. A missing file still means "none yet".
func TestReadCorpusDirRefusesUnreadableFeedback(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: mode bits do not deny reads")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "WORK.md"), []byte("# Worklog — active\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fb := filepath.Join(root, "FEEDBACK.md")
	if err := os.WriteFile(fb, []byte("# Worklog Feedback Log\n\n## 1 — tui-error\n**Trigger**: x\n"), 0o000); err != nil {
		t.Fatal(err)
	}

	if _, err := ReadCorpusDir(root); err == nil {
		t.Fatal("ReadCorpusDir accepted an unreadable FEEDBACK.md; the next feedback write would render over it")
	}
}

// TestReadCorpusDirAllowsMissingFeedback keeps the legitimate case working:
// absent is "no feedback yet", not an error.
func TestReadCorpusDirAllowsMissingFeedback(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "WORK.md"), []byte("# Worklog — active\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := ReadCorpusDir(root)
	if err != nil {
		t.Fatalf("ReadCorpusDir with no FEEDBACK.md: %v", err)
	}
	if len(c.Feedback) != 0 {
		t.Errorf("Feedback = %q, want empty", c.Feedback)
	}
}
