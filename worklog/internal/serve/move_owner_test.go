package serve

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Archiving is a pure store write. The handler used to rename the file and
// then tell the store, throwing the store's error away: a failed sync
// answered 200 with an empty log and left disk and store disagreeing,
// after which every later CLI write refused (adb-archive-store-desync).
// It then told the store first and renamed only what the store disowned.
// Now it only tells the store, because the store owns where a board task
// renders and the re-render is what places the file.

// moveServer wires a stub hook and records what it was asked to do. The
// data dir is a path that does not exist: nothing in this handler may read
// one any more.
func moveServer(t *testing.T, hook func(repo, id string, archived bool) (bool, error)) (*Server, *[]string) {
	t.Helper()
	var calls []string
	s := New(Config{
		WorklogDir: t.TempDir(),
	})
	s.MutateBoard = func(repo, id string, archived bool) (bool, error) {
		verb := "unarchive"
		if archived {
			verb = "archive"
		}
		calls = append(calls, verb+" "+repo+"/"+id)
		return hook(repo, id, archived)
	}
	return s, &calls
}

func moveResp(t *testing.T, s *Server, path, body string) (int, string) {
	t.Helper()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	resp := post(t, ts.URL+path, "application/json", body)
	defer resp.Body.Close()
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.String()
}

// The happy path in both directions: the store is told exactly once, with
// the right direction, and the version bumps synchronously so an open
// board redraws without waiting out the scan interval.
func TestArchiveAndUnarchiveAreStoreWrites(t *testing.T) {
	for _, c := range []struct{ path, want string }{
		{"/api/archive", "archive alpha/live"},
		{"/api/unarchive", "unarchive alpha/live"},
	} {
		t.Run(c.path, func(t *testing.T) {
			s, calls := moveServer(t, func(string, string, bool) (bool, error) { return true, nil })
			before := s.currentVersion()
			code, body := moveResp(t, s, c.path, `{"repo":"alpha","id":"live"}`)
			if code != 200 {
				t.Fatalf("status %d: %s", code, body)
			}
			var got map[string]string
			json.Unmarshal([]byte(body), &got)
			status := "archived"
			if c.path == "/api/unarchive" {
				status = "restored"
			}
			if got["status"] != status || got["repo"] != "alpha" || got["id"] != "live" {
				t.Errorf("body = %v", got)
			}
			if len(*calls) != 1 || (*calls)[0] != c.want {
				t.Errorf("store calls = %v, want one %q", *calls, c.want)
			}
			if s.currentVersion() != before+1 {
				t.Error("a move must bump the version synchronously")
			}
		})
	}
}

// An id the store has no board-tracked ticket for is a 404. Two shapes
// reach it: no ticket at all, and a ticket that exists but is not
// board-tracked. The second is the subtler one — it resolves, so setting a
// field on it would look like success while no card ever existed to move.
// Both answer the same, because from the board's side neither has a card.
func TestUnownedIdIsNotFound(t *testing.T) {
	s, calls := moveServer(t, func(string, string, bool) (bool, error) { return false, nil })
	code, body := moveResp(t, s, "/api/archive", `{"repo":"alpha","id":"ghost"}`)
	if code != 404 {
		t.Fatalf("status %d, want 404: %s", code, body)
	}
	if !strings.Contains(body, "task not found") {
		t.Errorf("body = %s", body)
	}
	if len(*calls) != 1 {
		t.Errorf("the store should still have been asked once, got %v", *calls)
	}
}

// A failing store write is a failure: no 200, the cause in the body and in
// the log, and nothing changed.
func TestStoreFailureIsSurfaced(t *testing.T) {
	s, _ := moveServer(t, func(string, string, bool) (bool, error) {
		return false, errors.New("store is sad")
	})
	var logged bytes.Buffer
	log.SetOutput(&logged)
	defer log.SetOutput(os.Stderr)

	code, body := moveResp(t, s, "/api/archive", `{"repo":"alpha","id":"live"}`)
	if code != 500 {
		t.Errorf("status %d, want 500 — a silent 200 is the bug this replaced", code)
	}
	if !strings.Contains(body, "store is sad") {
		t.Errorf("the response hid the cause: %s", body)
	}
	if !strings.Contains(logged.String(), "store is sad") {
		t.Errorf("the failure was not logged: %q", logged.String())
	}
}

// While the corpus is frozen for adoption, a dashboard click must not write
// through the conversion. serve is freeze-exempt as a process — the
// sentinel is checked once at CLI start and this handler runs per request,
// long after — so it checks for itself. Adoption on live data has had to
// stop the service by hand until now.
func TestFrozenCorpusRefusesTheWrite(t *testing.T) {
	s, calls := moveServer(t, func(string, string, bool) (bool, error) {
		t.Error("the store was written while the corpus was frozen")
		return true, nil
	})
	if err := os.WriteFile(filepath.Join(s.cfg.WorklogDir, ".freeze"),
		[]byte(`{"pid":1,"reason":"adopt"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	code, body := moveResp(t, s, "/api/archive", `{"repo":"alpha","id":"live"}`)
	if code != 503 {
		t.Fatalf("status %d, want 503: %s", code, body)
	}
	if !strings.Contains(body, "frozen") {
		t.Errorf("body = %s", body)
	}
	if len(*calls) != 0 {
		t.Errorf("the store was asked anyway: %v", *calls)
	}
}

// A corrupt or unreadable sentinel fails safe as frozen rather than as
// open, matching freeze.Check's own rule.
func TestUnreadableFreezeSentinelRefusesTheWrite(t *testing.T) {
	s, calls := moveServer(t, func(string, string, bool) (bool, error) { return true, nil })
	if err := os.WriteFile(filepath.Join(s.cfg.WorklogDir, ".freeze"),
		[]byte("not json at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _ := moveResp(t, s, "/api/archive", `{"repo":"alpha","id":"live"}`); code != 503 {
		t.Errorf("status %d, want 503 — a sentinel that cannot be read must not read as open", code)
	}
	if len(*calls) != 0 {
		t.Errorf("the store was asked anyway: %v", *calls)
	}
}

// A server built with no hook cannot write. That is a wiring fault, not a
// client error, so it is a 500 and it says so rather than answering 200
// having done nothing.
func TestMissingHookIsAServerError(t *testing.T) {
	s := New(Config{WorklogDir: t.TempDir()})
	code, body := moveResp(t, s, "/api/archive", `{"repo":"alpha","id":"live"}`)
	if code != 500 {
		t.Fatalf("status %d, want 500: %s", code, body)
	}
	if !strings.Contains(body, "not wired") {
		t.Errorf("body = %s", body)
	}
}
