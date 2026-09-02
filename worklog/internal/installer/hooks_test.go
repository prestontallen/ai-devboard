package installer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeSettingsFile drops a settings.json into a temp home and returns its
// path plus the home dir.
func writeSettingsFile(t *testing.T, body string) (string, string) {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := SettingsPath(home)
	if body != "" {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return path, home
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatalf("result is not valid JSON: %v\n%s", err, raw)
	}
	return root
}

// hookCommands returns every command string registered under SessionStart.
func hookCommands(t *testing.T, path string) []string {
	t.Helper()
	var out []string
	for _, g := range sessionStartGroups(readJSON(t, path)) {
		for _, h := range groupHooks(g) {
			if cmd, ok := h["command"].(string); ok {
				out = append(out, cmd)
			}
		}
	}
	return out
}

const realisticSettings = `{
  "enabledPlugins": { "clangd-lsp@claude-plugins-official": true },
  "effortLevel": "high",
  "modelSettings": { "claude-fable-5": { "effortLevel": "high" } },
  "tui": "fullscreen"
}`

// Criterion 5: the merge adds the entry and preserves every foreign key.
func TestInstallHookMerge(t *testing.T) {
	path, home := writeSettingsFile(t, realisticSettings)
	want := HookCommand(HookBinPath(home))

	if err := InstallHook(path, want); err != nil {
		t.Fatalf("InstallHook: %v", err)
	}

	root := readJSON(t, path)
	var before map[string]any
	if err := json.Unmarshal([]byte(realisticSettings), &before); err != nil {
		t.Fatal(err)
	}
	for k, v := range before {
		got, ok := root[k]
		if !ok {
			t.Fatalf("key %q was dropped by the merge", k)
		}
		wantJSON, _ := json.Marshal(v)
		gotJSON, _ := json.Marshal(got)
		if string(wantJSON) != string(gotJSON) {
			t.Errorf("key %q changed: %s -> %s", k, wantJSON, gotJSON)
		}
	}

	cmds := hookCommands(t, path)
	if len(cmds) != 1 || cmds[0] != want {
		t.Errorf("commands = %v, want exactly [%q]", cmds, want)
	}

	// The entry must carry a matcher and a timeout.
	groups := sessionStartGroups(root)
	if len(groups) != 1 {
		t.Fatalf("want 1 matcher group, got %d", len(groups))
	}
	if groups[0]["matcher"] != hookMatcher {
		t.Errorf("matcher = %v, want %q", groups[0]["matcher"], hookMatcher)
	}
	if groupHooks(groups[0])[0]["timeout"] != float64(hookTimeout) {
		t.Errorf("timeout = %v, want %d", groupHooks(groups[0])[0]["timeout"], hookTimeout)
	}

	// The original was backed up.
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Errorf("expected a .bak of the original: %v", err)
	}
}

// Criterion 6: re-running install neither rewrites the file nor duplicates.
func TestInstallHookIdempotent(t *testing.T) {
	path, home := writeSettingsFile(t, realisticSettings)
	want := HookCommand(HookBinPath(home))

	if err := InstallHook(path, want); err != nil {
		t.Fatalf("first InstallHook: %v", err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := InstallHook(path, want); err != nil {
		t.Fatalf("second InstallHook: %v", err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Errorf("second install changed the file:\n%s\n---\n%s", first, second)
	}
	info2, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(info2.ModTime()) {
		t.Error("second install rewrote the file; it should be a no-op")
	}
	if cmds := hookCommands(t, path); len(cmds) != 1 {
		t.Errorf("commands = %v, want exactly one", cmds)
	}

	state, _, err := InspectHook(path, want)
	if err != nil || state != HookCurrent {
		t.Errorf("InspectHook = (%v, %v), want HookCurrent", state, err)
	}
}

// Criterion 7: a foreign SessionStart group survives the merge.
func TestInstallHookForeignEntry(t *testing.T) {
	const foreign = `{
  "hooks": {
    "SessionStart": [
      {"matcher": "startup", "hooks": [{"type": "command", "command": "/usr/bin/other-tool init"}]}
    ],
    "PreToolUse": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "/usr/bin/guard"}]}
    ]
  }
}`
	path, home := writeSettingsFile(t, foreign)
	want := HookCommand(HookBinPath(home))

	if err := InstallHook(path, want); err != nil {
		t.Fatalf("InstallHook: %v", err)
	}

	cmds := hookCommands(t, path)
	if len(cmds) != 2 {
		t.Fatalf("commands = %v, want the foreign one plus ours", cmds)
	}
	var sawForeign, sawOurs bool
	for _, c := range cmds {
		if c == "/usr/bin/other-tool init" {
			sawForeign = true
		}
		if c == want {
			sawOurs = true
		}
	}
	if !sawForeign {
		t.Error("the foreign SessionStart entry was lost")
	}
	if !sawOurs {
		t.Error("our entry was not added")
	}

	// An unrelated event must be untouched.
	hooks, _ := readJSON(t, path)["hooks"].(map[string]any)
	if _, ok := hooks["PreToolUse"]; !ok {
		t.Error("PreToolUse was dropped")
	}
}

// Criterion 8: malformed settings.json is refused, not overwritten.
func TestInstallHookMalformed(t *testing.T) {
	const broken = "{ this is not json"
	path, home := writeSettingsFile(t, broken)
	want := HookCommand(HookBinPath(home))

	if _, _, err := InspectHook(path, want); err == nil {
		t.Error("InspectHook should report malformed JSON as an error")
	}
	if err := InstallHook(path, want); err == nil {
		t.Fatal("InstallHook should refuse a file it cannot parse")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != broken {
		t.Errorf("the malformed file was modified:\n%s", raw)
	}
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Error("no backup should be written when nothing is written")
	}
}

// Criterion 9: absence is not drift; a wrong binary path is.
func TestInstallHookCheck(t *testing.T) {
	t.Run("absent", func(t *testing.T) {
		path, home := writeSettingsFile(t, realisticSettings)
		state, _, err := InspectHook(path, HookCommand(HookBinPath(home)))
		if err != nil {
			t.Fatal(err)
		}
		if state != HookAbsent {
			t.Errorf("state = %v, want HookAbsent", state)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		home := t.TempDir()
		state, _, err := InspectHook(SettingsPath(home), HookCommand(HookBinPath(home)))
		if err != nil {
			t.Fatalf("a missing settings.json must not be an error: %v", err)
		}
		if state != HookAbsent {
			t.Errorf("state = %v, want HookAbsent", state)
		}
	})

	t.Run("stale path", func(t *testing.T) {
		path, home := writeSettingsFile(t, `{
  "hooks": {"SessionStart": [
    {"matcher": "startup|resume|clear|compact",
     "hooks": [{"type": "command", "command": "/opt/old/worklog hook session-start"}]}
  ]}
}`)
		want := HookCommand(HookBinPath(home))
		state, found, err := InspectHook(path, want)
		if err != nil {
			t.Fatal(err)
		}
		if state != HookStale {
			t.Errorf("state = %v, want HookStale", state)
		}
		if found != "/opt/old/worklog hook session-start" {
			t.Errorf("found = %q, want the stale command for the drift message", found)
		}

		// Repair rewrites the path in place rather than appending.
		if err := InstallHook(path, want); err != nil {
			t.Fatal(err)
		}
		cmds := hookCommands(t, path)
		if len(cmds) != 1 || cmds[0] != want {
			t.Errorf("commands = %v, want exactly [%q]", cmds, want)
		}
	})
}

// Criterion 10: inspection never writes. (The install.go call sites gate
// writes on mode; this pins the library half.)
func TestInstallHookNoWrite(t *testing.T) {
	path, home := writeSettingsFile(t, realisticSettings)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, _, err := InspectHook(path, HookCommand(HookBinPath(home))); err != nil {
			t.Fatal(err)
		}
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Error("InspectHook modified settings.json")
	}
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Error("InspectHook wrote a backup")
	}
}

// A fresh machine with no settings.json at all still gets a valid file.
func TestInstallHookCreatesFile(t *testing.T) {
	home := t.TempDir()
	path := SettingsPath(home)
	want := HookCommand(HookBinPath(home))

	if err := InstallHook(path, want); err != nil {
		t.Fatalf("InstallHook: %v", err)
	}
	if cmds := hookCommands(t, path); len(cmds) != 1 || cmds[0] != want {
		t.Errorf("commands = %v, want [%q]", cmds, want)
	}
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Error("nothing existed to back up, but a .bak was written")
	}
}
