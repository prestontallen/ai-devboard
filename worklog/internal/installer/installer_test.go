package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fakeRepo builds a minimal checkout with every deploy source present.
func fakeRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	for _, d := range []string{"dev-context", "contract", "fan-out"} {
		writeFile(t, filepath.Join(repo, d, "SKILL.md"), "# "+d+"\n")
	}
	writeFile(t, filepath.Join(repo, "fan-out", "references", "risk-scout.md"), "ref\n")
	writeFile(t, filepath.Join(repo, "worklog", "skill", "SKILL.md"), "# worklog\n")
	writeFile(t, filepath.Join(repo, "worklog", "skill", "claude", "command.md"), "# cmd\n")
	return repo
}

func TestLoadConfigBashEraFormat(t *testing.T) {
	p := filepath.Join(t.TempDir(), "targets")
	writeFile(t, p, "# comment\n\n/home/x/.claude/skills\n/home/x/.cursor/skills\n")
	cfg, ok, err := LoadConfig(p)
	if err != nil || !ok {
		t.Fatal(err, ok)
	}
	if cfg.RepoRoot != "" || len(cfg.Targets) != 2 || cfg.Targets[1] != "/home/x/.cursor/skills" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestConfigRoundTripWithRepo(t *testing.T) {
	p := filepath.Join(t.TempDir(), "targets")
	in := Config{RepoRoot: "/home/x/ai-devboard", Targets: []string{"/a/skills", "/b/skills"}}
	if err := SaveConfig(p, in); err != nil {
		t.Fatal(err)
	}
	out, ok, err := LoadConfig(p)
	if err != nil || !ok || out.RepoRoot != in.RepoRoot || len(out.Targets) != 2 {
		t.Fatalf("out=%+v ok=%v err=%v", out, ok, err)
	}
}

func TestValidateTargetRejectsRelativeAndRoot(t *testing.T) {
	for _, bad := range []string{"", "n", "relative/path", "/"} {
		if err := ValidateTarget(bad); err == nil {
			t.Fatalf("ValidateTarget(%q) accepted", bad)
		}
	}
	if err := ValidateTarget("/home/x/.claude/skills"); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyRepoRefusesMissingSource(t *testing.T) {
	repo := fakeRepo(t)
	os.Rename(filepath.Join(repo, "fan-out"), filepath.Join(repo, "fan-out-moved"))
	err := VerifyRepo(repo)
	if err == nil || !strings.Contains(err.Error(), "fan-out") {
		t.Fatalf("expected missing-source refusal, got %v", err)
	}
	// and Run must not have deleted anything at a target
	target := t.TempDir()
	writeFile(t, filepath.Join(target, "fan-out", "SKILL.md"), "deployed\n")
	if _, err := Run(repo, []string{target}, t.TempDir(), ModeInstall); err == nil {
		t.Fatal("Run accepted a repo with missing sources")
	}
	if _, err := os.Stat(filepath.Join(target, "fan-out", "SKILL.md")); err != nil {
		t.Fatal("deployed copy was deleted despite missing source")
	}
}

func TestRunDeploysAndIsIdempotent(t *testing.T) {
	repo, home := fakeRepo(t), t.TempDir()
	claude := filepath.Join(home, ".claude", "skills")
	other := filepath.Join(t.TempDir(), "agentx", "skills")

	rep, err := Run(repo, []string{claude, other}, home, ModeInstall)
	if err != nil || rep.Drift {
		t.Fatal(err, rep)
	}
	for _, want := range []string{
		filepath.Join(claude, "fan-out", "references", "risk-scout.md"),
		filepath.Join(other, "worklog", "SKILL.md"),
		filepath.Join(home, ".claude", "commands", "worklog.md"),
	} {
		if _, err := os.Stat(want); err != nil {
			t.Fatalf("missing %s", want)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(other), "..", ".claude")); err == nil {
		t.Fatal("command file leaked to non-claude target")
	}
	rep2, _ := Run(repo, []string{claude, other}, home, ModeInstall)
	for _, a := range rep2.Actions {
		if a.Kind == "note" && !strings.Contains(a.Text, "up to date") {
			t.Fatalf("second run not idempotent: %+v", a)
		}
	}
	// check mode after a drift
	writeFile(t, filepath.Join(claude, "contract", "SKILL.md"), "tampered\n")
	rep3, _ := Run(repo, []string{claude, other}, home, ModeCheck)
	if !rep3.Drift {
		t.Fatal("check missed tampered file")
	}
}

func TestRunMigratesLegacySymlink(t *testing.T) {
	repo, home := fakeRepo(t), t.TempDir()
	claude := filepath.Join(home, ".claude", "skills")
	os.MkdirAll(claude, 0o755)
	os.Symlink(filepath.Join(repo, "dev-context"), filepath.Join(claude, "dev-context"))

	rep, _ := Run(repo, []string{claude}, home, ModeCheck)
	found := false
	for _, a := range rep.Actions {
		if strings.Contains(a.Text, "legacy symlink") {
			found = true
		}
	}
	if !found || !rep.Drift {
		t.Fatalf("check did not flag symlink: %+v", rep.Actions)
	}
	Run(repo, []string{claude}, home, ModeInstall)
	fi, err := os.Lstat(filepath.Join(claude, "dev-context"))
	if err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatal("symlink not migrated to copy")
	}
}

func TestDryRunTouchesNothing(t *testing.T) {
	repo, home := fakeRepo(t), t.TempDir()
	claude := filepath.Join(home, ".claude", "skills")
	rep, err := Run(repo, []string{claude}, home, ModeDryRun)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Actions) == 0 || rep.Actions[0].Kind != "plan" {
		t.Fatalf("expected plan actions, got %+v", rep.Actions)
	}
	if _, err := os.Stat(claude); !os.IsNotExist(err) {
		t.Fatal("dry-run created target dir")
	}
}
