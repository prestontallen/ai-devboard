package adopt

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fingerprint is an independent description of a tree: every path and its
// digest. The tests compare fingerprints rather than trusting the manifest,
// so a manifest that agrees with itself cannot pass on its own.
func fingerprint(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	if root == "" {
		return out
	}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		rel, _ := filepath.Rel(root, p)
		sum := sha256.Sum256(data)
		out[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func equalTrees(t *testing.T, a, b map[string]string, label string) {
	t.Helper()
	if len(a) != len(b) {
		t.Errorf("%s: %d files, want %d\n got=%v\nwant=%v", label, len(b), len(a), keys(b), keys(a))
		return
	}
	for k, v := range a {
		if b[k] != v {
			t.Errorf("%s: %s differs (want %s, got %s)", label, k, v, b[k])
		}
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func corpus(t *testing.T) (Roots, string) {
	t.Helper()
	root := t.TempDir()
	r := Roots{Worklog: filepath.Join(root, "worklog"), Devboard: filepath.Join(root, "devboard")}
	write(t, r.Worklog, "WORK.md", "# Worklog — active\n\n## Next\n")
	write(t, r.Worklog, "FEEDBACK.md", "# Worklog Feedback Log\n")
	write(t, r.Worklog, "notes/a.md", "note a\n")
	write(t, r.Worklog, "archive/2026-09.md", "# Archive\n")
	// Deliberately included: the converter never reads these, which is
	// exactly why a snapshot must.
	write(t, r.Worklog, "notes/stale.md.bak", "old\n")
	write(t, r.Worklog, "INDEX.md", "# Index\n")
	write(t, r.Devboard, "repo/a.yaml", "schema: 1\nworklog: a\n")
	return r, filepath.Join(root, "snap")
}

// TestSnapshotCapturesEveryFile: a snapshot that captures only what the
// converter understands cannot restore what it does not.
func TestSnapshotCapturesEveryFile(t *testing.T) {
	r, dest := corpus(t)
	m, err := Snapshot(r, dest)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"WORK.md", "FEEDBACK.md", "notes/a.md", "archive/2026-09.md", "notes/stale.md.bak", "INDEX.md"} {
		if _, ok := m.Worklog[rel]; !ok {
			t.Errorf("snapshot missed %s", rel)
		}
	}
	if _, ok := m.Devboard["repo/a.yaml"]; !ok {
		t.Error("snapshot missed the devboard file")
	}
	equalTrees(t, fingerprint(t, r.Worklog), fingerprint(t, filepath.Join(dest, "worklog")), "worklog copy")
	equalTrees(t, fingerprint(t, r.Devboard), fingerprint(t, filepath.Join(dest, "devboard")), "devboard copy")
}

// TestRestoreIsByteExact is criterion 8.
func TestRestoreIsByteExact(t *testing.T) {
	r, dest := corpus(t)
	before := fingerprint(t, r.Worklog)
	beforeBoard := fingerprint(t, r.Devboard)
	if _, err := Snapshot(r, dest); err != nil {
		t.Fatal(err)
	}

	// Mutate the way a half-finished adoption would: rewrite, delete, add.
	write(t, r.Worklog, "WORK.md", "# Worklog — active\n\n## Now\n\n- [ ] **X** — new\n")
	if err := os.Remove(filepath.Join(r.Worklog, "notes", "a.md")); err != nil {
		t.Fatal(err)
	}
	write(t, r.Worklog, "notes/invented.md", "should not survive a rollback\n")
	write(t, r.Devboard, "repo/b.yaml", "schema: 1\nworklog: b\n")

	if err := Restore(dest, r); err != nil {
		t.Fatalf("restore: %v", err)
	}
	equalTrees(t, before, fingerprint(t, r.Worklog), "worklog after restore")
	equalTrees(t, beforeBoard, fingerprint(t, r.Devboard), "devboard after restore")
}

// TestRestoreSurvivesTheCheckersBeingWrong is criterion 9, and it is the
// claim the whole design rests on: recoverability does not depend on any
// correctness machinery being right. Here the "adoption" is actively
// destructive — it truncates every file it touches and invents new ones —
// and the rollback must still land byte-exact.
func TestRestoreSurvivesTheCheckersBeingWrong(t *testing.T) {
	r, dest := corpus(t)
	before := fingerprint(t, r.Worklog)
	if _, err := Snapshot(r, dest); err != nil {
		t.Fatal(err)
	}

	err := filepath.WalkDir(r.Worklog, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		return os.WriteFile(p, nil, 0o644) // truncate everything
	})
	if err != nil {
		t.Fatal(err)
	}
	write(t, r.Worklog, "notes/garbage.md", "junk\n")

	if err := Restore(dest, r); err != nil {
		t.Fatalf("restore after a destructive run: %v", err)
	}
	equalTrees(t, before, fingerprint(t, r.Worklog), "worklog after a destructive run")
}

// TestRestoreRefusesACorruptSnapshot: a bad backup must never be allowed to
// overwrite a live tree.
func TestRestoreRefusesACorruptSnapshot(t *testing.T) {
	r, dest := corpus(t)
	if _, err := Snapshot(r, dest); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dest, "worklog"), "WORK.md", "tampered\n")

	live := fingerprint(t, r.Worklog)
	if err := Restore(dest, r); err == nil {
		t.Fatal("restore accepted a snapshot whose bytes disagree with its manifest")
	} else if !strings.Contains(err.Error(), "unverifiable") {
		t.Errorf("error = %v, want it to name the unverifiable snapshot", err)
	}
	equalTrees(t, live, fingerprint(t, r.Worklog), "live tree after a refused restore")
}

// TestVerifyDetectsTamper covers the digest check on its own.
func TestVerifyDetectsTamper(t *testing.T) {
	r, dest := corpus(t)
	if _, err := Snapshot(r, dest); err != nil {
		t.Fatal(err)
	}
	if err := Verify(dest); err != nil {
		t.Fatalf("fresh snapshot did not verify: %v", err)
	}
	write(t, filepath.Join(dest, "worklog"), "notes/a.md", "changed\n")
	if err := Verify(dest); err == nil {
		t.Error("Verify accepted a tampered snapshot")
	}
}

// TestSnapshotRefusesNonRegularFiles: a symlink cannot be promised back.
func TestSnapshotRefusesNonRegularFiles(t *testing.T) {
	r, dest := corpus(t)
	if err := os.Symlink(filepath.Join(r.Worklog, "notes", "a.md"), filepath.Join(r.Worklog, "notes", "link.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := Snapshot(r, dest); err == nil {
		t.Error("Snapshot accepted a symlink it cannot restore")
	}
}

// TestRestoreWithoutDevboard: devboard is opt-in by directory presence.
func TestRestoreWithoutDevboard(t *testing.T) {
	root := t.TempDir()
	r := Roots{Worklog: filepath.Join(root, "worklog")}
	write(t, r.Worklog, "WORK.md", "# Worklog — active\n")
	dest := filepath.Join(root, "snap")

	before := fingerprint(t, r.Worklog)
	if _, err := Snapshot(r, dest); err != nil {
		t.Fatal(err)
	}
	write(t, r.Worklog, "WORK.md", "changed\n")
	if err := Restore(dest, r); err != nil {
		t.Fatal(err)
	}
	equalTrees(t, before, fingerprint(t, r.Worklog), "worklog after restore")
}

// TestSnapshotRoundTripSnapshot exercises snapshot → destructive mutation →
// restore against a real corpus copy named by WORKLOG_SNAPSHOT.
func TestSnapshotRoundTripSnapshot(t *testing.T) {
	src := os.Getenv("WORKLOG_SNAPSHOT")
	if src == "" {
		t.Skip("set WORKLOG_SNAPSHOT to exercise a real corpus")
	}
	root := t.TempDir()
	r := Roots{Worklog: filepath.Join(root, "worklog")}
	if err := copyTree(src, r.Worklog, "", map[string]string{}); err != nil {
		t.Fatal(err)
	}
	if board := os.Getenv("DEVBOARD_SNAPSHOT"); board != "" {
		r.Devboard = filepath.Join(root, "devboard")
		if err := copyTree(board, r.Devboard, "", map[string]string{}); err != nil {
			t.Fatal(err)
		}
	}

	before, beforeBoard := fingerprint(t, r.Worklog), fingerprint(t, r.Devboard)
	m, err := Snapshot(r, filepath.Join(root, "snap"))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("snapshot captured %s", m.Describe())

	// Destroy the corpus the way a botched adoption would.
	if err := filepath.WalkDir(r.Worklog, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		return os.WriteFile(p, []byte("destroyed\n"), 0o644)
	}); err != nil {
		t.Fatal(err)
	}
	write(t, r.Worklog, "notes/invented.md", "junk\n")

	if err := Restore(filepath.Join(root, "snap"), r); err != nil {
		t.Fatalf("restore: %v", err)
	}
	equalTrees(t, before, fingerprint(t, r.Worklog), "real worklog after restore")
	equalTrees(t, beforeBoard, fingerprint(t, r.Devboard), "real devboard after restore")
}
