// Package serve is the devboard dashboard server: the Go port of the retired
// devboard/server.py, behavior-frozen against the /api/tasks contract
// (devboard/API.md).
//
// It reads and writes the worklog store, through three hooks the CLI layer
// injects, and touches no file of its own: the payload is built from
// tickets, the change stream is a store fingerprint, and the two POST
// endpoints record an archive on a ticket. The rendered YAML tree it used
// to walk is now output nothing here consumes
// (adb-serve-store-direct).
package serve

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prestontallen/ai-devboard/worklog/internal/freeze"
)

// staticFS carries every front-end byte the binary serves. The plain
// (non-"all:") pattern silently skips names beginning with "." or "_", so
// the guard that matters is TestEmbeddedManifest, which asserts the tree
// holds exactly the expected files — not the pattern itself.
//
//go:embed static
var staticFS embed.FS

// The page is read out of staticFS at init rather than carrying its own
// //go:embed directive, so it is not embedded twice. There was a second
// page here until adb-lens-cutover retired the outgoing board.
var (
	appHTML = mustEmbed("static/app.html")
	// assetsFS is rooted at static/assets, so no URL under /assets/ can
	// name the page — /assets/app.html has nothing to resolve to, which is
	// what keeps the board reachable at exactly one URL.
	assetsFS = mustSub("static/assets")
)

func mustEmbed(name string) []byte {
	b, err := staticFS.ReadFile(name)
	if err != nil {
		panic("serve: embedded " + name + " missing: " + err.Error())
	}
	return b
}

func mustSub(dir string) fs.FS {
	sub, err := fs.Sub(staticFS, dir)
	if err != nil {
		panic("serve: embedded " + dir + " missing: " + err.Error())
	}
	return sub
}

// Config carries the server's runtime settings, sourced from the same
// DEVBOARD_* env vars the Python server honored, with native defaults
// replacing the container paths.
type Config struct {
	WorklogDir   string
	Addr         string
	Port         int
	ScanInterval time.Duration
}

// ConfigFromEnv resolves DEVBOARD_WORKLOG, DEVBOARD_PORT and
// DEVBOARD_SCAN_INTERVAL, defaulting to the native worklog dir and
// 0.0.0.0:8484 (the board is used over LAN).
//
// DEVBOARD_DATA is gone: the server no longer has a data directory to
// point at.
func ConfigFromEnv() Config {
	cfg := Config{
		WorklogDir:   defaultWorklogDir(),
		Addr:         "0.0.0.0",
		Port:         8484,
		ScanInterval: time.Second,
	}
	if d := os.Getenv("DEVBOARD_WORKLOG"); d != "" {
		cfg.WorklogDir = d
	}
	if p := os.Getenv("DEVBOARD_PORT"); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			cfg.Port = n
		}
	}
	if s := os.Getenv("DEVBOARD_SCAN_INTERVAL"); s != "" {
		if f, err := strconv.ParseFloat(s, 64); err == nil && f > 0 {
			cfg.ScanInterval = time.Duration(f * float64(time.Second))
		}
	}
	return cfg
}

func defaultWorklogDir() string {
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "worklog")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "worklog")
}

// Server watches the data dir and serves the board. Change notification is a
// version counter plus a channel closed on every bump.
type Server struct {
	cfg       Config
	keepalive time.Duration // SSE idle comment interval; a field so tests can shrink it
	mu        sync.Mutex
	ver       int64
	notify    chan struct{}

	// MutateBoard, if set, records an archive/unarchive in the store, which
	// moves the file as a side effect: the store decides whether a board
	// task renders live or under _archive/, so a re-render puts it in the
	// right place and clears the other. Injected by the CLI layer rather
	// than imported directly. The original cycle was through
	// internal/verify, now deleted; the seam stays because
	// internal/projection's tests import this package, so importing
	// projection from here would cycle in the test binary.
	//
	// It reports whether the store owned the move. A file the store does
	// not board-track is not the store's to place — hand-dropped producer
	// files are supported input and have no ticket behind them — so the
	// handler renames those itself.
	//
	// NOT best-effort. It used to be, on the reasoning that the file had
	// already moved and the next write's hand-edit guard would surface any
	// disagreement. It did surface it: by refusing every subsequent write,
	// after a 200 and an empty log. An error here now fails the request
	// with the file untouched (adb-archive-store-desync).
	MutateBoard func(repo, id string, archived bool) (owned bool, err error)

	// StoreFingerprint is called once per scan interval and answers
	// "has anything the board draws changed since last time". Injected by
	// the CLI layer for the same reasons as the two hooks above.
	//
	// Cheap is the whole requirement: this runs every second forever. The
	// CLI's implementation stats the database files as a gate and only
	// reads and hashes when that moves, because a full read of the live
	// store measured 80ms and a stat measures nothing. The gate alone will
	// not do, since a write that changes no value still rewrites rows and
	// moves the file.
	StoreFingerprint func() (string, error)

	// LoadStoreSnapshot is called once per /api/tasks request for
	// everything the payload needs. Injected by the CLI layer for the same
	// cycle reason as MutateBoard, and because the composition root is the
	// one place that constructs a store (adb-store-boundary).
	//
	// Two obligations outlive the shadow that introduced them. It must
	// open and close the store within the call: a held handle stops the
	// WAL checkpointing, and `store relocate` then refuses forever. And a
	// (nil, nil) return means "no store on this machine", which must be
	// answered without having created the database — sqlitestore.Open
	// creates and migrates, and an empty store minted by a board read
	// makes every CLI write refuse from then on.
	LoadStoreSnapshot func() (*StoreSnapshot, error)
}

func New(cfg Config) *Server {
	return &Server{cfg: cfg, keepalive: 15 * time.Second, notify: make(chan struct{})}
}

func (s *Server) currentVersion() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ver
}

// bump increments the version and wakes every SSE client.
func (s *Server) bump() {
	s.mu.Lock()
	s.ver++
	close(s.notify)
	s.notify = make(chan struct{})
	s.mu.Unlock()
}

func (s *Server) versionAndNotify() (int64, chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ver, s.notify
}

// Watch polls for changes until stop is closed. Run it in a goroutine.
//
// The signal is the store's, not the file tree's. It used to be a stat of
// every rendered board file plus notes/ and FEEDBACK.md, which worked only
// because every CLI write rendered those files; the store is the source
// now, and the projection is disposable output that a later ticket deletes
// outright.
//
// StoreFingerprint answers "has anything the board draws changed", and the
// contract it has to meet is that a write which changes nothing produces
// no event. The file watcher got that for free, because an identical
// render was skipped and the mtime never moved.
func (s *Server) Watch(stop <-chan struct{}) {
	if s.StoreFingerprint == nil {
		return // nothing to watch; the board still serves and still bumps on writes
	}
	prev, _ := s.StoreFingerprint()
	ticker := time.NewTicker(s.cfg.ScanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			cur, err := s.StoreFingerprint()
			if err != nil {
				// A transient read failure is not a change. Reporting one
				// would make every client refetch on a busy database.
				continue
			}
			if cur != prev {
				prev = cur
				s.bump()
			}
		}
	}
}

// Handler returns the full route surface: /, /index.html, /next,
// /assets/*, /api/tasks, /events, /api/archive, /api/unarchive —
// everything else 404s.
//
// Since adb-lens-cutover, / serves the Lens Board and /next redirects to
// it. The redirect is permanent, but the route is kept rather than 404ed:
// a fragment never reaches the server, so the only way a bookmarked
// /next#/backlog keeps working is for the browser to reapply the fragment
// to a target that carries none. 404ing would have broken every saved
// link silently.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch r.Method {
		case http.MethodGet:
			switch path {
			case "/", "/index.html":
				s.send(w, http.StatusOK, appHTML, "text/html; charset=utf-8")
			case "/next", "/next/":
				http.Redirect(w, r, "/", http.StatusPermanentRedirect)
			case "/api/tasks":
				payload, err := s.tasksPayload()
				if err != nil {
					log.Printf("devboard: reading the store failed: %v", err)
					s.send(w, http.StatusInternalServerError,
						[]byte(`{"error": "store read failed"}`), "application/json")
					return
				}
				body, err := json.Marshal(payload)
				if err != nil {
					s.send(w, http.StatusInternalServerError,
						[]byte(`{"error": "encoding failed"}`), "application/json")
					return
				}
				s.send(w, http.StatusOK, body, "application/json")
			case "/events":
				s.sse(w, r)
			case "/api/archive", "/api/unarchive":
				s.send(w, http.StatusMethodNotAllowed, []byte(`{"error": "POST only"}`), "application/json")
			default:
				if strings.HasPrefix(path, "/assets/") {
					s.asset(w, path)
					return
				}
				s.notFound(w)
			}
		case http.MethodPost:
			switch path {
			case "/api/archive":
				s.move(w, r, true)
			case "/api/unarchive":
				s.move(w, r, false)
			default:
				s.send(w, http.StatusNotFound, []byte(`{"error": "not found"}`), "application/json")
			}
		default:
			s.send(w, http.StatusNotImplemented, []byte(`{"error": "unsupported method"}`), "application/json")
		}
	})
}

func (s *Server) send(w http.ResponseWriter, code int, body []byte, ctype string) {
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	w.Write(body)
}

func (s *Server) notFound(w http.ResponseWriter) {
	s.send(w, http.StatusNotFound, []byte(`{"error": "not found"}`), "application/json")
}

// asset serves one embedded file from static/assets.
//
// It is hand-rolled rather than delegating to http.FileServerFS because that
// helper breaks four things devboard/API.md freezes: it 404s in text/plain
// instead of the JSON error shape, sets no Cache-Control, serves a directory
// listing for any directory without an index.html, and 301-redirects request
// paths ending in /index.html. Routing through send instead keeps every
// documented invariant true for free.
//
// Only embedded bytes are reachable — there is no disk fallback and no
// user-supplied path reaches the filesystem.
func (s *Server) asset(w http.ResponseWriter, urlPath string) {
	name := strings.TrimPrefix(urlPath, "/assets/")
	// Anything non-canonical is refused rather than normalized, so a
	// traversal attempt can never resolve to a file it cleans down to.
	// "" and a trailing slash both name a directory.
	if name == "" || strings.HasSuffix(name, "/") || path.Clean(name) != name ||
		path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") {
		s.notFound(w)
		return
	}
	// ReadFile fails on a directory, so directories 404 rather than list.
	body, err := fs.ReadFile(assetsFS, name)
	if err != nil {
		s.notFound(w)
		return
	}
	s.send(w, http.StatusOK, body, assetType(name))
}

// assetType maps the extensions actually present under static/assets. Go's
// mime package does not register .map, and its .js answer varies by system
// mime.types, so the table is explicit rather than inherited.
func assetType(name string) string {
	switch path.Ext(name) {
	case ".js":
		return "text/javascript; charset=utf-8"
	case ".map", ".json":
		return "application/json"
	case ".css":
		return "text/css; charset=utf-8"
	case ".html":
		return "text/html; charset=utf-8"
	case ".md":
		return "text/markdown; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	default:
		return "application/octet-stream"
	}
}

// move records an archive or unarchive in the store — the server's only
// write, and now a pure one: it moves no file, takes no lock, and reads
// no directory.
//
// The strict application/json requirement doubles as the CSRF guard:
// cross-origin JSON needs a preflight this server never answers.
//
// The store decides the outcome, because it owns where a board task
// renders: setting the flag IS the move, and the re-render places the
// file. The rename this handler used to perform was a second writer
// racing that projection, which is what left disk and store disagreeing
// and made every later CLI write refuse (adb-archive-store-desync). The
// `.lock` it took coordinated only server requests with each other, never
// with the CLI, which moved onto SQLite's own locking at cutover; the
// store's write gate now serializes both.
func (s *Server) move(w http.ResponseWriter, r *http.Request, toArchive bool) {
	ctype := strings.ToLower(strings.TrimSpace(strings.SplitN(r.Header.Get("Content-Type"), ";", 2)[0]))
	if ctype != "application/json" {
		s.send(w, http.StatusUnsupportedMediaType,
			[]byte(`{"error": "Content-Type must be application/json"}`), "application/json")
		return
	}
	var body struct {
		Repo string `json:"repo"`
		ID   string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.send(w, http.StatusBadRequest, []byte(`{"error": "invalid JSON body"}`), "application/json")
		return
	}
	// Neither value reaches a filesystem path any more. The shape check
	// stays because it is frozen wire behavior and because a slug that
	// looks like a path is a malformed request whichever way it is used.
	for _, part := range []string{body.Repo, body.ID} {
		if part == "" || strings.HasPrefix(part, ".") || strings.Contains(part, "..") ||
			strings.ContainsAny(part, `/\`) {
			s.send(w, http.StatusBadRequest, []byte(`{"error": "invalid repo or id"}`), "application/json")
			return
		}
	}

	// Adoption's freeze stops CLI writes by a sentinel checked once at
	// process start. serve is exempt as a process and this handler runs
	// per request, long afterwards — so a dashboard click could write
	// straight through a conversion that was mid-flight. It checks for
	// itself instead. An unreadable sentinel fails safe as frozen.
	if frozen, _, err := freeze.Check(s.cfg.WorklogDir); frozen || err != nil {
		s.send(w, http.StatusServiceUnavailable,
			[]byte(`{"error": "the worklog is frozen for adoption; try again when it finishes"}`),
			"application/json")
		return
	}

	if s.MutateBoard == nil {
		log.Print("devboard: a board write arrived with no store hook wired")
		s.send(w, http.StatusInternalServerError,
			[]byte(`{"error": "board writes are not wired"}`), "application/json")
		return
	}
	owned, err := s.MutateBoard(body.Repo, body.ID, toArchive)
	if err != nil {
		log.Printf("devboard: recording %s of %s/%s failed, nothing changed: %v",
			moveVerb(toArchive), body.Repo, body.ID, err)
		s.sendMoveErr(w, err)
		return
	}
	// Not owned means the store has no board-tracked ticket under that id.
	// From the board's side that is the same as not existing: it has no
	// card, so there is nothing to archive.
	if !owned {
		s.send(w, http.StatusNotFound, []byte(`{"error": "task not found"}`), "application/json")
		return
	}
	s.bump() // wake SSE clients now; don't wait out the scan interval
	s.sendMoveOK(w, body.Repo, body.ID, toArchive)
}

func moveVerb(toArchive bool) string {
	if toArchive {
		return "archive"
	}
	return "unarchive"
}

func (s *Server) sendMoveOK(w http.ResponseWriter, repo, id string, toArchive bool) {
	status := "restored"
	if toArchive {
		status = "archived"
	}
	resp, _ := json.Marshal(map[string]string{"status": status, "repo": repo, "id": id})
	s.send(w, http.StatusOK, resp, "application/json")
}

func (s *Server) sendMoveErr(w http.ResponseWriter, err error) {
	resp, _ := json.Marshal(map[string]string{"error": fmt.Sprintf("move failed: %v", err)})
	s.send(w, http.StatusInternalServerError, resp, "application/json")
}

// sse streams version bumps: one event immediately on connect, one per
// change, and a keepalive comment every 15s of idle.
func (s *Server) sse(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.send(w, http.StatusInternalServerError, []byte(`{"error": "streaming unsupported"}`), "application/json")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)

	last, notify := s.versionAndNotify()
	fmt.Fprintf(w, "data: {\"version\": %d}\n\n", last)
	flusher.Flush()

	keepalive := time.NewTimer(s.keepalive)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-notify:
			var cur int64
			cur, notify = s.versionAndNotify()
			if cur != last {
				last = cur
				fmt.Fprintf(w, "data: {\"version\": %d}\n\n", cur)
				flusher.Flush()
			}
		case <-keepalive.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
		if !keepalive.Stop() {
			select {
			case <-keepalive.C:
			default:
			}
		}
		keepalive.Reset(s.keepalive)
	}
}

// ListenAndServe starts the watcher and serves until the listener fails.
func (s *Server) ListenAndServe() error {
	stop := make(chan struct{})
	defer close(stop)
	go s.Watch(stop)
	addr := fmt.Sprintf("%s:%d", s.cfg.Addr, s.cfg.Port)
	fmt.Printf("devboard: serving the worklog at %s on http://%s\n", s.cfg.WorklogDir, addr)
	return http.ListenAndServe(addr, s.Handler())
}
