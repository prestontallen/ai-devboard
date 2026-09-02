package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const linkCLIFixture = `## Now
- [~] **AUTH-1** — Refactor auth
  - **ID**: auth-1
  - **Repo**: api
  - **Started**: 2026-05-15

## Next

## Someday
`

func linkFixtureDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "WORK.md"), []byte(linkCLIFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// invokeLink drives the `link` cobra subcommand and captures stdout.
func invokeLink(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	prev := flagDir
	flagDir = dir
	t.Cleanup(func() { flagDir = prev })

	cmd := newLinkCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestLinkCLISetWritesField(t *testing.T) {
	dir := linkFixtureDir(t)
	out, err := invokeLink(t, dir, "auth-1", "Jira", "https://company.atlassian.net/browse/AUTH-1234", "--json")
	if err != nil {
		t.Fatalf("invokeLink: %v\nout: %s", err, out)
	}
	var res map[string]string
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("json: %v\nout: %s", err, out)
	}
	if res["url"] != "https://company.atlassian.net/browse/AUTH-1234" {
		t.Errorf("url = %q", res["url"])
	}
	if res["name"] != "Jira" {
		t.Errorf("name = %q", res["name"])
	}
	data, _ := os.ReadFile(filepath.Join(dir, "WORK.md"))
	if !strings.Contains(string(data), "  - **Link**: Jira — https://company.atlassian.net/browse/AUTH-1234") {
		t.Errorf("WORK.md missing new Link line:\n%s", string(data))
	}
}

func TestLinkCLIClearRemovesLine(t *testing.T) {
	dir := linkFixtureDir(t)
	if _, err := invokeLink(t, dir, "auth-1", "Jira", "https://company.atlassian.net/browse/AUTH-1234", "--json"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if _, err := invokeLink(t, dir, "auth-1", "Jira", "--clear", "--json"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "WORK.md"))
	if strings.Contains(string(data), "**Link**") {
		t.Errorf("expected Link line removed entirely:\n%q", string(data))
	}
}

func TestLinkCLIGetShowsCurrent(t *testing.T) {
	dir := linkFixtureDir(t)
	if _, err := invokeLink(t, dir, "auth-1", "Jira", "https://company.atlassian.net/browse/AUTH-1234", "--json"); err != nil {
		t.Fatalf("set: %v", err)
	}
	out, err := invokeLink(t, dir, "auth-1", "Jira", "--json")
	if err != nil {
		t.Fatalf("read: %v\nout: %s", err, out)
	}
	var res map[string]string
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("json: %v\nout: %s", err, out)
	}
	if res["url"] != "https://company.atlassian.net/browse/AUTH-1234" {
		t.Errorf("url = %q", res["url"])
	}
}

func TestLinkCLIListAll(t *testing.T) {
	dir := linkFixtureDir(t)
	if _, err := invokeLink(t, dir, "auth-1", "Jira", "https://a.example/1", "--json"); err != nil {
		t.Fatalf("set jira: %v", err)
	}
	if _, err := invokeLink(t, dir, "auth-1", "Slack", "https://b.example/2", "--json"); err != nil {
		t.Fatalf("set slack: %v", err)
	}
	out, err := invokeLink(t, dir, "auth-1", "--json")
	if err != nil {
		t.Fatalf("list: %v\nout: %s", err, out)
	}
	var res struct {
		Links []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"links"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("json: %v\nout: %s", err, out)
	}
	if len(res.Links) != 2 {
		t.Fatalf("links = %+v, want 2", res.Links)
	}
}

func TestLinkCLIListEmpty(t *testing.T) {
	dir := linkFixtureDir(t)
	out, err := invokeLink(t, dir, "auth-1")
	if err != nil {
		t.Fatalf("list: %v\nout: %s", err, out)
	}
	if !strings.Contains(out, "no links set") {
		t.Errorf("expected empty-state message, got: %q", out)
	}
}

func TestLinkCLIConflictURLAndClear(t *testing.T) {
	dir := linkFixtureDir(t)
	_, err := invokeLink(t, dir, "auth-1", "Jira", "https://example.com", "--clear", "--json")
	assertExit64(t, err)
}

func TestLinkCLIConflictURLAndEdit(t *testing.T) {
	dir := linkFixtureDir(t)
	_, err := invokeLink(t, dir, "auth-1", "Jira", "https://example.com", "--edit", "--json")
	assertExit64(t, err)
}

func TestLinkCLIConflictClearAndEdit(t *testing.T) {
	dir := linkFixtureDir(t)
	_, err := invokeLink(t, dir, "auth-1", "Jira", "--clear", "--edit", "--json")
	assertExit64(t, err)
}

func TestLinkCLIClearWithoutNameFails(t *testing.T) {
	dir := linkFixtureDir(t)
	_, err := invokeLink(t, dir, "auth-1", "--clear", "--json")
	assertExit64(t, err)
}

func TestLinkCLIEditWithoutNameFails(t *testing.T) {
	dir := linkFixtureDir(t)
	_, err := invokeLink(t, dir, "auth-1", "--edit", "--json")
	assertExit64(t, err)
}

// go test's stdin is never a TTY, so a valid --edit invocation exercises
// the non-TTY rejection path here (matching pr.go's equivalent, untested
// gap for the TTY-present path itself — same precedent as `worklog pr`).
func TestLinkCLIEditRequiresTTY(t *testing.T) {
	dir := linkFixtureDir(t)
	_, err := invokeLink(t, dir, "auth-1", "Jira", "--edit", "--json")
	assertExit64(t, err)
}

func TestLinkCLIChildMirror(t *testing.T) {
	dir := t.TempDir()
	// A child-of-epic ticket is just a normal bullet block carrying
	// **Parent**: — same shape internal/done's epic tests use. No devboard
	// data dir is set, so the devboard mirror itself no-ops; this test
	// only checks that SetLink surfaces Parent so the CLI picks the child
	// mirror path over the plain-ticket one.
	work := `## Now
- [~] **EPIC-A** — Cross-cutting effort
  - **ID**: epic-a
  - **Type**: epic
  - **Active children**: child-1
- [~] **CHILD-1** — First sub-task
  - **ID**: child-1
  - **Parent**: epic-a

## Next

## Someday
`
	if err := os.WriteFile(filepath.Join(dir, "WORK.md"), []byte(work), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := invokeLink(t, dir, "child-1", "Jira", "https://company.atlassian.net/browse/CHILD-1", "--json")
	if err != nil {
		t.Fatalf("invokeLink: %v\nout: %s", err, out)
	}
	var res map[string]string
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("json: %v\nout: %s", err, out)
	}
	if res["parent"] != "epic-a" {
		t.Errorf("parent = %q, want epic-a", res["parent"])
	}
}

func assertExit64(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	ec, ok := err.(exitCoder)
	if !ok {
		t.Fatalf("expected exitCoder, got %T: %v", err, err)
	}
	if ec.ExitCode() != 64 {
		t.Errorf("exit = %d, want 64", ec.ExitCode())
	}
}
