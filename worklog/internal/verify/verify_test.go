package verify

import (
	"crypto/sha256"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/convert"
	"github.com/prestontallen/ai-devboard/worklog/internal/migrate"
	"github.com/prestontallen/ai-devboard/worklog/internal/projection"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/memstore"
)

const corpusDir = "../convert/testdata/corpus"

func corpusSources(t *testing.T) migrate.Sources {
	t.Helper()
	abs, err := filepath.Abs(corpusDir)
	if err != nil {
		t.Fatal(err)
	}
	return migrate.Sources{
		WorklogDir:  abs,
		DevboardDir: filepath.Join(abs, "devboard"),
	}
}

// TestVerifyCleanCorpus is criterion 1: a corpus at a render fixpoint must
// report zero drift. The hand-authored fixture corpus itself is NOT such a
// fixpoint — it deliberately contains things a canonical render normalizes
// away (an implicit epic/child parent relation left unstated on the child
// side, a "implement" phase alias, a bare producer YAML with no worklog:
// join) — see TestRenderFixpoint for the same reasoning. So this test
// first renders the corpus once to get a genuinely canonical directory,
// then verifies THAT against itself.
func TestVerifyCleanCorpus(t *testing.T) {
	s := memstore.New()
	loadCorpus(t, s, corpusDir)
	canonical := t.TempDir()
	if err := projection.RenderAll(s, canonical); err != nil {
		t.Fatal(err)
	}

	rep, err := Run(memstore.New(), migrate.Sources{
		WorklogDir:  canonical,
		DevboardDir: filepath.Join(canonical, "devboard"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Clean() {
		t.Fatalf("expected clean report, got %d drifts: %+v", len(rep.Drifts), rep.Drifts)
	}
}

func loadCorpus(t *testing.T, s store.Store, dir string) {
	t.Helper()
	c, err := convert.ReadCorpusDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := convert.Load(s, c); err != nil {
		t.Fatal(err)
	}
}

// TestVerifySingleStagedRead is criterion 12: the store-conversion read and
// the devboard-feed comparator's read must consume one single staged
// snapshot, not two independent live-directory walks.
func TestVerifySingleStagedRead(t *testing.T) {
	calls := 0
	orig := stageFunc
	stageFunc = func(src migrate.Sources, dst string) error {
		calls++
		return orig(src, dst)
	}
	t.Cleanup(func() { stageFunc = orig })

	if _, err := Run(memstore.New(), corpusSources(t)); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("expected exactly one staging call, got %d", calls)
	}
}

// TestVerifyDetectsTornRead is criterion 9: a torn read must surface as a
// distinct error, never a false-positive drift report, never a crash.
func TestVerifyDetectsTornRead(t *testing.T) {
	torn := &migrate.ErrTornSnapshot{Detail: "simulated concurrent write"}
	orig := stageFunc
	stageFunc = func(src migrate.Sources, dst string) error { return torn }
	t.Cleanup(func() { stageFunc = orig })

	_, err := Run(memstore.New(), corpusSources(t))
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	var got *migrate.ErrTornSnapshot
	if !errors.As(err, &got) {
		t.Fatalf("expected *migrate.ErrTornSnapshot, got %T: %v", err, err)
	}
}

// TestVerifyNeverWritesLiveDirs is criterion 8: neither live directory is
// ever written, clean or drifted, success or failure.
func TestVerifyNeverWritesLiveDirs(t *testing.T) {
	live := t.TempDir()
	worklogDir := filepath.Join(live, "worklog")
	devboardDir := filepath.Join(live, "devboard")
	mustCopyDir(t, corpusDir, worklogDir)
	mustCopyDir(t, filepath.Join(corpusDir, "devboard"), devboardDir)
	// worklogDir shouldn't itself carry a nested devboard/ once split out;
	// convert.ReadCorpusDir's staged read only look at src.DevboardDir, and
	// listCorpusFiles never touches src.WorklogDir/devboard, so leaving it
	// in place is harmless — but remove it to keep the "live" fixture an
	// honest stand-in for the real sibling-directory layout.
	os.RemoveAll(filepath.Join(worklogDir, "devboard"))

	before := checksumTree(t, live)
	if _, err := Run(memstore.New(), migrate.Sources{WorklogDir: worklogDir, DevboardDir: devboardDir}); err != nil {
		t.Fatal(err)
	}
	after := checksumTree(t, live)
	if before != after {
		t.Error("live directory checksum changed after worklog verify")
	}
}

func mustCopyDir(t *testing.T, src, dst string) {
	t.Helper()
	if err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func checksumTree(t *testing.T, root string) string {
	t.Helper()
	var files []string
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		files = append(files, path)
		return nil
	})
	sort.Strings(files)
	h := sha256.New()
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		h.Write([]byte(f))
		h.Write(data)
	}
	return string(h.Sum(nil))
}
