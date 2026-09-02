package render

import (
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/parse"
)

func TestSetBlockLinkInsertsWhenMissing(t *testing.T) {
	src := `## Now
- [~] **AUTH-1** — Refactor auth
  - **ID**: auth-1
  - **PR**: https://example.com/pull/7
  - **Started**: 2026-05-15
`
	doc, err := parse.Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := SetBlockLink(doc, "auth-1", "Jira", "https://company.atlassian.net/browse/AUTH-1234")
	if err != nil {
		t.Fatalf("SetBlockLink: %v", err)
	}
	joined := strings.Join(out, "\n")
	if !strings.Contains(joined, "  - **Link**: Jira — https://company.atlassian.net/browse/AUTH-1234") {
		t.Errorf("expected new Link line:\n%s", joined)
	}
	lines := strings.Split(joined, "\n")
	iPR := lineIndex(lines, "  - **PR**: https://example.com/pull/7")
	iLink := lineIndex(lines, "  - **Link**: Jira — https://company.atlassian.net/browse/AUTH-1234")
	iStarted := lineIndex(lines, "  - **Started**: 2026-05-15")
	if !(iPR < iLink && iLink < iStarted) {
		t.Errorf("want PR < Link < Started, got %d < %d < %d:\n%s", iPR, iLink, iStarted, joined)
	}
}

func TestSetBlockLinkAppendsAfterExisting(t *testing.T) {
	src := `## Now
- [~] **AUTH-1** — Refactor auth
  - **ID**: auth-1
  - **Link**: Jira — https://company.atlassian.net/browse/AUTH-1234
  - **Started**: 2026-05-15
`
	doc, err := parse.Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := SetBlockLink(doc, "auth-1", "Slack", "https://example.slack.com/archives/C1/p1")
	if err != nil {
		t.Fatalf("SetBlockLink: %v", err)
	}
	lines := out
	iJira := lineIndex(lines, "  - **Link**: Jira — https://company.atlassian.net/browse/AUTH-1234")
	iSlack := lineIndex(lines, "  - **Link**: Slack — https://example.slack.com/archives/C1/p1")
	iStarted := lineIndex(lines, "  - **Started**: 2026-05-15")
	if iSlack != iJira+1 {
		t.Errorf("Slack at %d, want directly after Jira at %d:\n%s", iSlack, iJira, strings.Join(lines, "\n"))
	}
	if iSlack > iStarted {
		t.Errorf("Slack leaked past Started:\n%s", strings.Join(lines, "\n"))
	}
}

func TestSetBlockLinkUpdatesInPlace(t *testing.T) {
	src := `## Now
- [~] **AUTH-1** — Refactor auth
  - **ID**: auth-1
  - **Link**: Jira — https://company.atlassian.net/browse/AUTH-1234
  - **Link**: Slack — https://example.slack.com/archives/C1/p1
`
	doc, err := parse.Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := SetBlockLink(doc, "auth-1", "jira", "https://company.atlassian.net/browse/AUTH-9999")
	if err != nil {
		t.Fatalf("SetBlockLink: %v", err)
	}
	joined := strings.Join(out, "\n")
	if !strings.Contains(joined, "  - **Link**: jira — https://company.atlassian.net/browse/AUTH-9999") {
		t.Errorf("expected rewritten Jira line:\n%s", joined)
	}
	if !strings.Contains(joined, "  - **Link**: Slack — https://example.slack.com/archives/C1/p1") {
		t.Errorf("Slack line should survive untouched:\n%s", joined)
	}
	if strings.Count(joined, "**Link**: Jira") > 0 {
		t.Errorf("old-cased Jira line should have been replaced, not duplicated:\n%s", joined)
	}
}

func TestSetBlockLinkClearRemovesOnlyNamed(t *testing.T) {
	src := `## Now
- [~] **AUTH-1** — Refactor auth
  - **ID**: auth-1
  - **Link**: Jira — https://company.atlassian.net/browse/AUTH-1234
  - **Link**: Slack — https://example.slack.com/archives/C1/p1
  - **Started**: 2026-05-15
`
	doc, err := parse.Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := SetBlockLink(doc, "auth-1", "Jira", "")
	if err != nil {
		t.Fatalf("SetBlockLink: %v", err)
	}
	joined := strings.Join(out, "\n")
	if strings.Contains(joined, "Jira") {
		t.Errorf("Jira line should be removed entirely:\n%s", joined)
	}
	if !strings.Contains(joined, "  - **Link**: Slack — https://example.slack.com/archives/C1/p1") {
		t.Errorf("Slack line should survive:\n%s", joined)
	}
}

func TestListBlockLinks(t *testing.T) {
	src := `## Now
- [~] **AUTH-1** — Refactor auth
  - **ID**: auth-1
  - **Link**: Jira — https://company.atlassian.net/browse/AUTH-1234
  - **Link**: Slack — https://example.slack.com/archives/C1/p1
`
	doc, err := parse.Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	links, err := ListBlockLinks(doc, "auth-1")
	if err != nil {
		t.Fatalf("ListBlockLinks: %v", err)
	}
	if len(links) != 2 || links[0].Name != "Jira" || links[1].Name != "Slack" {
		t.Errorf("links = %+v", links)
	}
}

func TestListBlockLinksUnknownID(t *testing.T) {
	doc, err := parse.Bytes("WORK.md", []byte("## Now\n## Next\n## Someday\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := ListBlockLinks(doc, "nope"); err == nil {
		t.Fatal("expected error for unknown block")
	}
}
