package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/installer"
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

	// Every test in this package gets the service-manager stub, not just the
	// ones that go looking for it. Until now the extras were unreachable
	// under test by ACCIDENT: they sit behind promptAllowed() and the suite
	// has no TTY. This ticket adds a headless flag for each of them, which
	// removes that accident — and the devboard extra shells to systemctl and
	// docker. Without this line, `go test ./...` would start reloading the
	// developer's real systemd units and inspecting their containers.
	stubSystemCommands(t)

	return home, repo
}

// stubSystemCommands makes shelling out to the machine impossible for the
// duration of one test. Failing every command is deliberate: a test that
// needs a particular answer should say so explicitly rather than inherit
// whatever the developer's machine happens to be running.
func stubSystemCommands(t *testing.T) {
	t.Helper()
	orig := runCommand
	runCommand = func(name string, args ...string) ([]byte, error) {
		return nil, fmt.Errorf("test stub: refusing to run %q", name)
	}
	t.Cleanup(func() { runCommand = orig })
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

// Criterion 13: --with-session-hook is the headless consent path. It must
// install the hook with no TTY and no prompt — the case a remote session
// hits, where promptAllowed() is false and the opt-in is otherwise
// unreachable.
func TestInstallWithSessionHookHeadless(t *testing.T) {
	home, repo := installSandbox(t)
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	settings := installer.SettingsPath(home)
	if err := os.WriteFile(settings, []byte(`{"tui": "fullscreen"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _, err := runInstallCmd(t, "", "--repo", repo, "--with-session-hook")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "SessionStart hook: added") {
		t.Fatalf("expected the hook to be added, got %q", out)
	}

	want := installer.HookCommand(installer.HookBinPath(home))
	state, _, err := installer.InspectHook(settings, want)
	if err != nil {
		t.Fatal(err)
	}
	if state != installer.HookCurrent {
		t.Errorf("state = %v, want HookCurrent", state)
	}
	raw, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"tui"`) {
		t.Errorf("the pre-existing key was dropped:\n%s", raw)
	}
}

// Without the flag and without a TTY, nothing is written — the prompt is the
// only other consent path and it never runs headless.
func TestInstallWithoutFlagLeavesSettingsAlone(t *testing.T) {
	home, repo := installSandbox(t)
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	if _, _, err := runInstallCmd(t, "", "--repo", repo); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(installer.SettingsPath(home)); !os.IsNotExist(err) {
		t.Error("a headless install without the flag wrote settings.json")
	}
}

// --check never writes, so pairing it with --with-session-hook is a usage
// error rather than a silently ignored flag.
func TestInstallCheckWithHookFlagIsUsageError(t *testing.T) {
	_, repo := installSandbox(t)
	_, _, err := runInstallCmd(t, "", "--repo", repo, "--check", "--with-session-hook")
	if err == nil {
		t.Fatal("expected a usage error")
	}
	ec, ok := err.(exitCoder)
	if !ok || ec.ExitCode() != 64 {
		t.Fatalf("expected exit 64, got %v (%T)", err, err)
	}
}

func installerConfPathForTest() string {
	return filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "ai-devboard", "targets")
}

// TestDumbTermEOFRecordsNoSkillsDecision is the trap this ticket nearly fell
// into. A Confirm returns its DEFAULT at end-of-input without erroring, so
// asking "deploy skills?" that way would record a decline the human never
// made — and, because the record is consulted before the prompt, never ask
// them again. Silence must decide nothing.
func TestDumbTermEOFRecordsNoSkillsDecision(t *testing.T) {
	home, repo := installSandbox(t)
	t.Setenv("INSTALL_PROMPT_FORCE", "1")
	t.Setenv("TERM", "dumb")

	_, _, err := runInstallCmd(t, "", "--repo", repo)
	if err == nil {
		t.Fatal("an unanswered prompt succeeded")
	}
	consent, cerr := installer.LoadConsent(filepath.Join(home, ".config", "ai-devboard", "consent"))
	if cerr != nil {
		t.Fatal(cerr)
	}
	if consent.Asked(installer.ExtraSkills) {
		t.Error("an unanswered prompt was recorded as a decision")
	}
}

// TestEmptyConfigMeansDeclined: a config file naming no targets is something
// a human wrote. Reading it as "no opinion" and falling through to detection
// is how a deliberate choice gets overridden.
func TestEmptyConfigMeansDeclined(t *testing.T) {
	home, repo := installSandbox(t)
	confDir := filepath.Join(home, ".config", "ai-devboard")
	os.MkdirAll(confDir, 0o755)
	os.WriteFile(filepath.Join(confDir, "targets"), []byte("# binary only\n"), 0o644)
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755) // detectable, and must be ignored

	stdout, _, err := runInstallCmd(t, "", "--repo", repo)
	if err != nil {
		t.Fatalf("declining should be a clean end state: %v", err)
	}
	if !strings.Contains(stdout, "declined") {
		t.Errorf("output did not say skills were declined: %q", stdout)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".claude", "skills", "dev-context")); statErr == nil {
		t.Error("skills were deployed despite an empty config")
	}
}

// TestDeclinedSkillsAreCleanUnderCheck: a declined extra is a supported end
// state, not drift. Otherwise declining leaves --check failing forever, which
// is the asymmetry the devboard data dir already had.
func TestDeclinedSkillsAreCleanUnderCheck(t *testing.T) {
	home, repo := installSandbox(t)
	confDir := filepath.Join(home, ".config", "ai-devboard")
	os.MkdirAll(confDir, 0o755)
	os.WriteFile(filepath.Join(confDir, "consent"), []byte("skills declined\n"), 0o644)

	if _, _, err := runInstallCmd(t, "--check", "--repo", repo); err != nil {
		t.Errorf("--check on a machine that declined skills should exit 0, got %v", err)
	}
}

// TestExistingMachineIsNotReasked: a machine with a configured target list
// chose skills before there was anywhere to record it. Asking once more for
// something it plainly already has would be a regression dressed as a feature.
func TestExistingMachineIsNotReasked(t *testing.T) {
	home, repo := installSandbox(t)
	confDir := filepath.Join(home, ".config", "ai-devboard")
	os.MkdirAll(confDir, 0o755)
	target := filepath.Join(home, ".claude", "skills")
	os.WriteFile(filepath.Join(confDir, "targets"), []byte(target+"\n"), 0o644)

	if _, _, err := runInstallCmd(t, "", "--repo", repo); err != nil {
		t.Fatalf("install: %v", err)
	}
	consent, _ := installer.LoadConsent(filepath.Join(confDir, "consent"))
	if !consent.Accepted(installer.ExtraSkills) {
		t.Error("an already-configured machine was not read as having accepted skills")
	}
	if _, err := os.Stat(filepath.Join(target, "dev-context")); err != nil {
		t.Errorf("skills were not deployed to the configured target: %v", err)
	}
}

// TestDeclineFlagsRecordWithoutPrompting: a decline is a decision and needs
// the same headless form an acceptance has. Before this there were four ways
// to say yes and none to say no except answering a prompt, which is not a
// headless answer at all.
func TestDeclineFlagsRecordWithoutPrompting(t *testing.T) {
	for extra, flag := range map[string]string{
		installer.ExtraSkills:       "--no-skills",
		installer.ExtraSessionHook:  "--no-session-hook",
		installer.ExtraClaudeMD:     "--no-claude-md",
		installer.ExtraDevboardUnit: "--no-devboard-service",
	} {
		t.Run(extra, func(t *testing.T) {
			home, repo := installSandbox(t)
			os.MkdirAll(filepath.Join(home, ".claude"), 0o755)

			if _, _, err := runInstallCmd(t, "", flag, "--repo", repo); err != nil {
				t.Fatalf("%s: %v", flag, err)
			}
			consent, _ := installer.LoadConsent(filepath.Join(home, ".config", "ai-devboard", "consent"))
			if !consent.Asked(extra) {
				t.Fatalf("%s did not record a decision", flag)
			}
			if consent.Accepted(extra) {
				t.Errorf("%s recorded acceptance", flag)
			}
		})
	}
}

// TestDeclineFlagSkipsTheWork: recording a decline must also mean not doing
// the thing. Skills are the visible case — nothing should land in a target.
func TestDeclineFlagSkipsTheWork(t *testing.T) {
	home, repo := installSandbox(t)
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)

	if _, _, err := runInstallCmd(t, "", "--no-skills", "--repo", repo); err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "dev-context")); err == nil {
		t.Error("skills were deployed despite --no-skills")
	}
}

// TestContradictoryFlagsAreAUsageError: both flags for one extra is a
// contradiction, not a precedence question. Picking a winner would silently
// do the opposite of what half the command line asked for.
func TestContradictoryFlagsAreAUsageError(t *testing.T) {
	for _, pair := range [][2]string{
		{"--with-skills", "--no-skills"},
		{"--with-session-hook", "--no-session-hook"},
		{"--with-claude-md", "--no-claude-md"},
		{"--with-devboard-service", "--no-devboard-service"},
	} {
		_, repo := installSandbox(t)
		_, _, err := runInstallCmd(t, "", pair[0], pair[1], "--repo", repo)
		if err == nil {
			t.Errorf("%s %s was accepted", pair[0], pair[1])
		}
	}
}

// TestCheckRejectsDeclineFlagsToo: a decline is recorded, and recording is a
// write, so --check must refuse it on the same rule as an acceptance.
func TestCheckRejectsDeclineFlagsToo(t *testing.T) {
	for _, flag := range []string{
		"--no-skills", "--no-session-hook", "--no-claude-md", "--no-devboard-service",
	} {
		_, repo := installSandbox(t)
		_, _, err := runInstallCmd(t, "", "--check", flag, "--repo", repo)
		if err == nil || !strings.Contains(err.Error(), "never writes") {
			t.Errorf("--check %s: want a usage error, got %v", flag, err)
		}
	}
}

// TestToneProbeWalksTargets is adb-tone-glob-targets: the probe globbed
// ~/.claude/skills alone, so a Cursor-only machine warned forever about a
// tone skill it actually had.
func TestToneProbeWalksTargets(t *testing.T) {
	home, repo := installSandbox(t)
	cursor := filepath.Join(home, ".cursor", "skills")
	if err := os.MkdirAll(filepath.Join(cursor, "concise-tone"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A config naming ONLY the Cursor target. The old probe never looked here.
	if err := os.MkdirAll(filepath.Join(home, ".config", "ai-devboard"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "ai-devboard", "targets"),
		[]byte("repo "+repo+"\n"+cursor+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, stderr, err := runInstallCmd(t, "", "--repo", repo)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if strings.Contains(stderr, "no personal *tone* skill") {
		t.Errorf("warned about a missing tone skill that is present in %s:\n%s", cursor, stderr)
	}
	if !strings.Contains(out, "concise-tone") {
		t.Errorf("the tone skill in the Cursor target was never noticed:\n%s", out)
	}
}

// TestDanglingOptionalSkillIsDrift is adb-tone-symlink-drift: a glob cannot
// tell a working skill from a link whose target is gone.
func TestDanglingOptionalSkillIsDrift(t *testing.T) {
	home, repo := installSandbox(t)
	skills := filepath.Join(home, ".claude", "skills")
	if err := os.MkdirAll(skills, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, "gone-away"),
		filepath.Join(skills, "concise-tone")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".config", "ai-devboard"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "ai-devboard", "targets"),
		[]byte("repo "+repo+"\n"+skills+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _, _ := runInstallCmd(t, "", "--repo", repo, "--check")
	if !strings.Contains(out, "drift") || !strings.Contains(out, "concise-tone") {
		t.Errorf("a dangling optional skill was not reported as drift:\n%s", out)
	}
}
