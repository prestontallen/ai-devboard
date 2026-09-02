package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runInstallCmd executes `worklog install <args...>` with captured streams.
func runInstallCmd(t *testing.T, stdin string, args ...string) (string, string, error) {
	t.Helper()
	cmd := newRoot()
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(append([]string{"install"}, args...))
	err := cmd.Execute()
	return out.String(), errBuf.String(), err
}

// installSandbox: fake home + fake repo + config env isolation.
func installSandbox(t *testing.T) (home, repo string) {
	t.Helper()
	home = t.TempDir()
	repo = t.TempDir()
	for _, d := range []string{"dev-context", "contract", "fan-out"} {
		os.MkdirAll(filepath.Join(repo, d), 0o755)
		os.WriteFile(filepath.Join(repo, d, "SKILL.md"), []byte("# "+d+"\n"), 0o644)
	}
	os.MkdirAll(filepath.Join(repo, "worklog", "skill", "claude"), 0o755)
	os.MkdirAll(filepath.Join(repo, "worklog", "skill", "references"), 0o755)
	os.WriteFile(filepath.Join(repo, "worklog", "skill", "SKILL.md"), []byte("# w\n"), 0o644)
	os.WriteFile(filepath.Join(repo, "worklog", "skill", "references", "cli.md"), []byte("# r\n"), 0o644)
	os.WriteFile(filepath.Join(repo, "worklog", "skill", "claude", "command.md"), []byte("# c\n"), 0o644)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("DEVBOARD_DATA", filepath.Join(home, ".local", "share", "devboard"))
	t.Setenv("INSTALL_PROMPT_FORCE", "") // default: no prompts in tests
	return home, repo
}

func TestInstallNonTTYNeverPrompts(t *testing.T) {
	home, repo := installSandbox(t)
	os.MkdirAll(filepath.Join(home, ".cursor"), 0o755)
	// piped stdin, no config: must fall back to detection without a TUI
	// (huh would grab /dev/tty if it ever ran — this test would hang).
	out, _, err := runInstallCmd(t, "", "--repo", repo)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "detected agent dirs") {
		t.Fatalf("expected detection note, got %q", out)
	}
	if _, err := os.Stat(filepath.Join(home, ".cursor", "skills", "fan-out", "SKILL.md")); err != nil {
		t.Fatal("detection deploy missing")
	}
}

func TestInstallZeroTargetsIsError(t *testing.T) {
	_, repo := installSandbox(t) // home has NO agent dirs → detection empty
	_, _, err := runInstallCmd(t, "", "--repo", repo)
	if err == nil || !strings.Contains(err.Error(), "no install targets") {
		t.Fatalf("expected zero-target error, got %v", err)
	}
}

func TestInstallCheckWritesNothingAndReportsDrift(t *testing.T) {
	home, repo := installSandbox(t)
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	out, _, err := runInstallCmd(t, "", "--repo", repo, "--check")
	if err == nil {
		t.Fatal("expected drift exit")
	}
	if !strings.Contains(out, "drift:") {
		t.Fatalf("no drift lines: %q", out)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills")); !os.IsNotExist(err) {
		t.Fatal("--check deployed files")
	}
	if _, err := os.Stat(installerConfPathForTest()); !os.IsNotExist(err) {
		t.Fatal("--check wrote the config")
	}
}

func TestInstallUsesSavedConfigSilently(t *testing.T) {
	home, repo := installSandbox(t)
	target := filepath.Join(home, "custom", "skills")
	confDir := filepath.Join(home, ".config", "ai-devboard")
	os.MkdirAll(confDir, 0o755)
	os.WriteFile(filepath.Join(confDir, "targets"), []byte(target+"\n"), 0o644) // bash-era format
	out, _, err := runInstallCmd(t, "", "--repo", repo)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "targets: from config") {
		t.Fatalf("expected config note, got %q", out)
	}
	if _, err := os.Stat(filepath.Join(target, "contract", "SKILL.md")); err != nil {
		t.Fatal("config target not deployed")
	}
}

func TestInstallDumbTermEOFPromptYieldsZeroTargetError(t *testing.T) {
	// No agent dirs exist, so the accessible-mode (TERM=dumb) prompt has
	// nothing pre-selected; huh returns an EMPTY selection on EOF with no
	// error — the exact silent-success trap criterion 3 forbids.
	_, repo := installSandbox(t)
	t.Setenv("INSTALL_PROMPT_FORCE", "1")
	t.Setenv("TERM", "dumb")
	_, _, err := runInstallCmd(t, "", "--repo", repo)
	if err == nil || !strings.Contains(err.Error(), "no install targets") {
		t.Fatalf("dumb-term EOF must be a zero-target error, got %v", err)
	}
}

func TestInstallRepoGoneIsDistinctError(t *testing.T) {
	home, _ := installSandbox(t)
	confDir := filepath.Join(home, ".config", "ai-devboard")
	os.MkdirAll(confDir, 0o755)
	os.WriteFile(filepath.Join(confDir, "targets"),
		[]byte("repo /nonexistent/checkout\n"+filepath.Join(home, "x", "skills")+"\n"), 0o644)
	_, _, err := runInstallCmd(t, "", "--check")
	if err == nil || !strings.Contains(err.Error(), "repo not found at /nonexistent/checkout") {
		t.Fatalf("expected repo-not-found error, got %v", err)
	}
}

func installerConfPathForTest() string {
	return filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "ai-devboard", "targets")
}
