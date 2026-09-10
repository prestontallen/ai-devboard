package cli

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/installer"
	"github.com/prestontallen/ai-devboard/worklog/internal/storepath"
	"github.com/prestontallen/ai-devboard/worklog/skills"
)

// uninstallSandbox builds a home with every artifact class present, plus
// foreign entries that must survive. It returns the home.
//
// It seeds FOREIGN things deliberately and in the places they really occur on
// the maintainer's machine: a third-party skill symlink beside ours in two
// targets, and an agent directory that is detected but holds nothing of ours.
// A fixture without them would let every "we spare foreign entries" assertion
// pass vacuously.
func uninstallSandbox(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("WORKLOG_DIR", filepath.Join(home, ".local", "share", "worklog"))
	stubSystemCommands(t)

	mk := func(parts ...string) string {
		p := filepath.Join(append([]string{home}, parts...)...)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	write := func(p, s string) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	for _, target := range []string{".claude", ".cursor"} {
		for _, name := range installer.HistoricalSkillNames {
			write(filepath.Join(home, target, "skills", name, "SKILL.md"), "# "+name+"\n")
		}
		// The foreign neighbour. On the real machine this is an omarchy
		// symlink into ~/.local/share/omarchy.
		foreign := mk(".local", "share", "someone-else", "their-skill")
		if err := os.Symlink(foreign, filepath.Join(home, target, "skills", "omarchy")); err != nil {
			t.Fatal(err)
		}
	}
	// A detected agent dir holding ONLY a foreign entry.
	mk(".codex", "skills")
	other := mk(".local", "share", "someone-else", "codex-skill")
	if err := os.Symlink(other, filepath.Join(home, ".codex", "skills", "omarchy")); err != nil {
		t.Fatal(err)
	}

	for _, name := range installer.HistoricalCommandFiles {
		write(filepath.Join(home, ".claude", "commands", name), "# command\n")
	}
	write(filepath.Join(home, ".local", "bin", "worklog"), "binary")
	write(filepath.Join(home, ".config", "ai-devboard", "targets"),
		"repo /nowhere\n"+filepath.Join(home, ".claude", "skills")+"\n")
	write(filepath.Join(home, ".config", "ai-devboard", "consent"), "skills accepted\n")
	write(filepath.Join(home, ".config", "systemd", "user", "devboard.service"), "[Unit]\n")
	write(filepath.Join(home, ".config", "systemd", "user", "default.target.wants", "devboard.service"), "")
	write(filepath.Join(home, ".claude", "settings.json"), `{"hooks":{}}`)
	write(filepath.Join(home, ".claude", "CLAUDE.md"), "prose\n")
	mk(".local", "share", "worklog")
	mk(".local", "share", "contracts")
	return home
}

func runUninstallCmd(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	return runCLIAllowErr(t, append([]string{"uninstall"}, args...)...)
}

// TestUninstallDryRunByDefault is the property the whole command rests on.
func TestUninstallDryRunByDefault(t *testing.T) {
	home := uninstallSandbox(t)
	out, _, err := runUninstallCmd(t)
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if !strings.Contains(out, "dry run") {
		t.Errorf("output does not say it was a dry run:\n%s", out)
	}
	for _, p := range []string{
		filepath.Join(home, ".local", "bin", "worklog"),
		filepath.Join(home, ".claude", "skills", "dev-context", "SKILL.md"),
		filepath.Join(home, ".claude", "commands", "uc:worklog.md"),
		filepath.Join(home, ".config", "ai-devboard", "consent"),
		filepath.Join(home, ".config", "systemd", "user", "devboard.service"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("dry run removed %s", p)
		}
	}
	// It must also NAME what it found, or the plan is not a plan.
	for _, want := range []string{
		"dev-context",
		"uc:worklog.md",
		"devboard.service",
		filepath.Join(home, ".local", "bin", "worklog"),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("plan never mentions %q:\n%s", want, out)
		}
	}
}

// TestUninstallNeedsNoCheckout pins the property that distinguishes this
// command from install: it runs with no repo recorded, none nearby, and no
// worklog data directory.
func TestUninstallNeedsNoCheckout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("WORKLOG_DIR", filepath.Join(home, "nonexistent-corpus"))
	stubSystemCommands(t)

	out, stderr, err := runUninstallCmd(t)
	if err != nil {
		t.Fatalf("uninstall on a bare machine failed: %v (stderr %s)", err, stderr)
	}
	if !strings.Contains(out, "dry run") {
		t.Errorf("expected a clean dry run on a bare machine:\n%s", out)
	}
	if strings.Contains(out+stderr, "re-run install.sh") || strings.Contains(out+stderr, "no repo recorded") {
		t.Errorf("uninstall demanded a checkout:\n%s\n%s", out, stderr)
	}
}

// TestUninstallDataPathsFollowEnv proves the inventory is derivation rules
// rather than compiled-in paths. A machine that moved its corpus must see the
// moved corpus listed, not the default one.
func TestUninstallDataPathsFollowEnv(t *testing.T) {
	home := uninstallSandbox(t)
	moved := filepath.Join(home, "elsewhere", "corpus")
	if err := os.MkdirAll(moved, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(storepath.Dir(moved), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKLOG_DIR", moved)

	out, _, err := runUninstallCmd(t)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, moved) {
		t.Errorf("relocated corpus %s not listed:\n%s", moved, out)
	}
	if !strings.Contains(out, storepath.Dir(moved)) {
		t.Errorf("store derived from the relocated corpus (%s) not listed:\n%s",
			storepath.Dir(moved), out)
	}
	if strings.Contains(out, filepath.Join(home, ".local", "share", "worklog-store")) {
		t.Errorf("listed the DEFAULT store path despite WORKLOG_DIR moving it:\n%s", out)
	}
}

// TestUninstallPreservesData covers the default disposition and the one
// directory that must never appear at all.
func TestUninstallPreservesData(t *testing.T) {
	home := uninstallSandbox(t)
	scratch := filepath.Join(home, ".local", "share", "worklog-scratch")
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scratch, "mine.py"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _, err := runUninstallCmd(t)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "preserved") {
		t.Errorf("data not described as preserved:\n%s", out)
	}
	// worklog-scratch is the human's. It is not ours to list, offer, or
	// mention: naming it in a removal plan invites a yes about someone
	// else's files.
	if strings.Contains(out, "worklog-scratch") {
		t.Errorf("worklog-scratch appears in the plan; it is out of scope entirely:\n%s", out)
	}
	if _, err := os.Stat(scratch); err != nil {
		t.Errorf("worklog-scratch was touched: %v", err)
	}
}

// TestUninstallSparesForeignSkills is the assertion the fixture's foreign
// symlinks exist to make non-vacuous.
func TestUninstallSparesForeignSkills(t *testing.T) {
	home := uninstallSandbox(t)
	out, _, err := runUninstallCmd(t)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{".claude", ".cursor", ".codex"} {
		foreign := filepath.Join(home, target, "skills", "omarchy")
		if strings.Contains(out, foreign) {
			t.Errorf("plan names a foreign skill %s", foreign)
		}
		if _, err := os.Lstat(foreign); err != nil {
			t.Errorf("foreign skill %s disappeared: %v", foreign, err)
		}
	}
}

// TestUninstallRefusesUnrelocatedStore is the blocker the delta scout found.
// A database at the old path with none at the new one is not leftovers: it is
// the machine's only store, and requireStore tells the human to relocate it.
func TestUninstallRefusesUnrelocatedStore(t *testing.T) {
	home := uninstallSandbox(t)
	legacy := filepath.Join(home, ".local", "share", "worklog-migration")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storepath.DBIn(legacy), []byte("not really sqlite"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Deliberately NO database at the new path.

	out, stderr, err := runUninstallCmd(t)
	if err == nil {
		t.Fatalf("uninstall offered to remove an unrelocated store:\n%s", out)
	}
	if !strings.Contains(out+stderr+err.Error(), "store relocate") {
		t.Errorf("refusal does not name the remedy: %v\n%s\n%s", err, out, stderr)
	}
}

// TestUninstallLegacyOnePrompt covers the reporting half of the legacy offer:
// one directory, named contents, no removal without an answer.
func TestUninstallLegacyOnePrompt(t *testing.T) {
	home := uninstallSandbox(t)
	legacy := filepath.Join(home, ".local", "share", "worklog-migration")
	if err := os.MkdirAll(filepath.Join(legacy, "rollback"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "rollback", "worklog-pre-cutover"),
		[]byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "worklog.db.bak"), []byte("bak"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A live store at the new path proves the machine really did migrate.
	newStore := storepath.Dir(filepath.Join(home, ".local", "share", "worklog"))
	if err := os.MkdirAll(newStore, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storepath.DBIn(newStore), []byte("db"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _, err := runUninstallCmd(t)
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if !strings.Contains(out, "superseded layouts") {
		t.Errorf("legacy directory not reported as its own class:\n%s", out)
	}
	// The human must see what the single yes would cost.
	if !strings.Contains(out, "rollback") {
		t.Errorf("report never names the rollback binaries:\n%s", out)
	}
	if !strings.Contains(out, "database backup") {
		t.Errorf("report never names the database backups:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(legacy, "rollback", "worklog-pre-cutover")); err != nil {
		t.Errorf("legacy content removed without an answer: %v", err)
	}
}

// TestUninstallRefusesRetiredEnv: the retired variable is refused everywhere
// else, and uninstall is a bad place to make the one exception, since it would
// be honoring it for deletion.
func TestUninstallRefusesRetiredEnv(t *testing.T) {
	uninstallSandbox(t)
	t.Setenv(storepath.LegacyEnv, "/tmp/somewhere")
	out, stderr, err := runUninstallCmd(t)
	if err == nil {
		t.Fatalf("uninstall ran with %s set:\n%s", storepath.LegacyEnv, out)
	}
	if !strings.Contains(out+stderr+err.Error(), storepath.LegacyEnv) {
		t.Errorf("refusal does not name the variable: %v\n%s\n%s", err, out, stderr)
	}
}

// TestUninstallFlagsAreExclusive covers the contradiction, not a precedence
// question: picking a winner would do the opposite of half the command line.
func TestUninstallFlagsAreExclusive(t *testing.T) {
	uninstallSandbox(t)
	_, _, err := runUninstallCmd(t, "--purge-legacy", "--keep-legacy")
	if err == nil {
		t.Fatal("expected a refusal for --purge-legacy with --keep-legacy")
	}
}

// TestInventoryMatchesRepo stops the tests from both defining and verifying
// the artifact set.
//
// The inventory's skill names are checked against the repo's actual top-level
// skill directories, walked from disk. Without this, the inventory and the
// fixture that exercises it are the same assertion written twice, and a skill
// added to the repo would be missed by uninstall with every test still green.
func TestInventoryMatchesRepo(t *testing.T) {
	// Read the shipped set from the embedded FS, not from a relative repo
	// path. Walking "../../.." for top-level SKILL.md dirs stopped finding
	// anything once the skills moved under worklog/, and the walk's own
	// "not a checkout" skip would have turned that into a silent pass —
	// the exact both-define-and-verify hole this test exists to close.
	entries, err := fs.ReadDir(skills.FS, ".")
	if err != nil {
		t.Fatalf("reading the embedded skills FS: %v", err)
	}
	shipped := map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := fs.Stat(skills.FS, path.Join(e.Name(), "SKILL.md")); err == nil {
			shipped[e.Name()] = true
		}
	}
	if len(shipped) == 0 {
		t.Fatal("no skills are embedded; the inventory cannot be checked against anything")
	}
	known := map[string]bool{}
	for _, n := range installer.HistoricalSkillNames {
		known[n] = true
	}
	for name := range shipped {
		if !known[name] {
			t.Errorf("repo deploys skill %q but the uninstall inventory does not know it; "+
				"a machine would keep it forever", name)
		}
	}
	// worklog is deployed like the rest now that every skill lives under
	// worklog/skills/, so the walk covers it. Kept as an explicit assertion
	// because it is the one whose source dir also holds claude/command.md.
	if !known["worklog"] {
		t.Error("inventory omits the worklog skill")
	}
}

// TestRemovalSeamCoversEveryDelete is the guard that keeps a test from ever
// reaching the developer's real files.
func TestRemovalSeamCoversEveryDelete(t *testing.T) {
	src, err := os.ReadFile("uninstall.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	// The seam declaration itself is the one legitimate mention.
	body = strings.Replace(body, "removeAll  = os.RemoveAll", "", 1)
	body = strings.Replace(body, "removeFile = os.Remove", "", 1)
	for _, banned := range []string{"os.RemoveAll(", "os.Remove("} {
		if strings.Contains(body, banned) {
			t.Errorf("uninstall.go calls %s directly; it must go through the seam "+
				"so no test can reach the real filesystem", banned)
		}
	}
}

// TestUninstallRunsUnderFreeze covers the exemption.
//
// A freeze is a hold on the DATA, and the data is what uninstall preserves by
// default. Refusing to uninstall because one is held would strand a machine on
// a sentinel file whose own release command is part of what is being removed,
// and freezeExemptCommands is default-deny, so the entry has to be explicit.
func TestUninstallRunsUnderFreeze(t *testing.T) {
	home := uninstallSandbox(t)
	corpus := filepath.Join(home, ".local", "share", "worklog")
	if err := os.WriteFile(filepath.Join(corpus, ".freeze"),
		[]byte(`{"pid":1,"reason":"test","acquired":"2026-09-09T00:00:00Z"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	out, stderr, err := runUninstallCmd(t)
	if err != nil {
		t.Fatalf("uninstall refused while a freeze was held: %v (stderr %s)", err, stderr)
	}
	if !strings.Contains(out, "dry run") {
		t.Errorf("expected a normal dry run under a freeze:\n%s", out)
	}
	if strings.Contains(out+stderr, "frozen") {
		t.Errorf("uninstall reported a freeze refusal:\n%s\n%s", out, stderr)
	}
}
