package hazard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// corpus writes files into a scratch worklog/devboard pair. Keys are
// slash-relative; a "devboard/" prefix routes into the devboard root.
func corpus(t *testing.T, files map[string]string) (live, board string) {
	t.Helper()
	root := t.TempDir()
	live = filepath.Join(root, "worklog")
	board = filepath.Join(root, "devboard")
	for rel, body := range files {
		dst := live
		if strings.HasPrefix(rel, "devboard/") {
			dst, rel = board, strings.TrimPrefix(rel, "devboard/")
		}
		p := filepath.Join(dst, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(board); err != nil {
		board = ""
	}
	return live, board
}

func constructs(t *testing.T, live, board string) map[string]Finding {
	t.Helper()
	found, err := Scan(live, board)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	out := map[string]Finding{}
	for _, f := range found {
		out[f.Construct] = f
	}
	return out
}

// mustFire is the load-bearing assertion: a detector that cannot fire is
// worse than no detector, because a clean scan then reads as a guarantee.
func mustFire(t *testing.T, got map[string]Finding, construct string) {
	t.Helper()
	if _, ok := got[construct]; !ok {
		keys := make([]string, 0, len(got))
		for k := range got {
			keys = append(keys, k)
		}
		t.Errorf("%s did not fire; got %v", construct, keys)
	}
}

const cleanWork = "<!-- generated -->\n# Worklog — active\n\n## Next\n\n- [ ] **SOLO** — A ticket\n  - **ID**: solo\n"

func TestWorkMDConstructs(t *testing.T) {
	t.Run("preamble", func(t *testing.T) {
		live, board := corpus(t, map[string]string{
			"WORK.md": "# Work\n\nSome prose the reader throws away.\n\n## Next\n\n- [ ] **SOLO** — A ticket\n  - **ID**: solo\n",
		})
		mustFire(t, constructs(t, live, board), "workmd-preamble")
	})

	t.Run("empty field value", func(t *testing.T) {
		live, board := corpus(t, map[string]string{
			"WORK.md": cleanWork + "  - **Repo**:\n",
		})
		mustFire(t, constructs(t, live, board), "empty-field-value")
	})

	t.Run("empty PR is allowed", func(t *testing.T) {
		live, board := corpus(t, map[string]string{
			"WORK.md": cleanWork + "  - **PR**:\n",
		})
		if got := constructs(t, live, board); len(got) != 0 {
			t.Errorf("an empty PR bullet is deliberately re-emitted; got %v", got)
		}
	})

	t.Run("duplicate field label", func(t *testing.T) {
		live, board := corpus(t, map[string]string{
			"WORK.md": cleanWork + "  - **Repo**: a\n  - **Repo**: b\n",
		})
		mustFire(t, constructs(t, live, board), "duplicate-field-label")
	})
}

func TestArchiveConstructs(t *testing.T) {
	t.Run("entry missing completed", func(t *testing.T) {
		live, board := corpus(t, map[string]string{
			"WORK.md":            cleanWork,
			"archive/2026-09.md": "# Archive\n\n## 2026-09-03\n\n### old-thing — A title\n- **Summary**: done\n",
		})
		mustFire(t, constructs(t, live, board), "archive-entry-missing-completed")
	})

	t.Run("day heading mismatch", func(t *testing.T) {
		live, board := corpus(t, map[string]string{
			"WORK.md":            cleanWork,
			"archive/2026-09.md": "# Archive\n\n## 2026-09-03\n\n### old-thing — A title\n- **Completed**: 2026-09-01\n",
		})
		mustFire(t, constructs(t, live, board), "archive-day-mismatch")
	})

	t.Run("agreeing entry is clean", func(t *testing.T) {
		live, board := corpus(t, map[string]string{
			"WORK.md":            cleanWork,
			"archive/2026-09.md": "# Archive\n\n## 2026-09-03\n\n### old-thing — A title\n- **Completed**: 2026-09-03\n",
		})
		if got := constructs(t, live, board); len(got) != 0 {
			t.Errorf("an entry agreeing with its day heading is clean; got %v", got)
		}
	})
}

func TestFeedbackUnknownField(t *testing.T) {
	live, board := corpus(t, map[string]string{
		"WORK.md":     cleanWork,
		"FEEDBACK.md": "# Worklog Feedback Log\n\n## 1 — tui-error\n**Trigger**: x\n**Severity**: high\n",
	})
	mustFire(t, constructs(t, live, board), "feedback-unknown-field")
}

func TestDevboardConstructs(t *testing.T) {
	base := map[string]string{"WORK.md": cleanWork}

	t.Run("comment", func(t *testing.T) {
		f := map[string]string{"devboard/r/solo.yaml": "schema: 1\nworklog: solo\n# a note to self\nphase: done\n"}
		for k, v := range base {
			f[k] = v
		}
		live, board := corpus(t, f)
		mustFire(t, constructs(t, live, board), "yaml-comment")
	})

	t.Run("anchor", func(t *testing.T) {
		f := map[string]string{"devboard/r/solo.yaml": "schema: 1\nworklog: solo\nbranch: &b main\nsession: *b\n"}
		for k, v := range base {
			f[k] = v
		}
		live, board := corpus(t, f)
		mustFire(t, constructs(t, live, board), "yaml-anchor")
	})

	t.Run("duplicate key", func(t *testing.T) {
		f := map[string]string{"devboard/r/solo.yaml": "schema: 1\nworklog: solo\nphase: done\nphase: implementing\n"}
		for k, v := range base {
			f[k] = v
		}
		live, board := corpus(t, f)
		mustFire(t, constructs(t, live, board), "yaml-duplicate-key")
	})

	t.Run("title mismatch", func(t *testing.T) {
		f := map[string]string{"devboard/r/solo.yaml": "schema: 1\nworklog: solo\ntitle: Something else entirely\n"}
		for k, v := range base {
			f[k] = v
		}
		live, board := corpus(t, f)
		mustFire(t, constructs(t, live, board), "devboard-title-mismatch")
	})

	t.Run("matching title is clean", func(t *testing.T) {
		f := map[string]string{"devboard/r/solo.yaml": "schema: 1\nworklog: solo\ntitle: A ticket\n"}
		for k, v := range base {
			f[k] = v
		}
		live, board := corpus(t, f)
		if got := constructs(t, live, board); len(got) != 0 {
			t.Errorf("a title agreeing with the ticket is clean; got %v", got)
		}
	})

	// Unquoted scalars carrying apostrophes are what a text-scanning first
	// cut got wrong, reporting mismatches on the live corpus that were
	// artefacts of guessing at quoting.
	t.Run("apostrophes are not a mismatch", func(t *testing.T) {
		f := map[string]string{
			"WORK.md":              "<!-- g -->\n# Worklog — active\n\n## Next\n\n- [ ] **SOLO** — the CLI only accepts 'implementing'\n  - **ID**: solo\n",
			"devboard/r/solo.yaml": "schema: 1\nworklog: solo\ntitle: the CLI only accepts 'implementing'\n",
		}
		live, board := corpus(t, f)
		if got := constructs(t, live, board); len(got) != 0 {
			t.Errorf("apostrophes in an unquoted scalar are not a mismatch; got %v", got)
		}
	})

	t.Run("duplicate join", func(t *testing.T) {
		f := map[string]string{
			"devboard/r/solo.yaml":   "schema: 1\nworklog: solo\n",
			"devboard/r2/other.yaml": "schema: 1\nworklog: solo\n",
		}
		for k, v := range base {
			f[k] = v
		}
		live, board := corpus(t, f)
		mustFire(t, constructs(t, live, board), "devboard-duplicate-join")
	})
}
