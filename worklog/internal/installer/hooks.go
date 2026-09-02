package installer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SessionStart hook wiring for Claude Code.
//
// This is the one place the installer writes a file the human owns rather
// than one it deploys, so every operation here is conservative: opt-in at
// the call site, refuse rather than guess on malformed input, back up
// before the first mutation, and never touch a key we did not write.
//
// Hooks are a Claude Code feature; Cursor/Codex/Windsurf targets have no
// equivalent, which is why the worklog skill keeps a one-line prose
// fallback for the orientation this hook automates.

const (
	// hookMarker identifies our entry inside settings.json regardless of
	// which binary path it names. Anything containing it is ours.
	hookMarker = "hook session-start"

	// hookMatcher covers every session-start reason except "fork": a forked
	// session inherits its parent's context and would show the block twice.
	hookMatcher = "startup|resume|clear|compact"

	// hookTimeout bounds the hook at the harness level. It reads two small
	// markdown files; ten seconds means something is badly wrong and the
	// session should proceed without orientation rather than stall.
	hookTimeout = 10
)

// HookState describes settings.json relative to the hook we want.
type HookState int

const (
	// HookAbsent — no worklog entry. A legitimate state: the prompt is
	// opt-in, and declining must not be reported as drift.
	HookAbsent HookState = iota
	// HookCurrent — present and naming the installed binary.
	HookCurrent
	// HookStale — present but naming some other path (a moved binary, a
	// hand-edit, an install under a different home). This IS drift.
	HookStale
)

// SettingsPath returns the Claude Code user settings file.
func SettingsPath(home string) string {
	return filepath.Join(home, ".claude", "settings.json")
}

// HookBinPath returns the binary path the hook entry should name.
//
// It is deliberately the install location rather than os.Executable(): hooks
// do not inherit an interactive shell's PATH, and a `go run` invocation of
// the installer would otherwise bake a temp path into settings.json.
func HookBinPath(home string) string {
	return filepath.Join(home, ".local", "bin", "worklog")
}

// HookCommand returns the command string for the hook entry.
func HookCommand(binPath string) string {
	return binPath + " " + hookMarker
}

// InspectHook reports how settings.json relates to wantCmd. A missing file
// is HookAbsent, not an error. Malformed JSON IS an error — callers warn and
// leave the file alone rather than overwrite something they cannot read.
// The second return is the command string actually found, for the drift
// message.
func InspectHook(settingsPath, wantCmd string) (HookState, string, error) {
	root, err := readSettings(settingsPath)
	if err != nil {
		return HookAbsent, "", err
	}
	for _, group := range sessionStartGroups(root) {
		for _, h := range groupHooks(group) {
			cmd, _ := h["command"].(string)
			if !strings.Contains(cmd, hookMarker) {
				continue
			}
			if cmd == wantCmd {
				return HookCurrent, cmd, nil
			}
			return HookStale, cmd, nil
		}
	}
	return HookAbsent, "", nil
}

// InstallHook adds or repairs the SessionStart entry, preserving every other
// key in the file. It writes a .bak of the original on the first mutation
// and replaces the file atomically. A no-op when the entry is already
// current — the file is not rewritten at all.
func InstallHook(settingsPath, wantCmd string) error {
	root, err := readSettings(settingsPath)
	if err != nil {
		return err
	}

	entry := map[string]any{
		"type":    "command",
		"command": wantCmd,
		"timeout": hookTimeout,
	}

	// Repair in place if an entry of ours already exists; otherwise append a
	// new matcher group alongside whatever else is registered.
	repaired := false
	for _, group := range sessionStartGroups(root) {
		for _, h := range groupHooks(group) {
			if cmd, _ := h["command"].(string); strings.Contains(cmd, hookMarker) {
				if cmd == wantCmd {
					return nil // already current: do not rewrite the file
				}
				h["command"] = wantCmd
				repaired = true
			}
		}
	}

	if !repaired {
		hooks, ok := root["hooks"].(map[string]any)
		if !ok {
			hooks = map[string]any{}
			root["hooks"] = hooks
		}
		groups, _ := hooks["SessionStart"].([]any)
		hooks["SessionStart"] = append(groups, map[string]any{
			"matcher": hookMatcher,
			"hooks":   []any{entry},
		})
	}

	return writeSettings(settingsPath, root)
}

// readSettings parses settings.json. A missing file yields an empty object
// so a fresh machine can be configured; anything unparseable is an error.
func readSettings(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return map[string]any{}, nil
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON (%v)", path, err)
	}
	return root, nil
}

// writeSettings backs up the original and replaces the file atomically.
// Round-tripping through map[string]any does not preserve key order; values
// are preserved exactly, which is what matters for a config file.
func writeSettings(path string, root map[string]any) error {
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	if original, err := os.ReadFile(path); err == nil {
		if err := os.WriteFile(path+".bak", original, 0o644); err != nil {
			return fmt.Errorf("could not back up %s: %v", path, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".settings-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		os.Remove(name)
		return err
	}
	return os.Rename(name, path)
}

// sessionStartGroups returns the matcher groups under hooks.SessionStart,
// tolerating every shape a hand-edited file can take.
func sessionStartGroups(root map[string]any) []map[string]any {
	hooks, ok := root["hooks"].(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := hooks["SessionStart"].([]any)
	if !ok {
		return nil
	}
	var out []map[string]any
	for _, g := range raw {
		if m, ok := g.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// groupHooks returns the handler objects inside one matcher group.
func groupHooks(group map[string]any) []map[string]any {
	raw, ok := group["hooks"].([]any)
	if !ok {
		return nil
	}
	var out []map[string]any
	for _, h := range raw {
		if m, ok := h.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}
