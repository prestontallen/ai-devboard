package serve

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prestontallen/ai-devboard/worklog/internal/yamlx"
)

// fixtureServer is a server with a small store-fed board behind it, for
// tests whose subject is routing, assets or transport rather than payload
// content. It reads nothing from disk: the payload comes from the
// injected snapshot, and the data dir exists only for the change watcher.
func fixtureServer(t *testing.T) *Server {
	t.Helper()
	srv := New(Config{
		WorklogDir:   t.TempDir(),
		ScanInterval: time.Second,
	})
	srv.LoadStoreSnapshot = func() (*StoreSnapshot, error) {
		return &StoreSnapshot{Tasks: []StoreTask{task("demo", "a-card", false, 0)}}, nil
	}
	return srv
}

// TestWriteEndpoints: the request-shape half of the archive endpoints, as
// frozen in devboard/API.md. What the store does with a well-formed
// request is move_owner_test.go's subject; this is everything decided
// before the store is asked.
func TestWriteEndpoints(t *testing.T) {
	s, _ := moveServer(t, func(string, string, bool) (bool, error) { return true, nil })
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	for _, c := range []struct {
		name, path, ctype, body string
		want                    int
	}{
		{"csrf guard", "/api/archive", "text/plain", `{"repo":"alpha","id":"live"}`, 415},
		{"invalid json", "/api/archive", "application/json", `{nope`, 400},
		{"traversal id", "/api/archive", "application/json", `{"repo":"alpha","id":"../x"}`, 400},
		{"empty repo", "/api/archive", "application/json", `{"id":"live"}`, 400},
		{"dot repo", "/api/archive", "application/json", `{"repo":".hidden","id":"live"}`, 400},
		{"unknown post", "/api/nope", "application/json", `{}`, 404},
		{"well formed", "/api/archive", "application/json", `{"repo":"alpha","id":"live"}`, 200},
	} {
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

// TestSSE: immediate version event on connect, an event per bump, and a
// keepalive comment on idle.
func TestSSE(t *testing.T) {
	s := fixtureServer(t)
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

// The change stream follows the store now, not a file tree. A fingerprint
// that moves is an event; one that does not is silence, which is the whole
// point — a write that changes nothing used to produce no event because an
// identical render left the file's mtime alone, and that behavior has to
// survive the move to the store.
func TestWatcherFollowsTheStoreFingerprint(t *testing.T) {
	var fp string
	s := New(Config{ScanInterval: 5 * time.Millisecond})
	s.StoreFingerprint = func() (string, error) { return fp, nil }
	stop := make(chan struct{})
	defer close(stop)
	go s.Watch(stop)

	settle := func() { time.Sleep(60 * time.Millisecond) }
	settle()
	if v := s.currentVersion(); v != 0 {
		t.Fatalf("an unchanged fingerprint bumped the version to %d", v)
	}

	fp = "changed"
	deadline := time.Now().Add(2 * time.Second)
	for s.currentVersion() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the watcher never noticed the change")
		}
		time.Sleep(5 * time.Millisecond)
	}
	at := s.currentVersion()

	// Holding still must not keep firing.
	settle()
	if s.currentVersion() != at {
		t.Errorf("the version kept moving with a steady fingerprint: %d -> %d", at, s.currentVersion())
	}
}

// A read failure is not a change. Reporting one would make every open
// board refetch whenever the database was briefly busy.
func TestWatcherTreatsAReadFailureAsNoChange(t *testing.T) {
	s := New(Config{ScanInterval: 5 * time.Millisecond})
	s.StoreFingerprint = func() (string, error) { return "", errors.New("database is busy") }
	stop := make(chan struct{})
	defer close(stop)
	go s.Watch(stop)
	time.Sleep(60 * time.Millisecond)
	if v := s.currentVersion(); v != 0 {
		t.Errorf("a failing fingerprint bumped the version to %d", v)
	}
}

// With no hook wired the watcher exits rather than spinning; the board
// still serves and a dashboard write still bumps directly.
func TestWatcherWithoutAHookStops(t *testing.T) {
	s := New(Config{ScanInterval: time.Millisecond})
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { s.Watch(stop); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		close(stop)
		t.Fatal("Watch did not return with no fingerprint hook")
	}
}

// TestIndexAndRoutes: / and /index.html serve the embedded page; nothing
// else serves content. Since adb-lens-cutover that page is the Lens Board.
func TestIndexAndRoutes(t *testing.T) {
	ts := httptest.NewServer(fixtureServer(t).Handler())
	defer ts.Close()

	disk, err := os.ReadFile("static/app.html")
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
	// The embed widening added /next and /assets/*, so this loop grew rather
	// than shrank. /static/app.html still 404s because assets are rooted at
	// static/assets — the board page is reachable at exactly one URL, and
	// nothing under /assets/ can name it. `/next/` left this list at the
	// cutover: it redirects now, and is asserted in TestNextRedirects.
	for _, path := range []string{
		"/static/index.html", "/static/app.html", "/server.py", "/api",
		"/assets", "/assets/", "/assets/vendor/", "/assets/src/",
		"/assets/nope.js", "/assets/index.html", "/assets/app.html",
		"/nextfoo",
	} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body := new(bytes.Buffer)
		body.ReadFrom(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Errorf("%s: got %d want 404", path, resp.StatusCode)
		}
		// The JSON error shape is frozen by devboard/API.md; a directory
		// listing or a text/plain "404 page not found" would both pass a
		// status-only check.
		if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("%s: content-type %q want application/json", path, ct)
		}
		if got := body.String(); got != `{"error": "not found"}` {
			t.Errorf("%s: body %q", path, got)
		}
	}
}

// TestNextRedirects: /next is a permanent redirect to / since the cutover,
// and the route is kept rather than 404ed so a bookmarked /next#/backlog
// still lands on the right lens — the browser reapplies the fragment
// because the target carries none, which is not something the server can
// do for it.
func TestNextRedirects(t *testing.T) {
	ts := httptest.NewServer(fixtureServer(t).Handler())
	defer ts.Close()

	// The default client follows redirects, which would hide the status.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	for _, path := range []string{"/next", "/next/"} {
		resp, err := client.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body := new(bytes.Buffer)
		body.ReadFrom(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusPermanentRedirect {
			t.Errorf("%s: got %d want %d", path, resp.StatusCode, http.StatusPermanentRedirect)
		}
		if loc := resp.Header.Get("Location"); loc != "/" {
			t.Errorf("%s: Location %q want /", path, loc)
		}
		// A redirect that also served the page would keep two URLs alive for
		// one board, which is the split the cutover exists to end.
		if bytes.Contains(body.Bytes(), []byte("<script type=\"importmap\">")) {
			t.Errorf("%s served the shell instead of redirecting", path)
		}
	}
}

// TestAppShell: / serves the Preact shell, and the import map names every
// vendored specifier. hooks.module.js itself imports bare "preact", so a
// shell that lost the map would fail to boot in the browser while every Go
// test stayed green.
func TestAppShell(t *testing.T) {
	ts := httptest.NewServer(fixtureServer(t).Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body := new(bytes.Buffer)
	body.ReadFrom(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("got %d want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("content-type %q", ct)
	}
	disk, err := os.ReadFile("static/app.html")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body.Bytes(), disk) {
		t.Error("/ is not byte-identical to static/app.html")
	}
	for _, want := range []string{
		`type="importmap"`,
		`"preact": "/assets/vendor/preact.module.js"`,
		`"preact/hooks": "/assets/vendor/hooks.module.js"`,
		`"htm": "/assets/vendor/htm.module.js"`,
		// The shell loads one entry module and pulls the rest in transitively,
		// so this names what /next actually requests — not whichever component
		// happened to be first.
		`/assets/src/app.js`,
	} {
		if !strings.Contains(body.String(), want) {
			t.Errorf("shell missing %q", want)
		}
	}
}

// TestAssetServing: every vendored file is reachable, typed, and identical
// to what is committed on disk.
func TestAssetServing(t *testing.T) {
	ts := httptest.NewServer(fixtureServer(t).Handler())
	defer ts.Close()

	cases := []struct{ url, disk, ctype string }{
		{"/assets/vendor/preact.module.js", "static/assets/vendor/preact.module.js", "text/javascript; charset=utf-8"},
		{"/assets/vendor/hooks.module.js", "static/assets/vendor/hooks.module.js", "text/javascript; charset=utf-8"},
		{"/assets/vendor/htm.module.js", "static/assets/vendor/htm.module.js", "text/javascript; charset=utf-8"},
		{"/assets/vendor/preact.module.js.map", "static/assets/vendor/preact.module.js.map", "application/json"},
		{"/assets/src/chip.js", "static/assets/src/chip.js", "text/javascript; charset=utf-8"},
	}
	for _, c := range cases {
		resp, err := http.Get(ts.URL + c.url)
		if err != nil {
			t.Fatal(err)
		}
		body := new(bytes.Buffer)
		body.ReadFrom(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != 200 {
			t.Errorf("%s: got %d want 200", c.url, resp.StatusCode)
			continue
		}
		if ct := resp.Header.Get("Content-Type"); ct != c.ctype {
			t.Errorf("%s: content-type %q want %q", c.url, ct, c.ctype)
		}
		// API.md freezes "all responses carry Cache-Control: no-store".
		// http.FileServerFS sets none, which is one reason assets are
		// hand-served.
		if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
			t.Errorf("%s: cache-control %q want no-store", c.url, cc)
		}
		disk, err := os.ReadFile(c.disk)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(body.Bytes(), disk) {
			t.Errorf("%s: served bytes differ from %s", c.url, c.disk)
		}
	}
}

// TestAssetTraversal drives the handler directly: an http.Client normalizes
// "/assets/../x" before it ever leaves the process, so going through one
// would test the client rather than the server.
func TestAssetTraversal(t *testing.T) {
	h := fixtureServer(t).Handler()
	for _, path := range []string{
		"/assets/../server.go",
		"/assets/../../go.mod",
		"/assets/%2e%2e/server.go",
		"/assets/..%2fserver.go",
		"/assets/vendor/../../index.html",
		"/assets//etc/passwd",
		"/assets/./vendor/htm.module.js",
		"/assets/vendor/./htm.module.js",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		if w.Code != 404 {
			t.Errorf("%s: got %d want 404", path, w.Code)
		}
		if body := w.Body.String(); body != `{"error": "not found"}` {
			t.Errorf("%s: leaked body %q", path, body)
		}
	}
}

// TestMethodInvariants: the widened surface must not have taught the server
// new methods or stray redirects. http.FileServerFS answers HEAD and
// 301-redirects paths ending in /index.html; the hand-rolled handler does
// neither.
//
// /next is the one deliberate redirect (TestNextRedirects owns it), so it
// stays in the method loop and leaves the redirect loop — asserting "nothing
// redirects" over a route that must redirect would have meant deleting the
// invariant instead of narrowing it.
func TestMethodInvariants(t *testing.T) {
	h := fixtureServer(t).Handler()
	for _, path := range []string{"/next", "/assets/vendor/htm.module.js", "/assets/"} {
		for _, method := range []string{http.MethodHead, http.MethodPut, http.MethodDelete} {
			req := httptest.NewRequest(method, path, nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)

			if method == http.MethodHead {
				// HEAD is not GET and not POST, so it lands in the same
				// 501 arm everything else does — unchanged from before.
				if w.Code != http.StatusNotImplemented {
					t.Errorf("HEAD %s: got %d want 501", path, w.Code)
				}
				continue
			}
			if w.Code != http.StatusNotImplemented {
				t.Errorf("%s %s: got %d want 501", method, path, w.Code)
			}
		}
		// No route redirects except the one that is supposed to.
		if path == "/next" {
			continue
		}
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code >= 300 && w.Code < 400 {
			t.Errorf("GET %s: unexpected redirect %d to %q", path, w.Code, w.Header().Get("Location"))
		}
	}

	// The board page itself must never redirect: FileServerFS's /index.html
	// behaviour is exactly what a hand-rolled handler exists to avoid.
	for _, path := range []string{"/", "/index.html"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK {
			t.Errorf("GET %s: got %d want 200", path, w.Code)
		}
	}
}

// TestEmbeddedManifest is the real guard on the embed pattern. The plain
// //go:embed static form silently skips names beginning with "." or "_",
// and just as silently absorbs anything else that appears under static/ —
// a stray node_modules on a developer's machine included. Pinning the exact
// file list is what turns either accident into a failing test.
func TestEmbeddedManifest(t *testing.T) {
	want := map[string]bool{
		"static/app.html":                           true,
		"static/assets/src/app.js":                  true,
		"static/assets/src/archive.js":              true,
		"static/assets/src/backlog.js":              true,
		"static/assets/src/board.js":                true,
		"static/assets/src/card.js":                 true,
		"static/assets/src/clipboard.js":            true,
		"static/assets/src/chip.js":                 true,
		"static/assets/src/counts.js":               true,
		"static/assets/src/data.js":                 true,
		"static/assets/src/detail.js":               true,
		"static/assets/src/done.js":                 true,
		"static/assets/src/epic.js":                 true,
		"static/assets/src/friction.js":             true,
		"static/assets/src/grid.js":                 true,
		"static/assets/src/ledger.js":               true,
		"static/assets/src/markdown.js":             true,
		"static/assets/src/needs.js":                true,
		"static/assets/src/phases.js":               true,
		"static/assets/src/routes.js":               true,
		"static/assets/src/rows.js":                 true,
		"static/assets/src/sections.js":             true,
		"static/assets/src/waiting.js":              true,
		"static/assets/vendor/README.md":            true,
		"static/assets/vendor/htm.module.js":        true,
		"static/assets/vendor/hooks.module.js":      true,
		"static/assets/vendor/hooks.module.js.map":  true,
		"static/assets/vendor/preact.module.js":     true,
		"static/assets/vendor/preact.module.js.map": true,
	}
	got := map[string]bool{}
	err := fs.WalkDir(staticFS, "static", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			got[p] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for p := range want {
		if !got[p] {
			t.Errorf("embedded FS is missing %s", p)
		}
	}
	for p := range got {
		if !want[p] {
			t.Errorf("embedded FS carries an unexpected file: %s", p)
		}
	}
}

// TestVendorChecksums re-derives the hashes recorded in the vendor README,
// so a hand-edited or drifted vendored file fails offline — the release
// path sha256-verifies its downloads, and vendored bytes deserve the same.
func TestVendorChecksums(t *testing.T) {
	readme, err := os.ReadFile("static/assets/vendor/README.md")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := fs.ReadDir(assetsFS, "vendor")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		body, err := fs.ReadFile(assetsFS, "vendor/"+e.Name())
		if err != nil {
			t.Fatal(err)
		}
		sum := fmt.Sprintf("%x", sha256.Sum256(body))
		if !strings.Contains(string(readme), sum) {
			t.Errorf("%s: sha256 %s is not recorded in the vendor README", e.Name(), sum)
		}
		checked++
	}
	if checked != 5 {
		t.Errorf("checked %d vendored files, want 5", checked)
	}
}

// TestConfig: env overrides and native defaults.
func TestConfig(t *testing.T) {
	for _, v := range []string{"DEVBOARD_WORKLOG", "DEVBOARD_PORT", "DEVBOARD_SCAN_INTERVAL"} {
		t.Setenv(v, "")
		os.Unsetenv(v)
	}
	cfg := ConfigFromEnv()
	if cfg.Port != 8484 || cfg.Addr != "0.0.0.0" || cfg.ScanInterval != time.Second {
		t.Errorf("defaults: %+v", cfg)
	}
	if !strings.HasSuffix(cfg.WorklogDir, filepath.Join(".local", "share", "worklog")) &&
		os.Getenv("XDG_DATA_HOME") == "" {
		t.Errorf("worklog default: %s", cfg.WorklogDir)
	}

	t.Setenv("DEVBOARD_WORKLOG", "/tmp/y")
	t.Setenv("DEVBOARD_PORT", "9090")
	t.Setenv("DEVBOARD_SCAN_INTERVAL", "0.5")
	cfg = ConfigFromEnv()
	if cfg.WorklogDir != "/tmp/y" || cfg.Port != 9090 ||
		cfg.ScanInterval != 500*time.Millisecond {
		t.Errorf("env overrides: %+v", cfg)
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

const backlogWorkMD = `# Worklog — active

## Now

- [~] **STARTED** — Already in flight
  - **ID**: started
  - **Repo**: r

## Next

- [ ] **NOLE-DOCKER-NET** — Fix Docker container internet access
  - **ID**: nole-docker-net
  - **Repo**: prestontallen/nole
  - **Tags**: nole, docker
  - **Acceptance**: ollama pull succeeds inside the container

- [ ] **AN-EPIC** — A cross-cutting effort
  - **ID**: an-epic
  - **Type**: epic
  - **Repo**: r

## Someday

- [ ] **LATER** — Something for later
  - **ID**: later
`
