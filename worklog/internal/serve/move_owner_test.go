package serve

import (
	"bytes"
	"errors"
	"log"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The move handler used to rename the file and then tell the store, throwing
// the store's error away. A failed sync therefore answered 200 with an empty
// log and left disk and store disagreeing, which made every later CLI write
// refuse (adb-archive-store-desync). These pin the three paths it now has.

func livePath(dir, repo, id string) string { return filepath.Join(dir, repo, id+".yaml") }
func arcPath(dir, repo, id string) string  { return filepath.Join(dir, repo, "_archive", id+".yaml") }
func gone(t *testing.T, p string) bool     { t.Helper(); _, err := os.Stat(p); return os.IsNotExist(err) }
func present(t *testing.T, p string) bool  { t.Helper(); _, err := os.Stat(p); return err == nil }

// TestMoveStoreOwnedDoesNotRename: when the store owns the task, the handler
// must not touch the file. The re-render inside the hook is what moves it, so
// a rename here would be a second writer racing the projection — the fault
// this ticket exists to remove.
func TestMoveStoreOwnedDoesNotRename(t *testing.T) {
	s, dir := writeTestServer(t)
	var calls []string
	s.MutateBoard = func(repo, id string, archived bool) (bool, error) {
		calls = append(calls, id)
		// Stand in for the projection: the store places the file.
		os.MkdirAll(filepath.Dir(arcPath(dir, repo, id)), 0o755)
		body, _ := os.ReadFile(livePath(dir, repo, id))
		os.WriteFile(arcPath(dir, repo, id), body, 0o644)
		os.Remove(livePath(dir, repo, id))
		return true, nil
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp := post(t, ts.URL+"/api/archive", "application/json", `{"repo":"alpha","id":"live"}`)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("got %d want 200", resp.StatusCode)
	}
	if len(calls) != 1 {
		t.Fatalf("store hook called %d times, want 1", len(calls))
	}
	if !present(t, arcPath(dir, "alpha", "live")) || !gone(t, livePath(dir, "alpha", "live")) {
		t.Error("the task did not end up archived exactly once")
	}
}

// TestMoveUnownedStillRenames: a hand-dropped producer file has no ticket, so
// the projection will never place it. Renaming is the only archive mechanism
// it has, and devboard/README.md calls those files supported.
func TestMoveUnownedStillRenames(t *testing.T) {
	s, dir := writeTestServer(t)
	s.MutateBoard = func(string, string, bool) (bool, error) { return false, nil }
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp := post(t, ts.URL+"/api/archive", "application/json", `{"repo":"alpha","id":"live"}`)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("got %d want 200", resp.StatusCode)
	}
	if !present(t, arcPath(dir, "alpha", "live")) || !gone(t, livePath(dir, "alpha", "live")) {
		t.Error("an unowned file must still be moved by the handler")
	}
}

// TestMoveSurfacesStoreFailure is the heart of it: a failing store write must
// not produce a half-applied move, and must not be answered with 200.
func TestMoveSurfacesStoreFailure(t *testing.T) {
	s, dir := writeTestServer(t)
	s.MutateBoard = func(string, string, bool) (bool, error) {
		return false, errors.New("store is sad")
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	var logged bytes.Buffer
	log.SetOutput(&logged)
	defer log.SetOutput(os.Stderr)

	resp := post(t, ts.URL+"/api/archive", "application/json", `{"repo":"alpha","id":"live"}`)
	defer resp.Body.Close()
	body := new(bytes.Buffer)
	body.ReadFrom(resp.Body)

	if resp.StatusCode < 400 {
		t.Errorf("got %d, want a failure status — a silent 200 is the bug", resp.StatusCode)
	}
	if !strings.Contains(body.String(), "store is sad") {
		t.Errorf("the response hid the cause: %s", body.String())
	}
	if !strings.Contains(logged.String(), "store is sad") {
		t.Errorf("the failure was not logged: %q", logged.String())
	}
	// The file must be exactly where it started.
	if !present(t, livePath(dir, "alpha", "live")) {
		t.Error("the file moved despite the failure — a half-applied move")
	}
	if present(t, arcPath(dir, "alpha", "live")) {
		t.Error("a copy was left in _archive/ after a failed move")
	}
}

// With no hook wired at all (a bare server, as the tests elsewhere build),
// the handler keeps its original rename behaviour.
func TestMoveWithoutAHookRenames(t *testing.T) {
	s, dir := writeTestServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp := post(t, ts.URL+"/api/archive", "application/json", `{"repo":"alpha","id":"live"}`)
	defer resp.Body.Close()
	if resp.StatusCode != 200 || !present(t, arcPath(dir, "alpha", "live")) {
		t.Errorf("status %d; archived=%v", resp.StatusCode, present(t, arcPath(dir, "alpha", "live")))
	}
}

// The unarchive direction takes the same three paths; this pins the store-owned
// one, since it is the direction that has to clear _archive/.
func TestUnarchiveStoreOwned(t *testing.T) {
	s, dir := writeTestServer(t)
	s.MutateBoard = func(repo, id string, archived bool) (bool, error) {
		if archived {
			t.Errorf("expected an unarchive, got archived=true")
		}
		os.MkdirAll(filepath.Dir(livePath(dir, repo, id)), 0o755)
		body, _ := os.ReadFile(arcPath(dir, repo, id))
		os.WriteFile(livePath(dir, repo, id), body, 0o644)
		os.Remove(arcPath(dir, repo, id))
		return true, nil
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp := post(t, ts.URL+"/api/unarchive", "application/json", `{"repo":"alpha","id":"old"}`)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("got %d want 200", resp.StatusCode)
	}
	if !present(t, livePath(dir, "alpha", "old")) || !gone(t, arcPath(dir, "alpha", "old")) {
		t.Error("un-archiving left the file in the wrong place, or in both")
	}
}
