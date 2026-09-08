package serve

// The shadow criteria battery (adb-store-serve-shadow). The fixture is a
// store seeded from the convert corpus — the store-shaped tree, not this
// package's file corpus, which is mostly bare producer files the store
// deliberately does not own (that's what scout finding 2 was about) —
// rendered to a temp layout by the same RenderTo the live install uses.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prestontallen/ai-devboard/worklog/internal/convert"
	"github.com/prestontallen/ai-devboard/worklog/internal/projection"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/memstore"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/sqlitestore"
)

const convertCorpus = "../convert/testdata/corpus"

func seededStore(t *testing.T, s store.Store) store.Store {
	t.Helper()
	c, err := convert.ReadCorpusDir(convertCorpus)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := convert.Load(s, c); err != nil {
		t.Fatal(err)
	}
	return s
}

func snapshotFrom(s store.Store) func() (*StoreSnapshot, error) {
	return func() (*StoreSnapshot, error) {
		files, stamps, err := projection.RenderSnapshot(s)
		if err != nil {
			return nil, err
		}
		return &StoreSnapshot{Files: files, BoardMTimes: stamps}, nil
	}
}

// shadowFixture renders a seeded store into a temp layout and returns a
// shadow-enabled server over it.
func shadowFixture(t *testing.T) (store.Store, *Server, string) {
	t.Helper()
	s := seededStore(t, memstore.New())
	root := t.TempDir()
	l := projection.Layout{WorklogDir: root, DevboardDir: filepath.Join(root, "devboard")}
	if err := projection.RenderTo(s, l); err != nil {
		t.Fatal(err)
	}
	srv := New(Config{
		DataDir:      filepath.Join(root, "devboard"),
		WorklogDir:   root,
		ScanInterval: time.Second,
		StoreShadow:  true,
	})
	srv.LoadStoreSnapshot = snapshotFrom(s)
	return s, srv, root
}

func mustSnapshot(t *testing.T, srv *Server) *StoreSnapshot {
	t.Helper()
	snap, err := srv.LoadStoreSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snap == nil {
		t.Fatal("nil snapshot from fixture loader")
	}
	return snap
}

// alignVolatile copies the two legitimately-request-scoped fields over so
// the equality claim is exactly "normalizing version and generated only".
func alignVolatile(fileP, storeP map[string]any) {
	storeP["version"] = fileP["version"]
	storeP["generated"] = fileP["generated"]
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })
	return &buf
}

func getTasks(t *testing.T, srv *Server) (int, []byte) {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/tasks", nil))
	return rec.Code, rec.Body.Bytes()
}

// TestShadowPayloadEqual is criterion 1: over a store-seeded corpus the
// two builders agree byte-for-byte after normalizing version and generated
// only — mtime included, backlog included.
func TestShadowPayloadEqual(t *testing.T) {
	_, srv, _ := shadowFixture(t)
	snap := mustSnapshot(t, srv)
	fileP := srv.allTasks()
	storeP := srv.allTasksStore(snap)
	alignVolatile(fileP, storeP)

	if diffs := diffPayloads(fileP, storeP); len(diffs) != 0 {
		t.Fatalf("payloads differ:\n%s", renderDiffs(diffs))
	}
	fb, err := json.Marshal(fileP)
	if err != nil {
		t.Fatal(err)
	}
	sb, err := json.Marshal(storeP)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(fb, sb) {
		t.Fatal("differ found no differences but marshaled payloads are not byte-equal")
	}
}

func renderDiffs(diffs []shadowDiff) string {
	var b strings.Builder
	for _, d := range diffs {
		fmt.Fprintf(&b, "  repo=%q task=%q path=%s %s\n", d.Repo, d.Task, d.Path, d.Detail)
	}
	return b.String()
}

// TestShadowHybridPassthrough is criterion 2: a bare producer file and an
// unparseable file appear identically on both sides — file-derived — and
// are never reported as drift.
func TestShadowHybridPassthrough(t *testing.T) {
	_, srv, root := shadowFixture(t)

	bare, err := os.ReadFile(filepath.Join(convertCorpus, "devboard", "nole", "bare.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	nole := filepath.Join(root, "devboard", "nole")
	if err := os.MkdirAll(nole, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nole, "bare.yaml"), bare, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nole, "broken.yaml"), []byte("{\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	snap := mustSnapshot(t, srv)
	fileP := srv.allTasks()
	storeP := srv.allTasksStore(snap)
	alignVolatile(fileP, storeP)
	if diffs := diffPayloads(fileP, storeP); len(diffs) != 0 {
		t.Fatalf("passthrough files reported as drift:\n%s", renderDiffs(diffs))
	}

	for _, p := range []map[string]any{fileP, storeP} {
		if e := findEntry(p, "nole", "bare"); e == nil {
			t.Fatal("bare producer file missing from a payload")
		}
		e := findEntry(p, "nole", "broken")
		if e == nil {
			t.Fatal("unparseable file missing from a payload")
		}
		if _, ok := e["error"]; !ok {
			t.Fatalf("unparseable file did not become an error card: %v", e)
		}
	}
}

func findEntry(payload map[string]any, repo, id string) map[string]any {
	repos, _ := payload["repos"].([]any)
	for _, r := range repos {
		g, _ := r.(map[string]any)
		if g["repo"] != repo {
			continue
		}
		tasks, _ := g["tasks"].([]any)
		for _, tv := range tasks {
			e, _ := tv.(map[string]any)
			if e["id"] == id {
				return e
			}
		}
	}
	return nil
}

// TestShadowIdentityStable is criterion 3: file, id, archived and
// repo-group membership — the archive endpoint's lookup key and the
// frontend's deep-link key — match entry for entry, order included. Runs
// with a board-archived ticket in play so both archived sources are
// exercised.
func TestShadowIdentityStable(t *testing.T) {
	s, srv, root := shadowFixture(t)

	tk := firstBoardTicket(t, s)
	tk.BoardArchived = true
	if err := s.PutTicket(tk); err != nil {
		t.Fatal(err)
	}
	l := projection.Layout{WorklogDir: root, DevboardDir: filepath.Join(root, "devboard")}
	if err := projection.RenderTo(s, l); err != nil {
		t.Fatal(err)
	}

	snap := mustSnapshot(t, srv)
	fileIDs := identityTuples(srv.allTasks())
	storeIDs := identityTuples(srv.allTasksStore(snap))
	if fmt.Sprint(fileIDs) != fmt.Sprint(storeIDs) {
		t.Fatalf("identity tuples diverge:\nfile:  %v\nstore: %v", fileIDs, storeIDs)
	}
	var archived bool
	for _, tup := range fileIDs {
		archived = archived || strings.Contains(tup, "archived")
	}
	if !archived {
		t.Fatal("fixture exercised no archived entry")
	}
}

func firstBoardTicket(t *testing.T, s store.Store) *store.Ticket {
	t.Helper()
	tickets, err := s.Tickets()
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range tickets {
		if tk.BoardTracked && tk.ParentID == "" {
			return tk
		}
	}
	t.Fatal("no board-tracked ticket in fixture")
	return nil
}

func identityTuples(payload map[string]any) []string {
	var out []string
	repos, _ := payload["repos"].([]any)
	for _, r := range repos {
		g, _ := r.(map[string]any)
		tasks, _ := g["tasks"].([]any)
		for _, tv := range tasks {
			e, _ := tv.(map[string]any)
			tup := fmt.Sprintf("%v/%v/%v", g["repo"], e["file"], e["id"])
			if e["archived"] == true {
				tup += "/archived"
			}
			out = append(out, tup)
		}
	}
	return out
}

// TestShadowOffByDefault is criterion 6: flag unset, the loader is never
// called and the response is byte-identical to a server with no shadow
// wiring at all (generated normalized — it is a timestamp).
func TestShadowOffByDefault(t *testing.T) {
	s := seededStore(t, memstore.New())
	root := t.TempDir()
	l := projection.Layout{WorklogDir: root, DevboardDir: filepath.Join(root, "devboard")}
	if err := projection.RenderTo(s, l); err != nil {
		t.Fatal(err)
	}
	cfg := Config{DataDir: filepath.Join(root, "devboard"), WorklogDir: root, ScanInterval: time.Second}

	called := false
	srv := New(cfg)
	srv.LoadStoreSnapshot = func() (*StoreSnapshot, error) {
		called = true
		return nil, nil
	}
	code, body := getTasks(t, srv)
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	if called {
		t.Fatal("flag unset but the store loader was called")
	}

	plain := New(cfg)
	code2, body2 := getTasks(t, plain)
	if code2 != 200 {
		t.Fatalf("status %d", code2)
	}
	if !bytes.Equal(normalizeVolatile(t, body), normalizeVolatile(t, body2)) {
		t.Fatal("shadow-wired (flag off) response differs from plain server response")
	}
}

func normalizeVolatile(t *testing.T, body []byte) []byte {
	t.Helper()
	var p map[string]any
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatal(err)
	}
	p["generated"] = float64(0)
	p["version"] = float64(0)
	out, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestShadowFailureContained is criterion 8: a broken store loader leaves
// the response a 200 with the file-built payload, and the failure is
// logged once — not once per request.
func TestShadowFailureContained(t *testing.T) {
	_, srv, _ := shadowFixture(t)
	srv.LoadStoreSnapshot = func() (*StoreSnapshot, error) {
		return nil, errors.New("boom: store locked")
	}
	buf := captureLog(t)

	var last []byte
	for i := 0; i < 3; i++ {
		code, body := getTasks(t, srv)
		if code != 200 {
			t.Fatalf("request %d: status %d", i, code)
		}
		last = body
	}
	plain := New(srv.cfg)
	_, body2 := getTasks(t, plain)
	if !bytes.Equal(normalizeVolatile(t, last), normalizeVolatile(t, body2)) {
		t.Fatal("failed shadow changed the served payload")
	}
	if got := strings.Count(buf.String(), "boom: store locked"); got != 1 {
		t.Fatalf("failure logged %d times over 3 requests, want 1:\n%s", got, buf.String())
	}
}

// TestShadowIgnoresHandEdits is criterion 9: a hand-edited projection is
// exactly what the shadow exists to catch — the board still serves (the
// EditedIn guard is not on this path), the edit is served file-side, and
// the disagreement is logged as unexpected.
func TestShadowIgnoresHandEdits(t *testing.T) {
	s, srv, root := shadowFixture(t)
	tk := firstBoardTicket(t, s)
	repo := tk.Repo
	if repo == "" {
		repo = "unknown"
	}
	path := filepath.Join(root, "devboard", repo, tk.Slug+".yaml")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("handedit: true\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	buf := captureLog(t)
	code, body := getTasks(t, srv)
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	if !bytes.Contains(body, []byte("handedit")) {
		t.Fatal("hand edit not served file-side")
	}
	if !strings.Contains(buf.String(), "UNEXPECTED") || !strings.Contains(buf.String(), "handedit") {
		t.Fatalf("hand edit not reported as unexpected drift:\n%s", buf.String())
	}
}

// TestShadowCoexistsWithMigrate is criterion 10's automated half: the
// loader opens the real sqlite store per request and closes it before
// responding, so no WAL sidecar outlives a request (migrate's
// seedWorkingCopy refuses on a non-empty WAL) and the db file can be
// swapped out from under the server between requests. The full
// `worklog migrate` / `adopt --commit` / storesync run stays manual.
func TestShadowCoexistsWithMigrate(t *testing.T) {
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "worklog.db")
	sq, err := sqlitestore.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	seededStore(t, sq)
	root := t.TempDir()
	l := projection.Layout{WorklogDir: root, DevboardDir: filepath.Join(root, "devboard")}
	if err := projection.RenderTo(sq, l); err != nil {
		t.Fatal(err)
	}
	if err := sq.Close(); err != nil {
		t.Fatal(err)
	}

	srv := New(Config{DataDir: filepath.Join(root, "devboard"), WorklogDir: root, ScanInterval: time.Second, StoreShadow: true})
	srv.LoadStoreSnapshot = func() (*StoreSnapshot, error) {
		ss, err := sqlitestore.Open(dbPath)
		if err != nil {
			return nil, err
		}
		defer ss.Close()
		files, stamps, err := projection.RenderSnapshot(ss)
		if err != nil {
			return nil, err
		}
		return &StoreSnapshot{Files: files, BoardMTimes: stamps}, nil
	}

	buf := captureLog(t)
	if code, _ := getTasks(t, srv); code != 200 {
		t.Fatalf("status %d", code)
	}
	for _, sidecar := range []string{dbPath + "-wal", dbPath + "-shm"} {
		if _, err := os.Stat(sidecar); !os.IsNotExist(err) {
			t.Fatalf("%s outlives the request — a held handle breaks migrate's swap", sidecar)
		}
	}

	// The swap: the db is renamed away and a replacement renamed in, the
	// way migrate.Run does it. The next request must serve from the new
	// file with no complaint.
	moved := dbPath + ".old"
	if err := os.Rename(dbPath, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(moved, dbPath); err != nil {
		t.Fatal(err)
	}
	if code, _ := getTasks(t, srv); code != 200 {
		t.Fatal("request after swap failed")
	}
	if strings.Contains(buf.String(), "store-shadow: skipped") {
		t.Fatalf("shadow broke across the swap:\n%s", buf.String())
	}
}

// TestShadowFlagNeedsExplicitConfig is criterion 11's unit half: the env
// flag is resolved once in ConfigFromEnv, so an embedded Server built
// with an explicit Config (verify builds two per run, the projection
// tests build one) is immune to the process environment. The suite-level
// half is running those packages' tests with the flag exported.
func TestShadowFlagNeedsExplicitConfig(t *testing.T) {
	t.Setenv("DEVBOARD_STORE_SHADOW", "1")
	if !ConfigFromEnv().StoreShadow {
		t.Fatal("ConfigFromEnv ignored DEVBOARD_STORE_SHADOW=1")
	}
	t.Setenv("DEVBOARD_STORE_SHADOW", "yes")
	if ConfigFromEnv().StoreShadow {
		t.Fatal(`only "1" enables shadow mode`)
	}

	t.Setenv("DEVBOARD_STORE_SHADOW", "1")
	srv := corpusServer(t) // explicit Config: shadow stays off
	srv.LoadStoreSnapshot = func() (*StoreSnapshot, error) {
		t.Error("embedded server with explicit Config ran the shadow")
		return nil, nil
	}
	if code, _ := getTasks(t, srv); code != 200 {
		t.Fatal("request failed")
	}
}

// TestShadowDiffReport is criterion 12: a deliberately divergent store
// produces a log that names repo, task id and JSON path per difference,
// classified expected or unexpected.
func TestShadowDiffReport(t *testing.T) {
	s, srv, _ := shadowFixture(t)
	tk := firstBoardTicket(t, s)
	tk.Branch = "divergent-branch"
	if err := s.PutTicket(tk); err != nil {
		t.Fatal(err)
	}
	if err := s.TouchBoardRendered(tk.ID, 0); err != nil { // unstamped: the expected class
		t.Fatal(err)
	}

	buf := captureLog(t)
	if code, _ := getTasks(t, srv); code != 200 {
		t.Fatalf("status %d", code)
	}
	logged := buf.String()
	repo := tk.Repo
	if repo == "" {
		repo = "unknown"
	}
	wantUnexpected := fmt.Sprintf("UNEXPECTED repo=%q task=%q", repo, tk.Slug)
	if !strings.Contains(logged, wantUnexpected) || !strings.Contains(logged, ".task.branch") {
		t.Fatalf("branch divergence not reported with repo/task/path:\n%s", logged)
	}
	if !strings.Contains(logged, "expected repo=") || !strings.Contains(logged, ".mtime") {
		t.Fatalf("unstamped mtime not classified expected:\n%s", logged)
	}
	if !strings.Contains(logged, "unexpected") || !strings.Contains(logged, "difference(s)") {
		t.Fatalf("no summary line:\n%s", logged)
	}

	// Same disagreement on the next request: reported once, not again.
	before := len(buf.String())
	if code, _ := getTasks(t, srv); code != 200 {
		t.Fatal("second request failed")
	}
	if buf.Len() != before {
		t.Fatalf("unchanged diff report repeated:\n%s", buf.String()[before:])
	}
}

// TestShadowWritesNothing is criterion 13: any number of shadowed requests
// modify nothing under the worklog or devboard trees.
func TestShadowWritesNothing(t *testing.T) {
	_, srv, root := shadowFixture(t)
	before := treeState(t, root)
	for i := 0; i < 3; i++ {
		if code, _ := getTasks(t, srv); code != 200 {
			t.Fatalf("status %d", code)
		}
	}
	if after := treeState(t, root); after != before {
		t.Fatalf("shadowed requests modified the tree:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func treeState(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "%s %d %d %x\n", path, info.Size(), info.ModTime().UnixNano(), content)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}
