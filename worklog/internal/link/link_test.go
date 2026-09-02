package link

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
)

func fixtureWorkdir(t *testing.T, work string) model.Workdir {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "WORK.md"), []byte(work), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, err := model.NewWorkdir(root)
	if err != nil {
		t.Fatal(err)
	}
	return wd
}

const fixtureWithLinks = `## Now
- [~] **AUTH-1** — Refactor auth
  - **ID**: auth-1
  - **Repo**: api
  - **Link**: Jira — https://company.atlassian.net/browse/AUTH-1234
  - **Started**: 2026-05-15

## Next

## Someday
`

const fixtureNoLinks = `## Now
- [~] **AUTH-1** — Refactor auth
  - **ID**: auth-1
  - **Repo**: api
  - **Started**: 2026-05-15

## Next

## Someday
`

func TestGet(t *testing.T) {
	wd := fixtureWorkdir(t, fixtureWithLinks)
	res, err := Get(wd, "auth-1", "Jira")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if res.URL != "https://company.atlassian.net/browse/AUTH-1234" {
		t.Errorf("URL = %q", res.URL)
	}
}

func TestGetUnsetName(t *testing.T) {
	wd := fixtureWorkdir(t, fixtureWithLinks)
	res, err := Get(wd, "auth-1", "Slack")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if res.URL != "" {
		t.Errorf("URL = %q, want empty", res.URL)
	}
}

func TestGetUnknownID(t *testing.T) {
	wd := fixtureWorkdir(t, fixtureWithLinks)
	_, err := Get(wd, "nope", "Jira")
	if !errors.Is(err, ErrIDNotFound) {
		t.Errorf("expected ErrIDNotFound, got %v", err)
	}
}

func TestList(t *testing.T) {
	wd := fixtureWorkdir(t, fixtureWithLinks)
	res, err := List(wd, "auth-1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(res.Links) != 1 || res.Links[0].Name != "Jira" {
		t.Errorf("Links = %+v", res.Links)
	}
}

func TestListNoLinks(t *testing.T) {
	wd := fixtureWorkdir(t, fixtureNoLinks)
	res, err := List(wd, "auth-1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(res.Links) != 0 {
		t.Errorf("Links = %+v, want none", res.Links)
	}
}

func TestSetLinkWritesValue(t *testing.T) {
	wd := fixtureWorkdir(t, fixtureNoLinks)
	res, err := SetLink(wd, "auth-1", "Jira", "https://company.atlassian.net/browse/AUTH-1234")
	if err != nil {
		t.Fatalf("SetLink: %v", err)
	}
	if res.URL != "https://company.atlassian.net/browse/AUTH-1234" || res.Previous != "" {
		t.Errorf("res = %+v", res)
	}
	data, _ := os.ReadFile(wd.WorkMD())
	if !strings.Contains(string(data), "  - **Link**: Jira — https://company.atlassian.net/browse/AUTH-1234") {
		t.Errorf("WORK.md missing new Link:\n%s", string(data))
	}
}

func TestSetLinkUpdatesInPlace(t *testing.T) {
	wd := fixtureWorkdir(t, fixtureWithLinks)
	res, err := SetLink(wd, "auth-1", "Jira", "https://company.atlassian.net/browse/AUTH-9999")
	if err != nil {
		t.Fatalf("SetLink: %v", err)
	}
	if res.Previous != "https://company.atlassian.net/browse/AUTH-1234" {
		t.Errorf("Previous = %q", res.Previous)
	}
	data, _ := os.ReadFile(wd.WorkMD())
	if !strings.Contains(string(data), "  - **Link**: Jira — https://company.atlassian.net/browse/AUTH-9999") {
		t.Errorf("WORK.md missing updated Link:\n%s", string(data))
	}
}

func TestSetLinkClearRemovesLine(t *testing.T) {
	wd := fixtureWorkdir(t, fixtureWithLinks)
	res, err := SetLink(wd, "auth-1", "Jira", "")
	if err != nil {
		t.Fatalf("SetLink: %v", err)
	}
	if res.URL != "" {
		t.Errorf("URL = %q, want empty", res.URL)
	}
	data, _ := os.ReadFile(wd.WorkMD())
	if strings.Contains(string(data), "**Link**") {
		t.Errorf("Link line should be gone entirely:\n%s", string(data))
	}
}
