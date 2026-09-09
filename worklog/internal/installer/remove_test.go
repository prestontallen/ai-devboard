package installer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The live settings.json shape on the maintainer's machine: our hook alone in
// its group, five foreign top-level keys beside "hooks". A fixture without the
// foreign keys would let "foreign keys survive" pass vacuously.
const liveSettings = `{
  "model": "opus",
  "hooks": {
    "SessionStart": [
      {
        "matcher": "startup|resume|clear|compact",
        "hooks": [
          {"type": "command", "command": "/home/x/.local/bin/worklog hook session-start", "timeout": 10}
        ]
      }
    ]
  },
  "enabledPlugins": {"a": true},
  "effortLevel": "high",
  "tui": {"theme": "dark"},
  "agentPushNotifEnabled": true
}`

func writeTemp(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRemoveHookPreservesForeign(t *testing.T) {
	t.Parallel()
	p := writeTemp(t, "settings.json", liveSettings)

	removed, err := RemoveHook(p)
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("RemoveHook reported nothing removed")
	}

	var got map[string]any
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("uninstall left settings.json unparseable: %v", err)
	}

	// Values, not layout: the file round-trips through a map, so key order
	// is not preserved and asserting on bytes would assert the wrong thing.
	for k, want := range map[string]any{
		"model":                 "opus",
		"effortLevel":           "high",
		"agentPushNotifEnabled": true,
	} {
		if got[k] != want {
			t.Errorf("foreign key %q = %v, want %v", k, got[k], want)
		}
	}
	for _, k := range []string{"enabledPlugins", "tui"} {
		if _, ok := got[k]; !ok {
			t.Errorf("foreign key %q was dropped", k)
		}
	}
	// No empty scaffolding left behind.
	if h, ok := got["hooks"]; ok {
		t.Errorf("hooks key survived with no hooks in it: %v", h)
	}
	if strings.Contains(string(raw), "session-start") {
		t.Error("the hook command string is still in the file")
	}
}

// TestRemoveHookSparesForeignHandler is the case that forces handler
// granularity: a human may add their own handler to the group we created.
func TestRemoveHookSparesForeignHandler(t *testing.T) {
	t.Parallel()
	p := writeTemp(t, "settings.json", `{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "startup|resume|clear|compact",
        "hooks": [
          {"type": "command", "command": "/home/x/.local/bin/worklog hook session-start"},
          {"type": "command", "command": "/usr/bin/their-own-thing"}
        ]
      }
    ]
  }
}`)
	removed, err := RemoveHook(p)
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("nothing removed")
	}
	raw, _ := os.ReadFile(p)
	if !strings.Contains(string(raw), "their-own-thing") {
		t.Errorf("a foreign handler in our own matcher group was deleted:\n%s", raw)
	}
	if strings.Contains(string(raw), "session-start") {
		t.Errorf("our handler survived:\n%s", raw)
	}
}

func TestRemoveHookKeepsBackup(t *testing.T) {
	t.Parallel()
	p := writeTemp(t, "settings.json", liveSettings)
	sentinel := "the pre-install settings, which are the ones worth keeping"
	if err := os.WriteFile(p+".bak", []byte(sentinel), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := RemoveHook(p); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(p + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != sentinel {
		t.Errorf("uninstall clobbered the existing backup:\ngot:  %s\nwant: %s", got, sentinel)
	}
}

func TestRemoveHookAbsentIsNoop(t *testing.T) {
	t.Parallel()
	body := `{"model":"opus","hooks":{"SessionStart":[{"matcher":"x","hooks":[{"command":"/usr/bin/theirs"}]}]}}`
	p := writeTemp(t, "settings.json", body)
	removed, err := RemoveHook(p)
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Error("reported a removal on a file holding no hook of ours")
	}
	got, _ := os.ReadFile(p)
	if string(got) != body {
		t.Errorf("a no-op rewrote the file:\ngot:  %s\nwant: %s", got, body)
	}
	if _, err := os.Stat(p + ".bak"); err == nil {
		t.Error("a no-op created a backup")
	}
}

func TestRemoveHookMalformedRefuses(t *testing.T) {
	t.Parallel()
	p := writeTemp(t, "settings.json", `{"hooks": not json`)
	if _, err := RemoveHook(p); err == nil {
		t.Fatal("expected an error on unparseable settings")
	}
}

func TestRemoveDirectiveKeepsProse(t *testing.T) {
	t.Parallel()
	body := "# My own notes\n\nSomething I wrote.\n\n" +
		string(MarkedDirective([]byte("# Managed\n\nrules here\n"))) +
		"\nMore of my prose.\n"

	next, removed := RemoveDirective([]byte(body))
	if !removed {
		t.Fatal("nothing removed")
	}
	s := string(next)
	for _, want := range []string{"# My own notes", "Something I wrote.", "More of my prose."} {
		if !strings.Contains(s, want) {
			t.Errorf("the human's prose %q was lost:\n%s", want, s)
		}
	}
	if strings.Contains(s, "rules here") || strings.Contains(s, "ai-devboard:begin") {
		t.Errorf("managed content survived:\n%s", s)
	}
}

// TestRemoveDirectiveWholeFile is this machine's actual shape: the file is
// nothing but the managed block.
func TestRemoveDirectiveWholeFile(t *testing.T) {
	t.Parallel()
	body := MarkedDirective([]byte("# Workflow\n\nrules\n"))
	next, removed := RemoveDirective(body)
	if !removed {
		t.Fatal("nothing removed")
	}
	if len(strings.TrimSpace(string(next))) != 0 {
		t.Errorf("expected nothing to remain, got %q", next)
	}
}

func TestRemoveDirectiveRefusesUnmarked(t *testing.T) {
	t.Parallel()
	// An unmarked directive: exactly the bytes install would write, minus
	// the markers. It cannot be told apart from the human's own prose.
	unmarked := "# Development workflow\n\nInvoke the dev-context skill first.\n"
	next, removed := RemoveDirective([]byte(unmarked))
	if removed {
		t.Error("removed an unmarked block it cannot prove it wrote")
	}
	if string(next) != unmarked {
		t.Errorf("bytes changed on a refusal:\ngot:  %q\nwant: %q", next, unmarked)
	}

	foreign := "# Notes\n\nI use the dev-context skill and ai-devboard daily.\n"
	next, removed = RemoveDirective([]byte(foreign))
	if removed || string(next) != foreign {
		t.Errorf("touched the human's own prose about this tool:\n%q", next)
	}
}

// TestRemoveDirectiveNeedsNoRepo is the signature property. InspectDirective
// takes the repo body and so cannot be called without a checkout; this must
// answer from the file alone.
func TestRemoveDirectiveNeedsNoRepo(t *testing.T) {
	t.Parallel()
	body := MarkedDirective([]byte("# Anything at all\n\nfrom any revision\n"))
	if !HasManagedDirective(body) {
		t.Fatal("HasManagedDirective could not see a marked block")
	}
	// Content the current repo would never produce still resolves, because
	// the markers are the handle and the body is irrelevant.
	if _, removed := RemoveDirective(body); !removed {
		t.Error("RemoveDirective needed to know what the repo says")
	}
}

// TestRemoveDirectiveRoundTrip pins the seam healing: install, uninstall,
// install again must not accumulate whitespace.
func TestRemoveDirectiveRoundTrip(t *testing.T) {
	t.Parallel()
	block := MarkedDirective([]byte("# W\n\nrules\n"))
	start := []byte("mine\n")

	once, err := ReplaceDirective(start, block)
	if err != nil {
		t.Fatal(err)
	}
	back, removed := RemoveDirective(once)
	if !removed {
		t.Fatal("nothing removed")
	}
	again, err := ReplaceDirective(back, block)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(once) {
		t.Errorf("a round trip changed the file:\nfirst:  %q\nsecond: %q", once, again)
	}
}
