package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/prestontallen/day2day/internal/model"
	"github.com/prestontallen/day2day/internal/parse"
	"github.com/prestontallen/day2day/internal/pr"
)

const tuiFixture = `## Now
- [~] **AUTH-1** — Refactor auth
  - **ID**: auth-1
  - **Repo**: api
  - **Tags**: refactor
  - **PR**: https://example.com/pull/1
  - **Started**: 2026-05-15

## Next

## Someday
`

func buildStatus(t *testing.T) *Status {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "WORK.md"), []byte(tuiFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, err := model.NewWorkdir(root)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := parse.File(wd.WorkMD())
	if err != nil {
		t.Fatal(err)
	}
	var wrote []struct{ id, val string }
	s := newStatusWithWriter(wd, doc, func(id, value string) (pr.Result, error) {
		wrote = append(wrote, struct{ id, val string }{id, value})
		return pr.Result{ID: id, PR: value, Previous: "https://example.com/pull/1"}, nil
	})
	// Stash the recorded writes on the Status for the test to read back.
	s.prStatus = "" // sanity
	_ = wrote
	return s
}

// keyMsg is a convenience to build a tea.KeyMsg for a single rune.
func keyMsg(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func TestTUIPressPEntersEditMode(t *testing.T) {
	s := buildStatus(t)
	// First send window-size so the list has items selectable.
	s.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	if s.mode != modeList {
		t.Fatalf("initial mode = %v", s.mode)
	}
	if b := s.selectedBlock(); b == nil {
		t.Fatal("no item selected after window-size")
	}

	s.Update(keyMsg('p'))
	if s.mode != modeEditPR {
		t.Errorf("after p: mode = %v, want modeEditPR", s.mode)
	}
	if s.prTarget != "auth-1" {
		t.Errorf("prTarget = %q", s.prTarget)
	}
	if s.prValue != "https://example.com/pull/1" {
		t.Errorf("prValue = %q (expected pre-populated)", s.prValue)
	}
}

func TestTUIEscCancelsEdit(t *testing.T) {
	s := buildStatus(t)
	s.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	s.Update(keyMsg('p'))
	if s.mode != modeEditPR {
		t.Fatalf("setup: not in edit mode")
	}
	s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if s.mode != modeList {
		t.Errorf("after Esc: mode = %v, want modeList", s.mode)
	}
	if s.prForm != nil {
		t.Errorf("expected form cleared after Esc")
	}
}

func TestTUISubmitInvokesWriter(t *testing.T) {
	// Custom writer tracks invocations.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "WORK.md"), []byte(tuiFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, err := model.NewWorkdir(root)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := parse.File(wd.WorkMD())
	if err != nil {
		t.Fatal(err)
	}
	var calls []struct{ id, val string }
	s := newStatusWithWriter(wd, doc, func(id, value string) (pr.Result, error) {
		calls = append(calls, struct{ id, val string }{id, value})
		return pr.Result{ID: id, PR: value, Previous: "old"}, nil
	})
	s.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	s.Update(keyMsg('p'))
	if s.mode != modeEditPR {
		t.Fatalf("not in edit mode")
	}
	// Directly drive submitPR (the form's internal state machine is
	// driven by huh; we test the persistence path independently).
	s.prValue = "https://example.com/pull/99"
	s.submitPR()
	if len(calls) != 1 {
		t.Fatalf("writer calls = %d, want 1", len(calls))
	}
	if calls[0].id != "auth-1" || calls[0].val != "https://example.com/pull/99" {
		t.Errorf("call = %+v", calls[0])
	}
}

func TestTUIDetailPaneShowsEmDashOnEmpty(t *testing.T) {
	// Fixture with empty PR.
	root := t.TempDir()
	src := `## Now
- [~] **AUTH-1** — Refactor auth
  - **ID**: auth-1
  - **PR**:
  - **Started**: 2026-05-15

## Next

## Someday
`
	if err := os.WriteFile(filepath.Join(root, "WORK.md"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, _ := model.NewWorkdir(root)
	doc, _ := parse.File(wd.WorkMD())
	s := newStatusWithWriter(wd, doc, func(id, value string) (pr.Result, error) {
		return pr.Result{}, nil
	})
	s.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	pane := s.detailPane()
	if !contains(pane, "PR: —") {
		t.Errorf("detail pane should show em-dash for empty PR:\n%s", pane)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && indexOf(s, sub) >= 0
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
