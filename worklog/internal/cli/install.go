package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/prestontallen/ai-devboard/worklog/internal/installer"
	"github.com/prestontallen/ai-devboard/worklog/internal/style"
	"github.com/prestontallen/ai-devboard/worklog/internal/version"
)

func newInstallCmd() *cobra.Command {
	var (
		flagRepo     string
		flagRelate   string
		flagCheck    bool
		flagDryRun   bool
		flagWithHook bool
	)
	cmd := &cobra.Command{
		Use:   "install",
		Args:  cobra.NoArgs,
		Short: "Deploy skills, config, and devboard setup (the install.sh brain)",
		Long: `install is the Go side of ./install.sh: target selection (interactive
multi-select on a TTY; saved config or detection otherwise), skill
deployment to every target, drift checking, and the opt-in extras.

Config lives at ` + "`~/.config/ai-devboard/targets`" + ` — the bash-era
plain-path format still parses; the 'repo <path>' line records the
checkout so --check works from anywhere.

Modes: (default) install/update · --check report drift, exit 1 · --dry-run
narrate, touch nothing. Check and dry-run never write the config, never
prompt, never build.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// --relate answers one question and exits: how would moving from
			// this binary's version to the given one go? It lives here, in
			// the binary that knows its own stamp, because the shell had no
			// comparison at all — only string equality — and that is what
			// let a nine-commits-newer build be replaced by an old release.
			// The shell now asks rather than guesses.
			if flagRelate != "" {
				fmt.Fprintln(cmd.OutOrStdout(),
					string(version.Relate(version.Parse(BuildVersion()), version.Parse(flagRelate))))
				return nil
			}
			if flagCheck && flagDryRun {
				return errWithExit(64, "cannot combine --check and --dry-run")
			}
			if flagCheck && flagWithHook {
				return errWithExit(64, "cannot combine --check and --with-session-hook (--check never writes)")
			}
			mode := installer.ModeInstall
			if flagCheck {
				mode = installer.ModeCheck
			}
			if flagDryRun {
				mode = installer.ModeDryRun
			}
			return runInstall(cmd, flagRepo, mode, flagWithHook)
		},
	}
	cmd.Flags().StringVar(&flagRelate, "relate", "",
		"compare this binary's version against the given one and print upgrade|downgrade|same|unknown, then exit")
	cmd.Flags().StringVar(&flagRepo, "repo", "", "path to the ai-devboard checkout (persisted; usually passed by install.sh)")
	cmd.Flags().BoolVar(&flagCheck, "check", false, "report drift; exit 1 if anything differs")
	cmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "print what would happen; change nothing")
	cmd.Flags().BoolVar(&flagWithHook, "with-session-hook", false,
		"install the Claude Code SessionStart hook without prompting (the headless way to opt in)")
	return cmd
}

// resolveRepoRoot returns the Go module root at or above the cwd. It is the
// last resort in runInstall's repo resolution, behind --repo and the config's
// 'repo' line; the checkout is its parent, since the module root is worklog/.
func resolveRepoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return installer.FindRepoRoot(cwd)
}

// promptAllowed gates every TUI: huh/bubbletea do NOT check for a TTY
// themselves (with piped stdin they silently open /dev/tty and take over
// the terminal), so the guard must be ours. INSTALL_PROMPT_FORCE is the
// test seam.
func promptAllowed() bool {
	return stdinIsTTY() || os.Getenv("INSTALL_PROMPT_FORCE") != ""
}

func runInstall(cmd *cobra.Command, repoFlag string, mode installer.Mode, withHook bool) error {
	out := cmd.OutOrStdout()
	errw := cmd.ErrOrStderr()
	home, err := os.UserHomeDir()
	if err != nil {
		return errWithExit(1, "resolve home: %v", err)
	}
	confPath := installer.ConfPath()
	cfg, hadConfig, err := installer.LoadConfig(confPath)
	if err != nil {
		return errWithExit(1, "read config %s: %v", confPath, err)
	}

	// Resolve the repo root: flag > config > cwd checkout.
	repoRoot := strings.TrimSpace(repoFlag)
	if repoRoot == "" {
		repoRoot = cfg.RepoRoot
	}
	if repoRoot == "" {
		if root, err := resolveRepoRoot(); err == nil {
			repoRoot = filepath.Dir(root) // resolveRepoRoot returns worklog/; repo is its parent
		}
	}
	if repoRoot == "" {
		return errWithExit(1, "no repo recorded and none found; re-run install.sh from an ai-devboard checkout (or pass --repo)")
	}
	if err := installer.VerifyRepo(repoRoot); err != nil {
		return errWithExit(1, "%v", err)
	}

	// Self-staleness: rebuild-and-exec so the run continues on the fresh
	// binary (a process cannot replace its own executable and carry on).
	//
	// This used to skip the comparison for anything whose version string did
	// not literally contain "-dev" or "-snapshot" — the same substring test
	// that, on the bootstrap side, let a build nine commits ahead be replaced
	// by the release it contained. Both halves failed identically because
	// they asked the same badly-posed question.
	//
	// The question that matters here is different from the bootstrap's, and
	// worth stating: not "which is newer" but "was this binary built from
	// THIS checkout". A commit mismatch is the whole test. What the version
	// stamp is for is deciding whether a rebuild is even appropriate — a
	// binary someone downloaded should not be silently rebuilt out from
	// under them just because a checkout happens to sit nearby.
	// WORKLOG_INSTALL_REEXEC breaks rebuild loops on racing dirty trees.
	fromRelease := version.Parse(BuildVersion()).IsRelease()
	if rev, err := installer.RepoRev(repoRoot); err == nil && !fromRelease && rev != BuildCommit() {
		switch mode {
		case installer.ModeCheck:
			fmt.Fprintln(out, style.Bad.Render(fmt.Sprintf(
				"drift: worklog binary: have %q, want %q", BuildCommit(), rev)))
		case installer.ModeDryRun:
			fmt.Fprintln(out, "would: rebuild worklog at "+rev)
		case installer.ModeInstall:
			if os.Getenv("WORKLOG_INSTALL_REEXEC") != "" {
				fmt.Fprintln(errw, style.Warn.Render(
					"WARN: still stale after rebuild (tree changing?); continuing"))
			} else if selfPath, err := os.Executable(); err == nil {
				if err := rebuildSelf(repoRoot, rev, selfPath); err != nil {
					fmt.Fprintln(errw, style.Warn.Render("WARN: self-rebuild failed: "+err.Error()))
				} else {
					fmt.Fprintln(out, style.Good.Render("rebuilt worklog at "+rev+"; continuing on fresh binary"))
					env := append(os.Environ(), "WORKLOG_INSTALL_REEXEC=1")
					if err := syscall.Exec(selfPath, os.Args, env); err != nil {
						fmt.Fprintln(errw, style.Warn.Render("WARN: exec failed; re-run worklog install"))
					}
				}
			}
		}
	}

	// Resolve targets: config > interactive prompt > detection.
	targets := cfg.Targets
	prompted := false
	switch {
	case hadConfig && len(targets) > 0:
		fmt.Fprintln(out, style.Dim.Render("targets: from config ("+strings.Join(targets, ", ")+")"))
	case mode == installer.ModeInstall && promptAllowed():
		targets, err = promptForTargets(home)
		if err != nil {
			return errWithExit(1, "target selection: %v", err)
		}
		prompted = true
	default:
		targets = installer.DetectTargets(home)
		fmt.Fprintln(out, style.Dim.Render("targets: detected agent dirs (no config yet; run interactively to choose)"))
	}
	// Zero targets is an ERROR, never a silent success: huh's accessible
	// mode (TERM=dumb) returns empty selections on EOF without erroring.
	if len(targets) == 0 {
		return errWithExit(1, "no install targets selected or detected; nothing would be deployed")
	}
	for _, t := range targets {
		if err := installer.ValidateTarget(t); err != nil {
			return errWithExit(64, "invalid target: %v", err)
		}
	}

	// Persist config: only from an interactive selection, or to add the
	// repo line to an existing config. Never in check/dry-run.
	if mode == installer.ModeInstall {
		if prompted || (hadConfig && cfg.RepoRoot != repoRoot) {
			if err := installer.SaveConfig(confPath, installer.Config{RepoRoot: repoRoot, Targets: targets}); err != nil {
				fmt.Fprintln(errw, style.Warn.Render("WARN: could not save config: "+err.Error()))
			} else if prompted {
				fmt.Fprintln(out, style.Dim.Render("targets saved: "+confPath))
			}
		}
	}

	rep, err := installer.Run(repoRoot, targets, home, mode)
	if err != nil {
		return errWithExit(1, "%v", err)
	}
	for _, a := range rep.Actions {
		switch a.Kind {
		case "note":
			fmt.Fprintln(out, a.Text)
		case "plan":
			fmt.Fprintln(out, "would: "+a.Text)
		case "stale":
			fmt.Fprintln(out, style.Bad.Render("drift: "+a.Text))
		case "warn":
			fmt.Fprintln(errw, style.Warn.Render("WARN: "+a.Text))
		}
	}

	installExtras(cmd, home, repoRoot, mode, &rep, withHook)

	if mode == installer.ModeCheck {
		if rep.Drift {
			fmt.Fprintln(out, "check: drift found")
			return errWithExit(1, "")
		}
		fmt.Fprintln(out, style.Good.Render("check: everything current"))
	}
	if mode == installer.ModeDryRun {
		fmt.Fprintln(out, "dry-run: nothing changed")
	}
	return nil
}

// promptForTargets shows the huh multi-select (detected dirs pre-checked)
// plus a custom-path input. Only reachable through promptAllowed.
func promptForTargets(home string) ([]string, error) {
	detected := installer.DetectTargets(home)
	opts := make([]huh.Option[string], len(detected))
	for i, d := range detected {
		opts[i] = huh.NewOption(d, d).Selected(true)
	}
	selected := append([]string(nil), detected...)
	var custom string
	form := huh.NewForm(huh.NewGroup(
		huh.NewMultiSelect[string]().
			Title("Deploy skills to which agents?").
			Options(opts...).
			Value(&selected),
		huh.NewInput().
			Title("Additional paths (comma-separated, blank for none)").
			Value(&custom),
	))
	if err := form.Run(); err != nil {
		return nil, err
	}
	for _, c := range strings.Split(custom, ",") {
		c = installer.ExpandTilde(strings.TrimSpace(c))
		if c == "" {
			continue
		}
		if err := installer.ValidateTarget(c); err != nil {
			return nil, err
		}
		selected = append(selected, c)
	}
	return selected, nil
}

// installExtras: PATH warning, tone check, devboard dir, opt-in prompts.
func installExtras(cmd *cobra.Command, home, repoRoot string, mode installer.Mode, rep *installer.Report, withHook bool) {
	out := cmd.OutOrStdout()
	errw := cmd.ErrOrStderr()

	binDir := filepath.Join(home, ".local", "bin")
	if !strings.Contains(":"+os.Getenv("PATH")+":", ":"+binDir+":") {
		fmt.Fprintln(errw, style.Warn.Render("WARN: "+binDir+" is not on PATH — add it to your shell profile"))
	}

	toneGlob := filepath.Join(home, ".claude", "skills", "*tone*")
	if m, _ := filepath.Glob(toneGlob); len(m) > 0 {
		if mode == installer.ModeInstall {
			fmt.Fprintln(out, style.Dim.Render("tone skill: "+filepath.Base(m[0])+" found"))
		}
	} else {
		fmt.Fprintln(errw, style.Warn.Render("WARN: no personal *tone* skill installed — dev-context ship phase falls back to its default voice"))
	}

	devboardDir := os.Getenv("DEVBOARD_DATA")
	if devboardDir == "" {
		devboardDir = filepath.Join(home, ".local", "share", "devboard")
	}
	if fi, err := os.Stat(devboardDir); err != nil || !fi.IsDir() {
		switch mode {
		case installer.ModeCheck:
			fmt.Fprintln(out, style.Bad.Render("drift: devboard data dir missing: "+devboardDir))
			rep.Drift = true
		case installer.ModeDryRun:
			fmt.Fprintln(out, "would: create "+devboardDir)
		case installer.ModeInstall:
			if err := os.MkdirAll(devboardDir, 0o755); err == nil {
				fmt.Fprintln(out, "devboard data dir: created "+devboardDir)
			}
		}
	}

	reportHookState(cmd, home, mode, rep, withHook)

	// Opt-in extras: interactive install mode only.
	if mode != installer.ModeInstall || !promptAllowed() {
		if mode == installer.ModeInstall {
			fmt.Fprintln(out, style.Dim.Render("hint: rerun interactively to opt into the SessionStart hook / CLAUDE.md directive / devboard service"))
		}
		return
	}
	offerSessionStartHook(cmd, home)
	claudeMD := filepath.Join(home, ".claude", "CLAUDE.md")
	if data, _ := os.ReadFile(claudeMD); !strings.Contains(string(data), "dev-context") {
		var yes bool
		if huh.NewForm(huh.NewGroup(huh.NewConfirm().
			Title("Append the dev-context directive to ~/.claude/CLAUDE.md?").
			Value(&yes))).Run() == nil && yes {
			if src, err := os.ReadFile(filepath.Join(repoRoot, "CLAUDE.md")); err == nil {
				f, err := os.OpenFile(claudeMD, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
				if err == nil {
					f.Write(src)
					f.Close()
					fmt.Fprintln(out, "CLAUDE.md directive: appended")
				}
			}
		}
	}
	if !devboardRunning() {
		var yes bool
		if huh.NewForm(huh.NewGroup(huh.NewConfirm().
			Title("Install and start the devboard service (systemd user unit running `worklog serve`)?").
			Value(&yes))).Run() == nil && yes {
			if err := installDevboardUnit(); err != nil {
				fmt.Fprintln(errw, "devboard service:", err)
			} else {
				fmt.Fprintln(out, "devboard: running at http://localhost:8484")
			}
		}
	}
}

// devboardRunning reports whether something already serves the board: the
// systemd user unit, or a container (the compose fallback, or the retired
// Python deployment still supervising itself).
// runCommand is the seam for shelling out to the machine's service manager.
//
// It exists because the devboard paths call real systemctl and real docker,
// and today they are only unreachable under test by accident: the extras sit
// behind promptAllowed(), and go test never has a TTY. The moment any of them
// gains a headless flag — which the consent ticket will add — the suite would
// start reloading the developer's systemd units and inspecting their
// containers. A package-level var is the smallest thing that makes that
// impossible rather than merely unlikely.
//
// installer.HookBinPath already avoids os.Executable() for exactly this
// reason; installDevboardUnit did not, and does now.
var runCommand = func(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

// selfPath is the seam for os.Executable(). Under go test that returns the
// test binary in the build cache, so a unit file written from it would name a
// path that disappears.
var selfPath = os.Executable

func devboardRunning() bool {
	if state, err := runCommand("systemctl", "--user", "is-active", "devboard.service"); err == nil &&
		strings.TrimSpace(string(state)) == "active" {
		return true
	}
	if _, err := exec.LookPath("docker"); err == nil {
		ps, _ := runCommand("docker", "ps", "--format", "{{.Names}}")
		if strings.Contains(string(ps), "devboard") {
			return true
		}
	}
	return false
}

const devboardUnit = `[Unit]
Description=Devboard dashboard (worklog serve)

[Service]
ExecStart=%s serve
Restart=on-failure

[Install]
WantedBy=default.target
`

// installDevboardUnit writes a systemd user unit pointing at this binary
// and enables it now. Pointing at the installed path (not a copied
// binary) means upgrades take effect on the unit's next restart.
func installDevboardUnit() error {
	bin, err := selfPath()
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	unitDir := filepath.Join(home, ".config", "systemd", "user")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		return err
	}
	unitPath := filepath.Join(unitDir, "devboard.service")
	if err := os.WriteFile(unitPath, []byte(fmt.Sprintf(devboardUnit, bin)), 0o644); err != nil {
		return err
	}
	if _, err := runCommand("systemctl", "--user", "daemon-reload"); err != nil {
		return fmt.Errorf("daemon-reload: %w", err)
	}
	if _, err := runCommand("systemctl", "--user", "enable", "--now", "devboard.service"); err != nil {
		return fmt.Errorf("enable --now: %w", err)
	}
	return nil
}

// reportHookState describes the SessionStart hook in every mode. Absence is
// a note, never drift: the hook is opt-in, and a human who declined it must
// still get a clean `install --check`. A stale entry — one naming a binary
// that is not the installed one — IS drift, because it silently stops
// working. Check and dry-run never open settings.json for writing.
//
// withHook is the headless consent path: typing --with-session-hook is the
// same act as answering the prompt, so it installs an absent hook without a
// TTY. Without it, an absent hook is left to offerSessionStartHook, which
// only runs interactively.
func reportHookState(cmd *cobra.Command, home string, mode installer.Mode, rep *installer.Report, withHook bool) {
	out := cmd.OutOrStdout()
	settings := installer.SettingsPath(home)
	want := installer.HookCommand(installer.HookBinPath(home))

	state, found, err := installer.InspectHook(settings, want)
	if err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), style.Warn.Render("WARN: SessionStart hook: "+err.Error()+"; leaving it alone"))
		return
	}
	switch state {
	case installer.HookCurrent:
		if mode == installer.ModeInstall {
			fmt.Fprintln(out, style.Dim.Render("SessionStart hook: installed"))
		}
	case installer.HookStale:
		msg := fmt.Sprintf("SessionStart hook in %s names %q, want %q", settings, found, want)
		switch mode {
		case installer.ModeCheck:
			fmt.Fprintln(out, style.Bad.Render("drift: "+msg))
			rep.Drift = true
		case installer.ModeDryRun:
			fmt.Fprintln(out, "would: repair "+msg)
		case installer.ModeInstall:
			if err := installer.InstallHook(settings, want); err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), style.Warn.Render("WARN: SessionStart hook: "+err.Error()))
			} else {
				fmt.Fprintln(out, "SessionStart hook: repaired to "+want)
			}
		}
	case installer.HookAbsent:
		switch {
		case mode == installer.ModeDryRun && withHook:
			fmt.Fprintln(out, "would: add the SessionStart hook to "+settings)
		case mode == installer.ModeInstall && withHook:
			if err := installer.InstallHook(settings, want); err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), style.Warn.Render("WARN: SessionStart hook: "+err.Error()))
			} else {
				fmt.Fprintln(out, "SessionStart hook: added to "+settings)
			}
		case mode != installer.ModeInstall:
			fmt.Fprintln(out, style.Dim.Render(
				"note: SessionStart hook not installed (opt-in; add it with --with-session-hook or an interactive run)"))
		}
	}
}

// offerSessionStartHook prompts once, on an interactive install, before
// writing to a file the human owns. Declining is remembered only in the
// sense that nothing is written — re-running install asks again.
func offerSessionStartHook(cmd *cobra.Command, home string) {
	out := cmd.OutOrStdout()
	settings := installer.SettingsPath(home)
	want := installer.HookCommand(installer.HookBinPath(home))

	state, _, err := installer.InspectHook(settings, want)
	if err != nil || state != installer.HookAbsent {
		return // malformed (already warned), current, or repaired above
	}
	var yes bool
	if huh.NewForm(huh.NewGroup(huh.NewConfirm().
		Title("Add the worklog SessionStart hook to ~/.claude/settings.json?").
		Description("Injects a short orientation block at the start of every Claude Code session.").
		Value(&yes))).Run() != nil || !yes {
		return
	}
	if err := installer.InstallHook(settings, want); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), style.Warn.Render("WARN: SessionStart hook: "+err.Error()))
		return
	}
	fmt.Fprintln(out, "SessionStart hook: added to "+settings)
}

// rebuildSelf builds the worklog binary at rev into selfPath, stamped with
// the same ldflags shape the bootstrap uses.
func rebuildSelf(repoRoot, rev, selfPath string) error {
	date := time.Now().UTC().Format("2006-01-02")
	build := exec.Command("go", "build",
		"-ldflags", fmt.Sprintf("-X main.version=%s -X main.commit=%s -X main.date=%s",
			BuildVersion(), rev, date),
		"-o", selfPath, "./cmd/worklog")
	build.Dir = filepath.Join(repoRoot, "worklog")
	outB, err := build.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(outB)))
	}
	return nil
}
