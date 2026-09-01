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
		"OnStart": func() error { return OnStart("x", "X") },
		"OnDone":  func() error { return OnDone("x") },
		"OnPR":    func() error { return OnPR("x", "u") },
	} {
		if err := fn(); err != nil {
			t.Fatalf("%s while disabled: %v", name, err)
		}
	}
}

func TestOnStartCreatesTaskFile(t *testing.T) {
	dir := withDataDir(t)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "sess-123")
	if err := OnStart("tkt-1", "Fix the thing"); err != nil {
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
