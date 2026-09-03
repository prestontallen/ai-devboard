package serve

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/prestontallen/ai-devboard/worklog/internal/lockfile"
	"github.com/prestontallen/ai-devboard/worklog/internal/yamlx"
)

func corpusServer(t *testing.T) *Server {
	t.Helper()
	return New(Config{
		DataDir:      "testdata/corpus/data",
		WorklogDir:   "testdata/corpus/worklog",
		ScanInterval: time.Second,
	})
}

// normalize applies the golden capture's normalizations: version/generated
// and per-task mtime zeroed, error text (unfrozen) collapsed to "<any>".
func normalize(payload map[string]any) {
	payload["version"] = float64(0)
	payload["generated"] = float64(0)
	repos, _ := payload["repos"].([]any)
	for _, r := range repos {
		repo, _ := r.(map[string]any)
		tasks, _ := repo["tasks"].([]any)
		for _, tv := range tasks {
			task, _ := tv.(map[string]any)
			if _, ok := task["mtime"]; ok {
				task["mtime"] = float64(0)
			}
			if _, ok := task["error"]; ok {
				task["error"] = "<any>"
			}
		}
	}
}

// TestGoldenTasks: the port's /api/tasks is structurally identical to the
// response server.py produced over the same corpus (golden_tasks.json,
// captured via testdata/capture_golden.py).
func TestGoldenTasks(t *testing.T) {
	raw, err := os.ReadFile("testdata/golden_tasks.json")
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]any
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}

	body, err := json.Marshal(corpusServer(t).allTasks())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	normalize(got)

	if !reflect.DeepEqual(got, want) {
		gj, _ := json.MarshalIndent(got, "", " ")
		wj, _ := json.MarshalIndent(want, "", " ")
		t.Errorf("payload diverges from server.py golden\ngot:\n%s\nwant:\n%s", gj, wj)
	}
}

// TestUnknownKeys: keys the schema structs don't know must reach the JSON
// verbatim — the frontend renders them in its "Other" table.
func TestUnknownKeys(t *testing.T) {
	body, _ := json.Marshal(corpusServer(t).allTasks())
	for _, needle := range []string{`"custom_top_level":"hello"`, `"another_unknown"`, `"nested":true`} {
		if !bytes.Contains(body, []byte(needle)) {
			t.Errorf("unknown-key passthrough missing %s", needle)
		}
	}
}

// TestLayout: missing and empty data dirs serve an empty board; feedback and
// repos are [] (never null) so the frontend's iteration doesn't crash.
func TestLayout(t *testing.T) {
	for _, dir := range []string{filepath.Join(t.TempDir(), "missing"), t.TempDir()} {
		s := New(Config{DataDir: dir, WorklogDir: filepath.Join(dir, "nope")})
		body, err := json.Marshal(s.allTasks())
		if err != nil {
			t.Fatal(err)
		}
		for _, needle := range []string{`"repos":[]`, `"feedback":[]`} {
			if !bytes.Contains(body, []byte(needle)) {
				t.Errorf("dir %s: want %s in %s", dir, needle, body)
			}
		}
	}
}

// TestErrorCard: an unparseable file degrades to an error card with no task
// or mtime, and NaN/Inf scalars are sanitized so encoding never fails.
func TestErrorCard(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "r")
	os.MkdirAll(repo, 0o755)
	os.WriteFile(filepath.Join(repo, "bad.yaml"), []byte("a: [unclosed\n"), 0o644)
	os.WriteFile(filepath.Join(repo, "nan.yaml"), []byte("title: n\nf: .nan\ng: .inf\n"), 0o644)
	os.WriteFile(filepath.Join(repo, "scalar.yaml"), []byte("just a string\n"), 0o644)

	s := New(Config{DataDir: dir, WorklogDir: dir})
	body, err := json.Marshal(s.allTasks())
	if err != nil {
		t.Fatalf("NaN/Inf must not break encoding: %v", err)
	}
	var got map[string]any
	json.Unmarshal(body, &got)
	tasks := got["repos"].([]any)[0].(map[string]any)["tasks"].([]any)
	byID := map[string]map[string]any{}
	for _, tv := range tasks {
		task := tv.(map[string]any)
		byID[task["id"].(string)] = task
	}
	for _, id := range []string{"bad", "scalar"} {
		card := byID[id]
		if card["error"] == nil {
			t.Errorf("%s: want error card, got %v", id, card)
		}
		if _, ok := card["task"]; ok {
			t.Errorf("%s: error card must not carry task", id)
		}
		if _, ok := card["mtime"]; ok {
			t.Errorf("%s: error card must not carry mtime", id)
		}
	}
	nan := byID["nan"]["task"].(map[string]any)
	if nan["f"] != ".nan" || nan["g"] != ".inf" {
		t.Errorf("NaN/Inf must sanitize to raw text, got f=%v g=%v", nan["f"], nan["g"])
	}
}

func writeTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	repo := filepath.Join(dir, "alpha")
	os.MkdirAll(filepath.Join(repo, archiveDir), 0o755)
	os.WriteFile(filepath.Join(repo, "live.yaml"), []byte("title: live\n"), 0o644)
	os.WriteFile(filepath.Join(repo, archiveDir, "old.yaml"), []byte("title: old\n"), 0o644)
	return New(Config{DataDir: dir, WorklogDir: dir}), dir
}

func post(t *testing.T, url, ctype, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(url, ctype, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// TestWriteEndpoints: the full status-code surface of the archive endpoints,
// as frozen in devboard/API.md.
func TestWriteEndpoints(t *testing.T) {
	s, dir := writeTestServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	cases := []struct {
		name, path, ctype, body string
		want                    int
	}{
		{"csrf guard", "/api/archive", "text/plain", `{"repo":"alpha","id":"live"}`, 415},
		{"invalid json", "/api/archive", "application/json", `{nope`, 400},
		{"traversal id", "/api/archive", "application/json", `{"repo":"alpha","id":"../x"}`, 400},
		{"empty repo", "/api/archive", "application/json", `{"id":"live"}`, 400},
		{"dot repo", "/api/archive", "application/json", `{"repo":".hidden","id":"live"}`, 400},
		{"missing task", "/api/archive", "application/json", `{"repo":"alpha","id":"ghost"}`, 404},
		{"unknown post", "/api/nope", "application/json", `{}`, 404},
	}
	for _, c := range cases {
		if resp := post(t, ts.URL+c.path, c.ctype, c.body); resp.StatusCode != c.want {
			t.Errorf("%s: got %d want %d", c.name, resp.StatusCode, c.want)
		}
	}

	// GET on a POST endpoint: 405 with a JSON body.
	resp, err := http.Get(ts.URL + "/api/archive")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 405 || resp.Header.Get("Content-Type") != "application/json" {
		t.Errorf("GET archive: got %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}

	// 409: destination already exists.
	os.WriteFile(filepath.Join(dir, "alpha", archiveDir, "live.yaml"), []byte("title: clash\n"), 0o644)
	if resp := post(t, ts.URL+"/api/archive", "application/json", `{"repo":"alpha","id":"live"}`); resp.StatusCode != 409 {
		t.Errorf("conflict: got %d want 409", resp.StatusCode)
	}
	os.Remove(filepath.Join(dir, "alpha", archiveDir, "live.yaml"))

	// Happy path: archive moves the file, bumps the version, returns the body.
	before := s.currentVersion()
	resp = post(t, ts.URL+"/api/archive", "application/json", `{"repo":"alpha","id":"live"}`)
	if resp.StatusCode != 200 {
		t.Fatalf("archive: got %d", resp.StatusCode)
	}
	var moved map[string]string
	json.NewDecoder(resp.Body).Decode(&moved)
	want := map[string]string{"status": "archived", "repo": "alpha", "id": "live"}
	if !reflect.DeepEqual(moved, want) {
		t.Errorf("archive body: got %v want %v", moved, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "alpha", archiveDir, "live.yaml")); err != nil {
		t.Error("file not moved to archive")
	}
	if s.currentVersion() != before+1 {
		t.Error("archive must bump the version synchronously")
	}

	// And back.
	resp = post(t, ts.URL+"/api/unarchive", "application/json", `{"repo":"alpha","id":"live"}`)
	if resp.StatusCode != 200 {
		t.Fatalf("unarchive: got %d", resp.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(dir, "alpha", "live.yaml")); err != nil {
		t.Error("file not restored")
	}
}

// TestArchiveLock: the move blocks on the same .lock devboard.Mutate takes,
// so a concurrent CLI mutation cannot race the rename.
func TestArchiveLock(t *testing.T) {
	s, dir := writeTestServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	release, err := lockfile.Acquire(filepath.Join(dir, "alpha", "live.yaml.lock"))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan int, 1)
	go func() {
		resp, err := http.Post(ts.URL+"/api/archive", "application/json",
			strings.NewReader(`{"repo":"alpha","id":"live"}`))
		if err != nil {
			done <- -1
			return
		}
		defer resp.Body.Close()
		done <- resp.StatusCode
	}()
	select {
	case code := <-done:
		release()
		t.Fatalf("move completed (%d) while the lock was held", code)
	case <-time.After(150 * time.Millisecond):
	}
	release()
	select {
	case code := <-done:
		if code != 200 {
			t.Fatalf("post-release move: got %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("move never completed after lock release")
	}
}

// TestSSE: immediate version event on connect, an event per bump, and a
// keepalive comment on idle.
func TestSSE(t *testing.T) {
	s := corpusServer(t)
	s.keepalive = 200 * time.Millisecond
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type: %s", ct)
	}
	r := bufio.NewReader(resp.Body)
	line := func() string {
		l, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		return strings.TrimRight(l, "\n")
	}
	if got := line(); got != `data: {"version": 0}` {
		t.Fatalf("connect event: %q", got)
	}
	line() // blank separator

	if got := line(); got != ": keepalive" {
		t.Fatalf("keepalive: %q", got)
	}
	line()

	s.bump()
	deadline := time.Now().Add(2 * time.Second)
	for {
		got := line()
		if got == `data: {"version": 1}` {
			break
		}
		if got != ": keepalive" && got != "" {
			t.Fatalf("unexpected event: %q", got)
		}
		if time.Now().After(deadline) {
			t.Fatal("bump event never arrived")
		}
	}
}

// TestWatcher: a task-file edit bumps the version within the scan interval.
func TestWatcher(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "r")
	os.MkdirAll(repo, 0o755)
	target := filepath.Join(repo, "t.yaml")
	os.WriteFile(target, []byte("title: a\n"), 0o644)

	s := New(Config{DataDir: dir, WorklogDir: dir, ScanInterval: 20 * time.Millisecond})
	stop := make(chan struct{})
	defer close(stop)
	go s.Watch(stop)

	time.Sleep(60 * time.Millisecond)
	os.WriteFile(target, []byte("title: changed longer\n"), 0o644)
	deadline := time.Now().Add(2 * time.Second)
	for s.currentVersion() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("watcher never noticed the edit")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestIndexAndRoutes: / and /index.html serve the embedded page; nothing
// else serves content.
func TestIndexAndRoutes(t *testing.T) {
	ts := httptest.NewServer(corpusServer(t).Handler())
	defer ts.Close()

	disk, err := os.ReadFile("static/index.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/index.html"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body := new(bytes.Buffer)
		body.ReadFrom(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || !bytes.Equal(body.Bytes(), disk) {
			t.Errorf("%s: status %d, byte-identical=%v", path, resp.StatusCode, bytes.Equal(body.Bytes(), disk))
		}
		if ct := resp.Header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
			t.Errorf("%s: content-type %s", path, ct)
		}
	}
	for _, path := range []string{"/static/index.html", "/server.py", "/api"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Errorf("%s: got %d want 404", path, resp.StatusCode)
		}
	}
}

// TestConfig: env overrides and native defaults.
func TestConfig(t *testing.T) {
	for _, v := range []string{"DEVBOARD_DATA", "DEVBOARD_WORKLOG", "DEVBOARD_PORT", "DEVBOARD_SCAN_INTERVAL"} {
		t.Setenv(v, "")
		os.Unsetenv(v)
	}
	cfg := ConfigFromEnv()
	home, _ := os.UserHomeDir()
	if cfg.Port != 8484 || cfg.Addr != "0.0.0.0" || cfg.ScanInterval != time.Second {
		t.Errorf("defaults: %+v", cfg)
	}
	if cfg.DataDir != filepath.Join(home, ".local", "share", "devboard") {
		t.Errorf("data default: %s", cfg.DataDir)
	}
	if !strings.HasSuffix(cfg.WorklogDir, filepath.Join(".local", "share", "worklog")) &&
		os.Getenv("XDG_DATA_HOME") == "" {
		t.Errorf("worklog default: %s", cfg.WorklogDir)
	}

	t.Setenv("DEVBOARD_DATA", "/tmp/x")
	t.Setenv("DEVBOARD_WORKLOG", "/tmp/y")
	t.Setenv("DEVBOARD_PORT", "9090")
	t.Setenv("DEVBOARD_SCAN_INTERVAL", "0.5")
	cfg = ConfigFromEnv()
	if cfg.DataDir != "/tmp/x" || cfg.WorklogDir != "/tmp/y" || cfg.Port != 9090 ||
		cfg.ScanInterval != 500*time.Millisecond {
		t.Errorf("env overrides: %+v", cfg)
	}
}

// TestFeedbackParity: the payload's feedback entries keep the shape the old
// server sent — `resolved` present even when 0 (Entry's omitempty must not
// leak through), and the migrated test_server.py payload pins hold.
func TestFeedbackParity(t *testing.T) {
	body, _ := json.Marshal(corpusServer(t).allTasks())
	var got map[string]any
	json.Unmarshal(body, &got)
	fb := got["feedback"].([]any)
	if len(fb) != 2 {
		t.Fatalf("feedback entries: %d", len(fb))
	}
	first := fb[0].(map[string]any)
	if v, ok := first["resolved"]; !ok || v != float64(0) {
		t.Errorf("resolved must be present-with-zero on unresolved entries, got %v (present=%v)", v, ok)
	}
	if first["signal"] != "missing-feature" {
		t.Errorf("signal: %v", first["signal"])
	}
	if strings.Contains(first["excerpt"].(string), "after an unknown field") {
		t.Error("unknown ** field leaked following lines into the excerpt")
	}
}

// TestScalarDates: PyYAML str() parity for the timestamp shapes that occur
// in task files (see contract criterion 2; YAML 1.2 bool divergence is
// TestYAML12Bools).
func TestScalarDates(t *testing.T) {
	cases := []struct {
		yaml string
		want any
	}{
		{`k: 2026-09-01`, "2026-09-01"},
		{`k: 2026-9-1`, "2026-09-01"}, // PyYAML normalizes; so do we
		{`k: "2026-09-01"`, "2026-09-01"},
		{`k: 2026-09-01 19:19:00`, "2026-09-01 19:19:00"},
		{`k: 2026-09-01T19:19:00Z`, "2026-09-01 19:19:00+00:00"},
	}
	for _, c := range cases {
		v, err := yamlx.YAMLToAny([]byte(c.yaml))
		if err != nil {
			t.Fatalf("%s: %v", c.yaml, err)
		}
		got := v.(map[string]any)["k"]
		if got != c.want {
			t.Errorf("%s: got %#v want %#v", c.yaml, got, c.want)
		}
	}
}

// TestYAML12Bools: the accepted divergence — bare yes/no/on/off stay
// strings under YAML 1.2, unlike PyYAML's 1.1 coercion. Documented in
// devboard/API.md; ratified 2026-09-02.
func TestYAML12Bools(t *testing.T) {
	v, err := yamlx.YAMLToAny([]byte("a: yes\nb: no\nc: on\nd: off\ne: true\nf: false\n"))
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	for k, want := range map[string]any{"a": "yes", "b": "no", "c": "on", "d": "off", "e": true, "f": false} {
		if m[k] != want {
			t.Errorf("%s: got %#v want %#v", k, m[k], want)
		}
	}
}

// TestNotesEmbedding: task.worklog pulls the notes file in; unsafe values
// don't escape the notes dir.
func TestNotesEmbedding(t *testing.T) {
	body, _ := json.Marshal(corpusServer(t).allTasks())
	if !bytes.Contains(body, []byte("Body of the note.")) {
		t.Error("notes content missing from payload")
	}

	dir := t.TempDir()
	repo := filepath.Join(dir, "r")
	os.MkdirAll(repo, 0o755)
	os.WriteFile(filepath.Join(repo, "evil.yaml"), []byte("worklog: ../secret\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "notes"), 0o755)
	os.WriteFile(filepath.Join(dir, "secret.md"), []byte("TOPSECRET"), 0o644)
	s := New(Config{DataDir: dir, WorklogDir: dir})
	body, _ = json.Marshal(s.allTasks())
	if bytes.Contains(body, []byte("TOPSECRET")) {
		t.Error("worklog traversal read outside notes/")
	}
}
