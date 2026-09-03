// Package serve is the devboard dashboard server: the Go port of the retired
// devboard/server.py, behavior-frozen against the /api/tasks contract
// (devboard/API.md). Layout: <DataDir>/<repo>/<task>.{yaml,yml,json}, with
// <repo>/_archive/ for archived tasks. The two POST endpoints are the only
// writes, and both are a single validated rename under the same .lock that
// devboard.Mutate takes — the server never writes under the worklog dir.
package serve

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prestontallen/ai-devboard/worklog/internal/devboard"
	"github.com/prestontallen/ai-devboard/worklog/internal/lockfile"
)

//go:embed static/index.html
var indexHTML []byte

// Config carries the server's runtime settings, sourced from the same
// DEVBOARD_* env vars the Python server honored, with native defaults
// replacing the container paths.
type Config struct {
	DataDir      string
	WorklogDir   string
	Addr         string
	Port         int
	ScanInterval time.Duration
}

// ConfigFromEnv resolves DEVBOARD_DATA, DEVBOARD_WORKLOG, DEVBOARD_PORT and
// DEVBOARD_SCAN_INTERVAL, defaulting to the native data dirs and 0.0.0.0:8484
// (the board is used over LAN).
func ConfigFromEnv() Config {
	cfg := Config{
		DataDir:      devboard.DataDir(),
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

	// AfterWrite, if set, runs after a successful archive/unarchive move —
	// adb-cutover M2's shadow-sync hook, injected by the CLI layer rather
	// than imported directly (internal/verify already imports this package
	// for board comparison, so a direct import here would cycle).
	AfterWrite func()
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

type fileSig struct {
	mtimeNS int64
	size    int64
}

// snapshot maps watched-file path -> (mtime, size): task files (live and
// archived), worklog notes and FEEDBACK.md, so note edits and friction
// changes hot-reload too.
func (s *Server) snapshot() map[string]fileSig {
	snap := make(map[string]fileSig)
	addDir := func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if !e.Type().IsRegular() || !hasTaskExt(e.Name()) {
				continue
			}
			if info, err := e.Info(); err == nil {
				snap[filepath.Join(dir, e.Name())] = fileSig{info.ModTime().UnixNano(), info.Size()}
			}
		}
	}
	if repos, err := os.ReadDir(s.cfg.DataDir); err == nil {
		for _, repo := range repos {
			if !repo.IsDir() || strings.HasPrefix(repo.Name(), ".") {
				continue
			}
			addDir(filepath.Join(s.cfg.DataDir, repo.Name()))
			addDir(filepath.Join(s.cfg.DataDir, repo.Name(), archiveDir))
		}
	}
	fpath := filepath.Join(s.cfg.WorklogDir, "FEEDBACK.md")
	if info, err := os.Stat(fpath); err == nil {
		snap[fpath] = fileSig{info.ModTime().UnixNano(), info.Size()}
	}
	if notes, err := os.ReadDir(filepath.Join(s.cfg.WorklogDir, "notes")); err == nil {
		for _, e := range notes {
			if !e.Type().IsRegular() || !strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
				continue
			}
			if info, err := e.Info(); err == nil {
				p := filepath.Join(s.cfg.WorklogDir, "notes", e.Name())
				snap[p] = fileSig{info.ModTime().UnixNano(), info.Size()}
			}
		}
	}
	return snap
}

// Watch polls for changes until stop is closed. Run it in a goroutine.
func (s *Server) Watch(stop <-chan struct{}) {
	prev := s.snapshot()
	ticker := time.NewTicker(s.cfg.ScanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			cur := s.snapshot()
			if !sigsEqual(prev, cur) {
				prev = cur
				s.bump()
			}
		}
	}
}

func sigsEqual(a, b map[string]fileSig) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

// Handler returns the full route surface: /, /index.html, /api/tasks,
// /events, /api/archive, /api/unarchive — everything else 404s.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch r.Method {
		case http.MethodGet:
			switch path {
			case "/", "/index.html":
				s.send(w, http.StatusOK, indexHTML, "text/html; charset=utf-8")
			case "/api/tasks":
				body, err := json.Marshal(s.allTasks())
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
				s.send(w, http.StatusNotFound, []byte(`{"error": "not found"}`), "application/json")
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

// move renames a task file into or out of <repo>/_archive/ — the server's
// only write. The strict application/json requirement doubles as the CSRF
// guard: cross-origin JSON needs a preflight this server never answers.
// The rename runs under the live task path's .lock — the same lock
// devboard.Mutate takes — so a concurrent CLI mutation can't race the move.
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
	for _, part := range []string{body.Repo, body.ID} {
		if part == "" || strings.HasPrefix(part, ".") || strings.Contains(part, "..") ||
			strings.ContainsAny(part, `/\`) {
			s.send(w, http.StatusBadRequest, []byte(`{"error": "invalid repo or id"}`), "application/json")
			return
		}
	}
	repoDir := filepath.Join(s.cfg.DataDir, body.Repo)
	arcDir := filepath.Join(repoDir, archiveDir)
	srcDir, dstDir := repoDir, arcDir
	if !toArchive {
		srcDir, dstDir = arcDir, repoDir
	}
	var fname string
	for _, f := range sortedTaskFiles(srcDir) {
		if strings.TrimSuffix(f, filepath.Ext(f)) == body.ID {
			fname = f
			break
		}
	}
	if fname == "" {
		s.send(w, http.StatusNotFound, []byte(`{"error": "task not found"}`), "application/json")
		return
	}

	unlock, err := lockfile.Acquire(filepath.Join(repoDir, fname) + ".lock")
	if err != nil {
		s.sendMoveErr(w, err)
		return
	}
	defer unlock()

	src, dst := filepath.Join(srcDir, fname), filepath.Join(dstDir, fname)
	// Re-check under the lock: a concurrent mutation may have moved either side.
	if _, err := os.Stat(src); err != nil {
		s.send(w, http.StatusNotFound, []byte(`{"error": "task not found"}`), "application/json")
		return
	}
	if _, err := os.Stat(dst); err == nil {
		s.send(w, http.StatusConflict, []byte(`{"error": "destination already exists"}`), "application/json")
		return
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		s.sendMoveErr(w, err)
		return
	}
	if err := os.Rename(src, dst); err != nil {
		s.sendMoveErr(w, err)
		return
	}
	s.bump() // wake SSE clients now; don't wait out the scan interval
	if s.AfterWrite != nil {
		s.AfterWrite()
	}

	status := "restored"
	if toArchive {
		status = "archived"
	}
	resp, _ := json.Marshal(map[string]string{"status": status, "repo": body.Repo, "id": body.ID})
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
	fmt.Printf("devboard: serving %s on http://%s\n", s.cfg.DataDir, addr)
	return http.ListenAndServe(addr, s.Handler())
}
