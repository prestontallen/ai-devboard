package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/installer"
	"github.com/prestontallen/ai-devboard/worklog/internal/storepath"
)

// recordCommands replaces the service seam with one that REMEMBERS.
//
// installSandbox's stub fails every command unconditionally, which is right
// for install (a test wanting a particular answer should say so) but useless
// here: "systemd removal succeeded" and "systemd unavailable" are the same
// observation through it, and the ordering criterion needs the sequence.
func recordCommands(t *testing.T, fail bool) *[]string {
	t.Helper()
	var log []string
	orig := runCommand
	runCommand = func(name string, args ...string) ([]byte, error) {
		log = append(log, strings.TrimSpace(name+" "+strings.Join(args, " ")))
		if fail {
			return nil, fmt.Errorf("test stub: %q unavailable", name)
		}
		return nil, nil
	}
	t.Cleanup(func() { runCommand = orig })
	return &log
}

// recordRemovals wraps the destruction seams so a test can assert the order
// filesystem removals happened in, not merely their end state.
func recordRemovals(t *testing.T) *[]string {
	t.Helper()
	var log []string
	origAll, origFile := removeAll, removeFile
	removeAll = func(p string) error {
		log = append(log, p)
		return origAll(p)
	}
	removeFile = func(p string) error {
		log = append(log, p)
		return origFile(p)
	}
	t.Cleanup(func() { removeAll, removeFile = origAll, origFile })
	return &log
}

func indexOfSuffix(log []string, suffix string) int {
	for i, s := range log {
		if strings.HasSuffix(s, suffix) {
			return i
		}
	}
	return -1
}

// TestUninstallRemovalOrder is the ordering criterion. The binary must be
// last, and the service must be disabled before its files are unlinked.
func TestUninstallRemovalOrder(t *testing.T) {
	home := uninstallSandbox(t)
	cmds := recordCommands(t, false)
	removals := recordRemovals(t)

	out, stderr, err := runUninstallCmd(t, "--commit")
	if err != nil {
		t.Fatalf("uninstall --commit: %v\n%s\n%s", err, out, stderr)
	}

	// systemd: disable --now must precede the unit file's removal.
	if len(*cmds) == 0 || !strings.Contains((*cmds)[0], "disable --now") {
		t.Errorf("first service call was not disable --now: %v", *cmds)
	}
	unitIdx := indexOfSuffix(*removals, filepath.Join("systemd", "user", "devboard.service"))
	if unitIdx < 0 {
		t.Fatalf("the unit file was never removed: %v", *removals)
	}
	// The enable symlink goes before the unit it points at, or systemd is
	// left complaining about a dangling link on every daemon-reload.
	wantsIdx := indexOfSuffix(*removals, filepath.Join("default.target.wants", "devboard.service"))
	if wantsIdx < 0 || wantsIdx > unitIdx {
		t.Errorf("the enable symlink was not removed before the unit: wants=%d unit=%d", wantsIdx, unitIdx)
	}

	// The binary is last, because the session hook names it by path.
	binIdx := indexOfSuffix(*removals, filepath.Join(".local", "bin", "worklog"))
	if binIdx < 0 {
		t.Fatalf("the binary was never removed: %v", *removals)
	}
	if binIdx != len(*removals)-1 {
		t.Errorf("the binary was removed at %d of %d, not last:\n%v",
			binIdx, len(*removals)-1, *removals)
	}

	// And the end state: our things gone, the foreign ones untouched.
	for _, gone := range []string{
		filepath.Join(home, ".local", "bin", "worklog"),
		filepath.Join(home, ".claude", "skills", "dev-context"),
		filepath.Join(home, ".claude", "commands", "uc:worklog.md"),
		filepath.Join(home, ".config", "ai-devboard", "consent"),
	} {
		if _, err := os.Lstat(gone); err == nil {
			t.Errorf("%s survived a committed uninstall", gone)
		}
	}
	for _, target := range []string{".claude", ".cursor", ".codex"} {
		foreign := filepath.Join(home, target, "skills", "omarchy")
		if _, err := os.Lstat(foreign); err != nil {
			t.Errorf("foreign skill %s was deleted: %v", foreign, err)
		}
		// The human's own skills directory is not ours to remove, even
		// once we have taken everything of ours out of it.
		if _, err := os.Stat(filepath.Join(home, target, "skills")); err != nil {
			t.Errorf("the %s/skills directory itself was removed: %v", target, err)
		}
	}
}

// TestUninstallAbortsBeforeBinary is the safety property behind the ordering.
func TestUninstallAbortsBeforeBinary(t *testing.T) {
	home := uninstallSandbox(t)
	recordCommands(t, false)
	// An unparseable settings.json makes the hook removal fail. The hook
	// still names the binary, so the binary must survive.
	if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"),
		[]byte(`{"hooks": not json`), 0o644); err != nil {
		t.Fatal(err)
	}

	out, stderr, err := runUninstallCmd(t, "--commit")
	if err == nil {
		t.Fatalf("expected a nonzero exit when a step failed:\n%s", out)
	}
	bin := filepath.Join(home, ".local", "bin", "worklog")
	if _, statErr := os.Stat(bin); statErr != nil {
		t.Errorf("the binary was removed despite a failed step: %v", statErr)
	}
	if !strings.Contains(out+stderr+err.Error(), "NOT removed") &&
		!strings.Contains(out+stderr+err.Error(), "incomplete") {
		t.Errorf("the failure was not reported plainly:\n%s\n%s\n%v", out, stderr, err)
	}
}

// TestUninstallSecondRunIsNoop covers the idempotence criterion.
func TestUninstallSecondRunIsNoop(t *testing.T) {
	uninstallSandbox(t)
	recordCommands(t, false)
	if _, _, err := runUninstallCmd(t, "--commit"); err != nil {
		t.Fatalf("first uninstall: %v", err)
	}
	out, stderr, err := runUninstallCmd(t, "--commit")
	if err != nil {
		t.Fatalf("second uninstall was not a clean no-op: %v\n%s\n%s", err, out, stderr)
	}
	if strings.Contains(out, "failed:") {
		t.Errorf("second run reported failures:\n%s", out)
	}
}

// TestUninstallDeletesEmptyClaudeMD is this machine's real shape.
func TestUninstallDeletesEmptyClaudeMD(t *testing.T) {
	home := uninstallSandbox(t)
	recordCommands(t, false)
	claudeMD := filepath.Join(home, ".claude", "CLAUDE.md")
	if err := os.WriteFile(claudeMD,
		installer.MarkedDirective([]byte("# Workflow\n\nrules\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runUninstallCmd(t, "--commit"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(claudeMD); err == nil {
		got, _ := os.ReadFile(claudeMD)
		t.Errorf("an empty CLAUDE.md was left behind (%d bytes); the harness still loads it", len(got))
	}
}

// TestUninstallKeepsClaudeMDProse is the same path with the human's own text.
func TestUninstallKeepsClaudeMDProse(t *testing.T) {
	home := uninstallSandbox(t)
	recordCommands(t, false)
	claudeMD := filepath.Join(home, ".claude", "CLAUDE.md")
	body := append([]byte("# Mine\n\nkeep me\n\n"),
		installer.MarkedDirective([]byte("# Workflow\n\nrules\n"))...)
	if err := os.WriteFile(claudeMD, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runUninstallCmd(t, "--commit"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatalf("the file was deleted despite holding the human's prose: %v", err)
	}
	if !strings.Contains(string(got), "keep me") {
		t.Errorf("the human's prose was lost:\n%s", got)
	}
	if strings.Contains(string(got), "ai-devboard:begin") {
		t.Errorf("the managed block survived:\n%s", got)
	}
}

// TestUninstallMalformedSettings pins the sad path explicitly.
func TestUninstallMalformedSettings(t *testing.T) {
	home := uninstallSandbox(t)
	recordCommands(t, false)
	if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"),
		[]byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, stderr, err := runUninstallCmd(t, "--commit")
	if err == nil {
		t.Fatal("expected a nonzero exit for unreadable settings")
	}
	if !strings.Contains(out+stderr+err.Error(), "settings.json") {
		t.Errorf("the report does not name the file it could not read:\n%s\n%v", out, err)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".local", "bin", "worklog")); statErr != nil {
		t.Error("the binary went while a hook naming it could not be removed")
	}
}

// TestUninstallDockerAbsent: docker missing is a note, not a failure.
func TestUninstallDockerAbsent(t *testing.T) {
	uninstallSandbox(t)
	recordCommands(t, true) // every service command fails, docker included
	out, stderr, err := runUninstallCmd(t, "--commit")
	if err != nil {
		t.Fatalf("a missing docker failed the uninstall: %v\n%s\n%s", err, out, stderr)
	}
	if strings.Contains(out, "failed:") {
		t.Errorf("absent docker reported as a failure:\n%s", out)
	}
}

// TestUninstallDockerNeverUsesVolumeFlag: the container bind-mounts two real
// data directories, so -v would reach past the container into them.
func TestUninstallDockerNeverUsesVolumeFlag(t *testing.T) {
	uninstallSandbox(t)
	cmds := recordCommands(t, false)
	if _, _, err := runUninstallCmd(t, "--commit"); err != nil {
		t.Fatal(err)
	}
	for _, c := range *cmds {
		if strings.HasPrefix(c, "docker rm") && strings.Contains(c, " -v") {
			t.Errorf("docker rm used -v, which reaches into bind-mounted data: %q", c)
		}
	}
}

// TestUninstallPurgeData covers the opt-in.
func TestUninstallPurgeData(t *testing.T) {
	home := uninstallSandbox(t)
	recordCommands(t, false)
	corpus := filepath.Join(home, ".local", "share", "worklog")
	scratch := filepath.Join(home, ".local", "share", "worklog-scratch")
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, _, err := runUninstallCmd(t, "--commit", "--purge-data"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(corpus); err == nil {
		t.Error("--purge-data left the corpus in place")
	}
	if _, err := os.Stat(scratch); err != nil {
		t.Error("--purge-data removed worklog-scratch, which is not ours")
	}
}

// TestUninstallFreezeOwnership is the blocker: a freeze held by a live foreign
// process must survive.
func TestUninstallFreezeOwnership(t *testing.T) {
	home := uninstallSandbox(t)
	recordCommands(t, false)
	sentinel := filepath.Join(home, ".local", "share", "worklog", ".freeze")
	if err := os.WriteFile(sentinel,
		[]byte(`{"pid":424242,"reason":"adopt --commit","acquired":"2026-09-09T00:00:00Z"}`),
		0o644); err != nil {
		t.Fatal(err)
	}
	orig := processAlive
	processAlive = func(int) bool { return true }
	t.Cleanup(func() { processAlive = orig })

	out, _, err := runUninstallCmd(t, "--commit")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Error("a freeze held by a LIVE process was released; that unblocks writers mid-window")
	}
	if !strings.Contains(out, "424242") {
		t.Errorf("the surviving freeze was not reported:\n%s", out)
	}
}

// TestUninstallReleasesFreeze is the other half: a stale freeze goes.
func TestUninstallReleasesFreeze(t *testing.T) {
	home := uninstallSandbox(t)
	recordCommands(t, false)
	sentinel := filepath.Join(home, ".local", "share", "worklog", ".freeze")
	if err := os.WriteFile(sentinel,
		[]byte(`{"pid":424242,"reason":"a run that died","acquired":"2026-09-09T00:00:00Z"}`),
		0o644); err != nil {
		t.Fatal(err)
	}
	orig := processAlive
	processAlive = func(int) bool { return false }
	t.Cleanup(func() { processAlive = orig })

	out, _, err := runUninstallCmd(t, "--commit")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sentinel); err == nil {
		t.Error("a stale freeze survived; a reinstall would refuse every write on day one")
	}
	if !strings.Contains(out, "released the write freeze") {
		t.Errorf("the release was not reported:\n%s", out)
	}
}

// TestUninstallLegacyHeadless covers the three no-TTY answers.
func TestUninstallLegacyHeadless(t *testing.T) {
	seed := func(t *testing.T) (home, legacy string) {
		t.Helper()
		home = uninstallSandbox(t)
		recordCommands(t, false)
		legacy = filepath.Join(home, ".local", "share", "worklog-migration")
		if err := os.MkdirAll(filepath.Join(legacy, "rollback"), 0o755); err != nil {
			t.Fatal(err)
		}
		// A relocated store, so the offer is allowed to be made at all.
		newStore := storepath.Dir(filepath.Join(home, ".local", "share", "worklog"))
		if err := os.MkdirAll(newStore, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(storepath.DBIn(newStore), []byte("db"), 0o644); err != nil {
			t.Fatal(err)
		}
		return home, legacy
	}

	t.Run("no answer keeps", func(t *testing.T) {
		_, legacy := seed(t)
		if _, _, err := runUninstallCmd(t, "--commit"); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(legacy); err != nil {
			t.Error("legacy data removed with no answer given")
		}
	})

	t.Run("keep-legacy keeps", func(t *testing.T) {
		_, legacy := seed(t)
		if _, _, err := runUninstallCmd(t, "--commit", "--keep-legacy"); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(legacy); err != nil {
			t.Error("--keep-legacy removed it")
		}
	})

	t.Run("purge-legacy removes", func(t *testing.T) {
		_, legacy := seed(t)
		if _, _, err := runUninstallCmd(t, "--commit", "--purge-legacy"); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(legacy); err == nil {
			t.Error("--purge-legacy left it in place")
		}
	})
}

// TestUninstallLegacyPromptDefaultsSafe is the end-of-input hazard, pinned.
//
// promptAllowed is forced on with no terminal attached, which is exactly the
// shape that once recorded consent for a prompt nobody saw. The data must
// survive it.
func TestUninstallLegacyPromptDefaultsSafe(t *testing.T) {
	home := uninstallSandbox(t)
	recordCommands(t, false)
	legacy := filepath.Join(home, ".local", "share", "worklog-migration")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(legacy, "irreplaceable")
	if err := os.WriteFile(keep, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	newStore := storepath.Dir(filepath.Join(home, ".local", "share", "worklog"))
	if err := os.MkdirAll(newStore, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storepath.DBIn(newStore), []byte("db"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("INSTALL_PROMPT_FORCE", "1")

	if _, _, err := runUninstallCmd(t, "--commit"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Error("an unanswered prompt deleted the data; the default must be keep")
	}
}

// TestUninstallRemovesLegacySymlinkDeploy: the bash era LINKED skills where
// the Go era copies them, so removal must handle both.
func TestUninstallRemovesLegacySymlinkDeploy(t *testing.T) {
	home := uninstallSandbox(t)
	recordCommands(t, false)
	// Replace a copied skill with a symlink, as link_skill would have left it.
	linked := filepath.Join(home, ".claude", "skills", "fan-out")
	if err := os.RemoveAll(linked); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(home, "checkout", "fan-out")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(src, linked); err != nil {
		t.Fatal(err)
	}

	if _, _, err := runUninstallCmd(t, "--commit"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(linked); err == nil {
		t.Error("a legacy symlink deployment survived")
	}
	// Unlinked, never followed: the checkout it pointed at is untouched.
	if _, err := os.Stat(src); err != nil {
		t.Error("removal followed the symlink into the checkout")
	}
}

// TestUninstallToneSymlink: ownership comes from where the link points, never
// from its name.
func TestUninstallToneSymlink(t *testing.T) {
	t.Run("into the checkout is ours", func(t *testing.T) {
		home := uninstallSandbox(t)
		recordCommands(t, false)
		repo := filepath.Join(home, "checkout")
		if err := os.MkdirAll(filepath.Join(repo, "concise-tone"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, ".config", "ai-devboard", "targets"),
			[]byte("repo "+repo+"\n"+filepath.Join(home, ".claude", "skills")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(home, ".claude", "skills", "concise-tone")
		if err := os.Symlink(filepath.Join(repo, "concise-tone"), link); err != nil {
			t.Fatal(err)
		}
		if _, _, err := runUninstallCmd(t, "--commit"); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(link); err == nil {
			t.Error("a tone symlink into our own checkout was left behind")
		}
	})

	t.Run("elsewhere is theirs", func(t *testing.T) {
		home := uninstallSandbox(t)
		recordCommands(t, false)
		elsewhere := filepath.Join(home, "someone-else", "their-tone")
		if err := os.MkdirAll(elsewhere, 0o755); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(home, ".claude", "skills", "their-tone")
		if err := os.Symlink(elsewhere, link); err != nil {
			t.Fatal(err)
		}
		if _, _, err := runUninstallCmd(t, "--commit"); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(link); err != nil {
			t.Error("deleted a tone skill that does not point into our checkout")
		}
	})
}

// TestUninstallLegacyRootResolution: legacy lookup is HOME-based while the
// corpus default is XDG-based, so the two can disagree and the report says so
// rather than searching twice and guessing.
func TestUninstallLegacyRootResolution(t *testing.T) {
	home := uninstallSandbox(t)
	moved := filepath.Join(home, "xdg", "worklog")
	if err := os.MkdirAll(moved, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "xdg"))
	t.Setenv("WORKLOG_DIR", moved)

	out, _, err := runUninstallCmd(t)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "rooted differently") {
		t.Errorf("a HOME/XDG root mismatch was not reported:\n%s", out)
	}
}

// TestLegacyAnswerMeansDelete tests the consent RULE rather than the widget.
//
// It exists because the integration test above could not: huh errors out with
// no terminal, so that path returns false from the error branch and never
// reaches the comparison. A mutation that made the comparison always say yes
// left it green. This is the assertion that actually holds the line.
func TestLegacyAnswerMeansDelete(t *testing.T) {
	t.Parallel()
	// Everything a form can produce on its own at end-of-input.
	for _, refusal := range []string{"", " ", "\n", "y", "Y", "yes", "YES", "true", "1", "no", "delet"} {
		if legacyAnswerMeansDelete(refusal) {
			t.Errorf("%q was accepted as consent to delete", refusal)
		}
	}
	// Only the whole word, typed on purpose.
	for _, consent := range []string{"delete", "DELETE", "  delete  ", "Delete"} {
		if !legacyAnswerMeansDelete(consent) {
			t.Errorf("%q should mean delete", consent)
		}
	}
}
