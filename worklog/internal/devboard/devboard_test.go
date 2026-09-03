package devboard

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"gopkg.in/yaml.v3"
)

func withDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("DEVBOARD_DATA", dir)
	return dir
}

func TestDisabledWhenDirMissing(t *testing.T) {
	t.Setenv("DEVBOARD_DATA", filepath.Join(t.TempDir(), "nope"))
	if Enabled() {
		t.Fatal("Enabled() = true for nonexistent dir")
	}
	// side effects must succeed as no-ops
	for name, fn := range map[string]func() error{
		"OnStart": func() error { return OnStart("x", "X", "", "") },
		"OnDone":  func() error { return OnDone("x") },
		"OnPR":    func() error { return OnPR("x", "u") },
		"OnLink":  func() error { return OnLink("x", "Jira", "u") },
	} {
		if err := fn(); err != nil {
			t.Fatalf("%s while disabled: %v", name, err)
		}
	}
}

func TestOnStartCreatesTaskFile(t *testing.T) {
	dir := withDataDir(t)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "sess-123")
	if err := OnStart("tkt-1", "Fix the thing", "", ""); err != nil {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "*", "tkt-1.yaml"))
	if len(matches) != 1 {
		t.Fatalf("expected one task file, got %v", matches)
	}
	var task Task
	raw, _ := os.ReadFile(matches[0])
	if err := yaml.Unmarshal(raw, &task); err != nil {
		t.Fatal(err)
	}
	if task.Title != "Fix the thing" || task.Worklog != "tkt-1" || task.Session != "sess-123" || task.Schema != 1 {
		t.Fatalf("bad task: %+v", task)
	}
}

func seed(t *testing.T, dir, repo, slug, content string) string {
	t.Helper()
	p := filepath.Join(dir, repo, slug+".yaml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestOnDoneSetsPhaseClearsNeedsYou(t *testing.T) {
	dir := withDataDir(t)
	p := seed(t, dir, "repo-a", "tkt-2", `schema: 1
title: T
phase: verify
needs_you:
  - type: question
    text: pending?
`)
	if err := OnDone("tkt-2"); err != nil {
		t.Fatal(err)
	}
	var task Task
	raw, _ := os.ReadFile(p)
	yaml.Unmarshal(raw, &task)
	if task.Phase != "done" || len(task.NeedsYou) != 0 {
		t.Fatalf("phase=%q needs_you=%v", task.Phase, task.NeedsYou)
	}
}

func TestOnPRSetAndClear(t *testing.T) {
	dir := withDataDir(t)
	p := seed(t, dir, "repo-a", "tkt-3", "schema: 1\ntitle: T\n")
	if err := OnPR("tkt-3", "https://x/pr/1"); err != nil {
		t.Fatal(err)
	}
	var task Task
	raw, _ := os.ReadFile(p)
	yaml.Unmarshal(raw, &task)
	if len(task.Links) != 1 || task.Links[0].URL != "https://x/pr/1" || task.Links[0].Label != "PR" {
		t.Fatalf("links=%v", task.Links)
	}
	if err := OnPR("tkt-3", ""); err != nil { // clear
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(p)
	task = Task{}
	yaml.Unmarshal(raw, &task)
	if len(task.Links) != 0 {
		t.Fatalf("links after clear=%v", task.Links)
	}
}

func TestOnLinkSetAndClear(t *testing.T) {
	dir := withDataDir(t)
	p := seed(t, dir, "repo-a", "tkt-4", "schema: 1\ntitle: T\n")
	if err := OnLink("tkt-4", "Jira", "https://x/jira/1"); err != nil {
		t.Fatal(err)
	}
	var task Task
	raw, _ := os.ReadFile(p)
	yaml.Unmarshal(raw, &task)
	if len(task.Links) != 1 || task.Links[0].URL != "https://x/jira/1" || task.Links[0].Label != "Jira" {
		t.Fatalf("links=%v", task.Links)
	}
	// Setting a second named link must not disturb the first.
	if err := OnLink("tkt-4", "Slack", "https://x/slack/1"); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(p)
	task = Task{}
	yaml.Unmarshal(raw, &task)
	if len(task.Links) != 2 {
		t.Fatalf("links after second set=%v", task.Links)
	}
	if err := OnLink("tkt-4", "Jira", ""); err != nil { // clear just Jira
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(p)
	task = Task{}
	yaml.Unmarshal(raw, &task)
	if len(task.Links) != 1 || task.Links[0].Label != "Slack" {
		t.Fatalf("links after clearing Jira=%v", task.Links)
	}
}

func TestMalformedFileLeftByteIdentical(t *testing.T) {
	dir := withDataDir(t)
	garbage := "title: broken\n  bad indent: [unclosed\n"
	p := seed(t, dir, "repo-a", "bad", garbage)
	err := Mutate(p, func(t *Task) error { t.Phase = "x"; return nil })
	if err == nil || !strings.Contains(err.Error(), "not valid YAML") {
		t.Fatalf("expected parse error, got %v", err)
	}
	raw, _ := os.ReadFile(p)
	if !bytes.Equal(raw, []byte(garbage)) {
		t.Fatal("malformed file was modified")
	}
}

func TestExtraFieldsSurviveRoundTrip(t *testing.T) {
	dir := withDataDir(t)
	p := seed(t, dir, "repo-a", "tkt-4", "schema: 1\ntitle: T\ncustom_field: keep me\n")
	if err := Mutate(p, func(t *Task) error { t.Phase = "plan"; return nil }); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(p)
	if !strings.Contains(string(raw), "custom_field: keep me") {
		t.Fatalf("extra field lost:\n%s", raw)
	}
}

func TestConcurrentMutatesLoseNothing(t *testing.T) {
	dir := withDataDir(t)
	p := seed(t, dir, "repo-a", "tkt-5", "schema: 1\ntitle: T\n")
	const n = 50
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- Mutate(p, func(t *Task) error {
				t.Plan = append(t.Plan, PlanItem{Text: fmt.Sprintf("item-%d", i), State: "pending"})
				return nil
			})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var task Task
	raw, _ := os.ReadFile(p)
	if err := yaml.Unmarshal(raw, &task); err != nil {
		t.Fatalf("file corrupt after concurrent writes: %v", err)
	}
	if len(task.Plan) != n {
		t.Fatalf("lost updates: %d/%d plan items", len(task.Plan), n)
	}
}

func TestFindAcrossRepos(t *testing.T) {
	dir := withDataDir(t)
	seed(t, dir, "repo-b", "tkt-6", "schema: 1\n")
	p, err := Find("tkt-6")
	if err != nil || !strings.HasSuffix(p, filepath.Join("repo-b", "tkt-6.yaml")) {
		t.Fatalf("Find = %q, %v", p, err)
	}
	p, err = Find("absent")
	if err != nil || p != "" {
		t.Fatalf("Find(absent) = %q, %v", p, err)
	}
}

func TestSyncEpicRosterCreatesFile(t *testing.T) {
	dir := withDataDir(t)
	roster := []ChildIdentity{
		{ID: "child-a", Title: "Child A", State: ChildActive},
		{ID: "child-b", Title: "Child B", State: ChildPending},
	}
	if err := SyncEpicRoster("epic-1", "The Epic", roster); err != nil {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "*", "epic-1.yaml"))
	if len(matches) != 1 {
		t.Fatalf("expected one epic file, got %v", matches)
	}
	var task Task
	raw, _ := os.ReadFile(matches[0])
	yaml.Unmarshal(raw, &task)
	if task.Type != "epic" || task.Title != "The Epic" || task.Worklog != "epic-1" {
		t.Fatalf("bad epic identity: %+v", task)
	}
	if len(task.Children) != 2 || task.Children[0].State != ChildActive || task.Children[1].State != ChildPending {
		t.Fatalf("bad roster: %+v", task.Children)
	}
}

func TestSyncEpicRosterBackfillsTitleWithoutDisturbingChildren(t *testing.T) {
	dir := withDataDir(t)
	seed(t, dir, "repo-a", "epic-2", `schema: 1
worklog: epic-2
children:
  - id: child-a
    state: active
    phase: implementing
    plan:
      - text: do the thing
        state: in_progress
`)
	if err := SyncEpicRoster("epic-2", "Now Titled", []ChildIdentity{{ID: "child-a", Title: "Child A", State: ChildActive}}); err != nil {
		t.Fatal(err)
	}
	p, _ := Find("epic-2")
	var task Task
	raw, _ := os.ReadFile(p)
	yaml.Unmarshal(raw, &task)
	if task.Type != "epic" || task.Title != "Now Titled" {
		t.Fatalf("title/type not backfilled: %+v", task)
	}
	if len(task.Children) != 1 || task.Children[0].Phase != "implementing" || len(task.Children[0].Plan) != 1 {
		t.Fatalf("existing child in-flight detail disturbed: %+v", task.Children)
	}
}

func TestSyncEpicRosterLeavesUnlistedChildrenAlone(t *testing.T) {
	dir := withDataDir(t)
	seed(t, dir, "repo-a", "epic-3", `schema: 1
type: epic
title: T
children:
  - id: child-old
    state: done
`)
	if err := SyncEpicRoster("epic-3", "T", []ChildIdentity{{ID: "child-new", Title: "New", State: ChildActive}}); err != nil {
		t.Fatal(err)
	}
	p, _ := Find("epic-3")
	var task Task
	raw, _ := os.ReadFile(p)
	yaml.Unmarshal(raw, &task)
	if len(task.Children) != 2 {
		t.Fatalf("expected roster to grow, not replace: %+v", task.Children)
	}
}

func TestMutateChildAppendsAndTargetsCorrectEntry(t *testing.T) {
	dir := withDataDir(t)
	p := seed(t, dir, "repo-a", "epic-4", `schema: 1
type: epic
title: T
children:
  - id: child-a
    state: active
  - id: child-b
    state: active
`)
	_ = dir
	if err := MutateChild(p, "child-b", func(c *ChildEntry) error {
		c.Phase = "verify"
		c.Plan = append(c.Plan, PlanItem{Text: "step", State: "done"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var task Task
	raw, _ := os.ReadFile(p)
	yaml.Unmarshal(raw, &task)
	var a, b *ChildEntry
	for i := range task.Children {
		switch task.Children[i].ID {
		case "child-a":
			a = &task.Children[i]
		case "child-b":
			b = &task.Children[i]
		}
	}
	if a == nil || b == nil {
		t.Fatalf("children missing: %+v", task.Children)
	}
	if a.Phase != "" || len(a.Plan) != 0 {
		t.Fatalf("mutation leaked into sibling child: %+v", a)
	}
	if b.Phase != "verify" || len(b.Plan) != 1 {
		t.Fatalf("target child not mutated: %+v", b)
	}
}

func TestMutateChildAppendsUnknownChildAsPending(t *testing.T) {
	dir := withDataDir(t)
	p := seed(t, dir, "repo-a", "epic-5", "schema: 1\ntype: epic\ntitle: T\n")
	_ = dir
	if err := MutateChild(p, "child-new", func(c *ChildEntry) error {
		c.Phase = "intake"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var task Task
	raw, _ := os.ReadFile(p)
	yaml.Unmarshal(raw, &task)
	if len(task.Children) != 1 || task.Children[0].ID != "child-new" || task.Children[0].State != ChildPending {
		t.Fatalf("unknown child not appended as pending: %+v", task.Children)
	}
	if task.Children[0].Phase != "intake" {
		t.Fatalf("mutation not applied: %+v", task.Children[0])
	}
}

func TestOnDoneClosesWaitingOn(t *testing.T) {
	dir := withDataDir(t)
	p := seed(t, dir, "repo-a", "tkt-9", `schema: 1
title: T
phase: verify
waiting_on:
  - text: open question
    who: infra
    asked: 2026-08-25
`)
	if err := OnDone("tkt-9"); err != nil {
		t.Fatal(err)
	}
	var task Task
	raw, _ := os.ReadFile(p)
	yaml.Unmarshal(raw, &task)
	if len(task.WaitingOn) != 0 {
		t.Fatalf("waiting_on not cleared: %+v", task.WaitingOn)
	}
	found := false
	for _, d := range task.Decision {
		if strings.Contains(d.What, "unanswered at close: open question (infra)") {
			found = true
		}
	}
	if !found {
		t.Fatalf("close-out decision missing: %+v", task.Decision)
	}
}

func TestOnStartMirrorsSpikeType(t *testing.T) {
	dir := withDataDir(t)
	if err := OnStart("spike-1", "Investigate", "spike", ""); err != nil {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "*", "spike-1.yaml"))
	if len(matches) != 1 {
		t.Fatalf("expected one task file, got %v", matches)
	}
	var task Task
	raw, _ := os.ReadFile(matches[0])
	if err := yaml.Unmarshal(raw, &task); err != nil {
		t.Fatal(err)
	}
	if task.Type != "spike" {
		t.Errorf("Type = %q, want spike", task.Type)
	}
}

// An ordinary ticket writes no type at all.
func TestOnStartLeavesTypeEmptyForTicket(t *testing.T) {
	dir := withDataDir(t)
	if err := OnStart("tkt-2", "Ordinary", "", ""); err != nil {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "*", "tkt-2.yaml"))
	var task Task
	raw, _ := os.ReadFile(matches[0])
	if err := yaml.Unmarshal(raw, &task); err != nil {
		t.Fatal(err)
	}
	if task.Type != "" {
		t.Errorf("Type = %q, want empty", task.Type)
	}
}

// A start must never clobber the epic marker SyncEpicRoster owns.
func TestOnStartNeverOverwritesEpicType(t *testing.T) {
	dir := withDataDir(t)
	if err := OnStart("epic-1", "An epic", "", ""); err != nil {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "*", "epic-1.yaml"))
	if err := Mutate(matches[0], func(tk *Task) error { tk.Type = "epic"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := OnStart("epic-1", "An epic", "spike", ""); err != nil {
		t.Fatal(err)
	}
	var task Task
	raw, _ := os.ReadFile(matches[0])
	if err := yaml.Unmarshal(raw, &task); err != nil {
		t.Fatal(err)
	}
	if task.Type != "epic" {
		t.Errorf("Type = %q, want epic preserved", task.Type)
	}
}

// TestChildEntryExtraSurvivesWrite is adb-childentry-extra: an
// unrecognised key under children[] used to be destroyed by the next
// write to the epic file, the one durability guarantee Task already had
// via its inline Extra map and ChildEntry did not. The sub-item lists
// carry the same guarantee now, so a producer's own annotation on a plan
// step or scorecard row survives too.
func TestChildEntryExtraSurvivesWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "epic.yaml")
	const src = `schema: 1
title: An epic
type: epic
worklog: epic-a
children:
    - id: child-1
      state: active
      title: First child
      producer_note: keep me
      plan:
        - text: step one
          state: pending
          producer_step_note: keep me too
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	// A write that touches something else entirely.
	if err := Mutate(path, func(task *Task) error {
		task.Children[0].Phase = "implementing"
		return nil
	}); err != nil {
		t.Fatalf("Mutate: %v", err)
	}

	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"producer_note: keep me", "producer_step_note: keep me too"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("unknown key destroyed by the write: %q missing from\n%s", want, out)
		}
	}
	if !strings.Contains(string(out), "phase: implementing") {
		t.Errorf("the actual mutation did not land:\n%s", out)
	}
}
