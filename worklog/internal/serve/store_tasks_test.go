package serve

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
)

// These assert the payload's shape directly, from a hand-built snapshot.
// The equality gate in internal/cli proves the store-fed builder agrees
// with the file walk it replaces; this proves the parts of the shape that
// a comparison cannot pin on its own — an ordering that a fixture happens
// to satisfy either way, or a field the fixture never populates.

func task(repo, slug string, archived bool, mtime int64) StoreTask {
	return StoreTask{Repo: repo, Slug: slug, Archived: archived, MTime: mtime,
		Task: map[string]any{"schema": 1, "worklog": slug, "title": slug}}
}

func payload(t *testing.T, snap *StoreSnapshot) map[string]any {
	t.Helper()
	srv := New(Config{})
	srv.LoadStoreSnapshot = func() (*StoreSnapshot, error) { return snap, nil }
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/api/tasks")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func repoNames(t *testing.T, p map[string]any) []string {
	t.Helper()
	var names []string
	for _, r := range p["repos"].([]any) {
		names = append(names, r.(map[string]any)["repo"].(string))
	}
	return names
}

func taskIDs(t *testing.T, p map[string]any, repo string) []string {
	t.Helper()
	for _, r := range p["repos"].([]any) {
		m := r.(map[string]any)
		if m["repo"] != repo {
			continue
		}
		var ids []string
		for _, e := range m["tasks"].([]any) {
			ids = append(ids, e.(map[string]any)["id"].(string))
		}
		return ids
	}
	t.Fatalf("no repo group %q in %v", repo, repoNames(t, p))
	return nil
}

// Repo groups are sorted by name whatever order the snapshot arrives in.
// Asserted here rather than left to the equality gate, where the snapshot
// is built from a map and an unsorted builder lands in the right order
// often enough to pass.
func TestRepoGroupsAreSorted(t *testing.T) {
	p := payload(t, &StoreSnapshot{Tasks: []StoreTask{
		task("zeta", "z1", false, 1), task("alpha", "a1", false, 1),
		task("middle", "m1", false, 1), task("unknown", "u1", false, 1),
	}})
	if got, want := repoNames(t, p), []string{"alpha", "middle", "unknown", "zeta"}; !reflect.DeepEqual(got, want) {
		t.Errorf("repo order = %v, want %v", got, want)
	}
}

// Within a repo: live entries first, then archived, each by slug. That is
// the order the directory walk produced when these were files, live dir
// before _archive/ and filenames sorted inside each.
func TestLiveEntriesPrecedeArchivedThenSortBySlug(t *testing.T) {
	p := payload(t, &StoreSnapshot{Tasks: []StoreTask{
		task("r", "zz-live", false, 1), task("r", "aa-archived", true, 1),
		task("r", "aa-live", false, 1), task("r", "zz-archived", true, 1),
	}})
	want := []string{"aa-live", "zz-live", "aa-archived", "zz-archived"}
	if got := taskIDs(t, p, "r"); !reflect.DeepEqual(got, want) {
		t.Errorf("entry order = %v, want %v", got, want)
	}
}

// `file` is composed, not stored, and `archived` is present only when
// true — both are frozen wire shape (devboard/API.md).
func TestEntryFileAndArchivedFlag(t *testing.T) {
	p := payload(t, &StoreSnapshot{Tasks: []StoreTask{
		task("r", "live-one", false, 0), task("r", "gone", true, 0),
	}})
	entries := p["repos"].([]any)[0].(map[string]any)["tasks"].([]any)
	live := entries[0].(map[string]any)
	arch := entries[1].(map[string]any)

	if live["file"] != "r/live-one.yaml" {
		t.Errorf("live file = %v", live["file"])
	}
	if _, ok := live["archived"]; ok {
		t.Error("a live entry carries an `archived` key; it must be absent, not false")
	}
	if arch["file"] != "r/_archive/gone.yaml" {
		t.Errorf("archived file = %v", arch["file"])
	}
	if arch["archived"] != true {
		t.Errorf("archived flag = %v", arch["archived"])
	}
}

// mtime is the ticket's render stamp in seconds, the same instant the file
// mtime reported.
func TestMTimeIsTheRenderStampInSeconds(t *testing.T) {
	p := payload(t, &StoreSnapshot{Tasks: []StoreTask{task("r", "t", false, 1_500_000_000_123_456_789)}})
	entry := p["repos"].([]any)[0].(map[string]any)["tasks"].([]any)[0].(map[string]any)
	if got := entry["mtime"].(float64); got < 1.5e9 || got > 1.5000001e9 {
		t.Errorf("mtime = %v, want ~1.5e9 seconds", got)
	}
}

// Notes join by slug for a ticket and by id for a child. A ticket with no
// notes carries no key at all.
func TestNotesAttachByTicketAndChild(t *testing.T) {
	parent := task("r", "epic", false, 0)
	parent.Task["children"] = []any{
		map[string]any{"id": "kid-with", "title": "K1"},
		map[string]any{"id": "kid-without", "title": "K2"},
	}
	p := payload(t, &StoreSnapshot{
		Tasks: []StoreTask{parent, task("r", "no-notes", false, 0)},
		Notes: map[string]string{"epic": "epic notes", "kid-with": "kid notes"},
	})
	entries := p["repos"].([]any)[0].(map[string]any)["tasks"].([]any)
	epic := entries[0].(map[string]any)
	if epic["notes"] != "epic notes" {
		t.Errorf("ticket notes = %v", epic["notes"])
	}
	kids := epic["task"].(map[string]any)["children"].([]any)
	if got := kids[0].(map[string]any)["notes"]; got != "kid notes" {
		t.Errorf("child notes = %v", got)
	}
	if _, ok := kids[1].(map[string]any)["notes"]; ok {
		t.Error("a child with no notes file carries a `notes` key")
	}
	if _, ok := entries[1].(map[string]any)["notes"]; ok {
		t.Error("a ticket with no notes file carries a `notes` key")
	}
}

// Both backlog sections are always present, in bar order, empty rather
// than absent: the lens shows "no backlog reported by the server" when the
// list is missing, which is a different thing to say than "nothing queued".
func TestBacklogSectionsAlwaysPresentInBarOrder(t *testing.T) {
	for name, snap := range map[string]*StoreSnapshot{
		"populated": {Backlog: map[model.SectionName][]model.Block{
			model.SectionNext:    {{ID: "n1", Title: "First"}},
			model.SectionSomeday: {{ID: "s1", Title: "Later"}},
		}},
		"empty": {},
	} {
		t.Run(name, func(t *testing.T) {
			sections := payload(t, snap)["backlog"].([]any)
			var got []string
			for _, s := range sections {
				got = append(got, s.(map[string]any)["name"].(string))
			}
			if want := []string{"Next", "Someday"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("sections = %v, want %v", got, want)
			}
			for _, s := range sections {
				if s.(map[string]any)["items"] == nil {
					t.Errorf("%v items is null; the lens needs a list", s.(map[string]any)["name"])
				}
			}
		})
	}
}

// A repo whose tickets all vanish leaves no empty group behind, and a
// store with nothing in it still answers with the full envelope.
func TestEmptyBoardKeepsTheEnvelope(t *testing.T) {
	p := payload(t, &StoreSnapshot{})
	if got := p["repos"].([]any); len(got) != 0 {
		t.Errorf("repos = %v, want empty", got)
	}
	for _, k := range []string{"version", "generated", "repos", "feedback", "backlog"} {
		if _, ok := p[k]; !ok {
			t.Errorf("payload is missing %q", k)
		}
	}
	if p["feedback"] == nil {
		t.Error("feedback is null; the lens needs a list")
	}
}

// Feedback keeps the shape the board expects: `resolved` is present even
// when zero, since Entry's omitempty would otherwise drop it and the two
// states would be indistinguishable. The bytes are the rendered
// FEEDBACK.md the store produces, parsed in memory rather than off disk.
func TestFeedbackShape(t *testing.T) {
	md := []byte(`# Friction log

## 1756800000 — missing-feature
**Trigger**: wanted a move verb
**Excerpt**:
> user: can we move tickets
> agent: no verb exists
**Severity**: high
> this line is after an unknown field
**Context**: worklog session

## 1756803600 — tui-error
**Trigger**: tui crashed on empty list
**Excerpt**:
> stack trace here
**Context**: tui
**Resolved**: 1756810000
`)
	fb := payload(t, &StoreSnapshot{Feedback: md})["feedback"].([]any)
	if len(fb) != 2 {
		t.Fatalf("entries = %d, want 2", len(fb))
	}
	first := fb[0].(map[string]any)
	if v, ok := first["resolved"]; !ok || v != float64(0) {
		t.Errorf("resolved on an open entry = %v (present=%v), want 0 and present", v, ok)
	}
	if first["signal"] != "missing-feature" {
		t.Errorf("signal = %v", first["signal"])
	}
	if strings.Contains(first["excerpt"].(string), "after an unknown field") {
		t.Error("an unknown ** field let the following lines leak into the excerpt")
	}
	if second := fb[1].(map[string]any); second["resolved"] != float64(1756810000) {
		t.Errorf("resolved on a closed entry = %v", second["resolved"])
	}
}

// A friction log that does not parse costs the friction lens and nothing
// else: it is one lens of seven and must not take the board down.
func TestUnparseableFeedbackCostsOnlyItsLens(t *testing.T) {
	p := payload(t, &StoreSnapshot{
		Tasks:    []StoreTask{task("r", "still-here", false, 0)},
		Feedback: []byte("## not a timestamp — nonsense\n**Trigger**: x\n"),
	})
	if fb := p["feedback"].([]any); len(fb) != 0 {
		t.Errorf("feedback = %v, want none", fb)
	}
	if ids := taskIDs(t, p, "r"); len(ids) != 1 {
		t.Errorf("the board lost its cards too: %v", ids)
	}
}

// The server has no devboard directory left to touch: the payload comes
// from tickets, the change stream from a store fingerprint, and archiving
// from a store write. A full board served by a Config that has nowhere to
// point is the direct form of the claim.
func TestPayloadIgnoresTheDevboardDirectory(t *testing.T) {
	srv := New(Config{})
	srv.LoadStoreSnapshot = func() (*StoreSnapshot, error) {
		return &StoreSnapshot{
			Tasks: []StoreTask{task("r", "card", false, 7)},
			Notes: map[string]string{"card": "notes reach the payload too"},
		}, nil
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/api/tasks")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var p map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		t.Fatal(err)
	}
	if got := taskIDs(t, p, "r"); len(got) != 1 || got[0] != "card" {
		t.Fatalf("cards = %v, want the one from the store", got)
	}
	entry := p["repos"].([]any)[0].(map[string]any)["tasks"].([]any)[0].(map[string]any)
	if entry["notes"] != "notes reach the payload too" {
		t.Errorf("notes = %v", entry["notes"])
	}
}

// A loader that fails is a 500 with the board's own error shape, not a
// half-built payload and not a panic.
func TestLoaderFailureIsA500(t *testing.T) {
	srv := New(Config{})
	srv.LoadStoreSnapshot = func() (*StoreSnapshot, error) { return nil, errors.New("db is on fire") }
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/api/tasks")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("a 500 must still be JSON: %v", err)
	}
	if _, ok := body["error"]; !ok {
		t.Errorf("body = %v, want an error key", body)
	}
}
