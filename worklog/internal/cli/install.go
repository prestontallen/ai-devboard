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
		flagRepo   string
		flagRelate string
		flagCheck  bool
		flagDryRun bool
		// One flag per extra, keyed by the same names the consent record
		// uses. A map rather than adjacent bools: the previous single flag
		// was threaded positionally through three call frames, and four of
		// those at one call site is a defect waiting to happen.
		flagWith = map[string]*bool{
			installer.ExtraSkills:       new(bool),
			installer.ExtraSessionHook:  new(bool),
			installer.ExtraClaudeMD:     new(bool),
			installer.ExtraDevboardUnit: new(bool),
		}
		// A decline is a decision, and needs the same headless, unfakeable
		// form as an acceptance. Without these there were four ways to say
		// yes and none to say no except answering a prompt — which is not a
		// headless answer at all.
		flagWithout = map[string]*bool{
			installer.ExtraSkills:       new(bool),
			installer.ExtraSessionHook:  new(bool),
			installer.ExtraClaudeMD:     new(bool),
			installer.ExtraDevboardUnit: new(bool),
		}
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
			// One rule over the whole set, stated once. Previously this was
			// a hand-written pairwise check covering exactly one of the
			// combinations, and --dry-run accepted the same flag that
			// --check rejected.
			for _, extra := range installer.Extras() {
				// Both flags for one extra is a contradiction, not a
				// precedence question. Picking a winner would silently do
				// the opposite of what half the command line asked for.
				if *flagWith[extra] && *flagWithout[extra] {
					return errWithExit(64, "cannot combine %s and %s",
						extraFlag[extra], extraFlagNo[extra])
				}
				// Both kinds WRITE — a decline is recorded, which is a write
				// like any other — so --check rejects them equally.
				for _, f := range []struct {
					set  bool
					name string
				}{{*flagWith[extra], extraFlag[extra]}, {*flagWithout[extra], extraFlagNo[extra]}} {
					if flagCheck && f.set {
						return errWithExit(64,
							"cannot combine --check and %s (--check never writes)", f.name)
					}
				}
			}
			mode := installer.ModeInstall
			if flagCheck {
				mode = installer.ModeCheck
			}
			if flagDryRun {
				mode = installer.ModeDryRun
			}
			return runInstall(cmd, flagRepo, mode, chosenExtras(flagWith), chosenExtras(flagWithout))
		},
	}
	cmd.Flags().StringVar(&flagRelate, "relate", "",
		"compare this binary's version against the given one and print upgrade|downgrade|same|unknown, then exit")
	cmd.Flags().StringVar(&flagRepo, "repo", "", "path to the ai-devboard checkout (persisted; usually passed by install.sh)")
	cmd.Flags().BoolVar(&flagCheck, "check", false, "report drift; exit 1 if anything differs")
	cmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "print what would happen; change nothing")
	cmd.Flags().BoolVar(flagWith[installer.ExtraSkills], "with-skills", false,
		"deploy the skills to every configured target without prompting")
	cmd.Flags().BoolVar(flagWith[installer.ExtraSessionHook], "with-session-hook", false,
		"install the Claude Code SessionStart hook without prompting")
	cmd.Flags().BoolVar(flagWith[installer.ExtraClaudeMD], "with-claude-md", false,
		"write the dev-context directive into ~/.claude/CLAUDE.md without prompting")
	cmd.Flags().BoolVar(flagWith[installer.ExtraDevboardUnit], "with-devboard-service", false,
		"install and start the devboard systemd user unit without prompting")
	cmd.Flags().BoolVar(flagWithout[installer.ExtraSkills], "no-skills", false,
		"record that skills should not be deployed, without prompting")
	cmd.Flags().BoolVar(flagWithout[installer.ExtraSessionHook], "no-session-hook", false,
		"record that the SessionStart hook is not wanted, without prompting")
	cmd.Flags().BoolVar(flagWithout[installer.ExtraClaudeMD], "no-claude-md", false,
		"record that the CLAUDE.md directive is not wanted, without prompting")
	cmd.Flags().BoolVar(flagWithout[installer.ExtraDevboardUnit], "no-devboard-service", false,
		"record that the devboard service is not wanted, without prompting")
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

func runInstall(cmd *cobra.Command, repoFlag string, mode installer.Mode, with, without map[string]bool) error {
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

	// Skills are an extra like the other three: asked once, recorded,
	// declinable. Until this, they deployed to every detected agent dir
	// without asking, so "install the binary and nothing else" was not a
	// state you could ask for.
	consentPath := installer.ConsentPath()
	consent, _ := installer.LoadConsent(consentPath)

	// A machine that already has a config with targets chose skills at some
	// point, before there was anywhere to record it. Reading that as
	// accepted is what stops every existing machine being asked once for
	// something it plainly already has.
	consentDirty := false
	if !consent.Asked(installer.ExtraSkills) && hadConfig && len(cfg.Targets) > 0 {
		consent.Record(installer.ExtraSkills, installer.Accepted)
		consentDirty = true
	}
	if without[installer.ExtraSkills] && !consent.Asked(installer.ExtraSkills) {
		consent.Record(installer.ExtraSkills, installer.Declined)
		consentDirty = true
	}
	if with[installer.ExtraSkills] && !consent.Accepted(installer.ExtraSkills) {
		consent.Record(installer.ExtraSkills, installer.Accepted)
		consentDirty = true
	}

	// A config that exists and names no targets is a decision somebody
	// wrote down, not an absence of one. Falling through to detection here
	// is how a deliberate empty choice got overridden.
	if hadConfig && len(cfg.Targets) == 0 && !consent.Asked(installer.ExtraSkills) {
		consent.Record(installer.ExtraSkills, installer.Declined)
		consentDirty = true
	}

	var rep installer.Report
	if consent.Asked(installer.ExtraSkills) && !consent.Accepted(installer.ExtraSkills) {
		fmt.Fprintln(out, style.Dim.Render("skills: declined — not deployed (delete the line in "+consentPath+" to be asked again)"))
		if consentPath != "" {
			_ = installer.SaveConsent(consentPath, consent)
		}
		installExtras(cmd, home, repoRoot, mode, &rep, with, without)
		return finishInstall(cmd, mode, rep)
	}

	// Resolve targets: config > interactive prompt > detection.
	targets := cfg.Targets
	prompted := false
	switch {
	case hadConfig && len(targets) > 0:
		fmt.Fprintln(out, style.Dim.Render("targets: from config ("+strings.Join(targets, ", ")+")"))
	case mode == installer.ModeInstall && promptAllowed():
		// Ask whether to deploy skills at all, before asking where.
		//
		// Note what does NOT protect us here, because it is tempting to
		// think it does: no prompt shape is end-of-input-proof. A Confirm
		// returns its default; a Select returns its first option. Measured,
		// not assumed — an earlier version of this recorded "accepted" for a
		// prompt nobody saw.
		//
		// The guard is that nothing is RECORDED until there are targets to
		// deploy to. An unanswered prompt therefore falls through to the
		// zero-target error and decides nothing, which is what lets the
		// human be asked again next time.
		answer := ""
		_ = huh.NewForm(huh.NewGroup(huh.NewSelect[string]().
			Title("Deploy the skills to your agent directories?").
			Description("Declining installs the binary only.").
			Options(
				huh.NewOption("Yes — deploy skills", "yes"),
				huh.NewOption("No — binary only", "no"),
			).
			Value(&answer))).Run()

		switch answer {
		case "no":
			consent.Record(installer.ExtraSkills, installer.Declined)
			if consentPath != "" {
				_ = installer.SaveConsent(consentPath, consent)
			}
			fmt.Fprintln(out, "skills: declined — not deployed")
			installExtras(cmd, home, repoRoot, mode, &rep, with, without)
			return finishInstall(cmd, mode, rep)
		case "yes":
			// fall through to choosing where
		default:
			// Nobody answered. Record nothing and fall through to the
			// zero-target error below, which is the guard this preserves.
			targets = nil
		}
		if answer == "yes" {
			targets, err = promptForTargets(home)
		}
		if err != nil {
			return errWithExit(1, "target selection: %v", err)
		}
		prompted = true
	default:
		targets = installer.DetectTargets(home)
		fmt.Fprintln(out, style.Dim.Render("targets: detected agent dirs (no config yet; run interactively to choose)"))
	}
	// Zero targets is an ERROR on every path that did not just record a
	// decline: huh's accessible mode (TERM=dumb) returns empty selections on
	// EOF without erroring, so silence here would look like consent.
	if len(targets) == 0 {
		return errWithExit(1, "no install targets selected or detected; nothing would be deployed")
	}
	for _, t := range targets {
		if err := installer.ValidateTarget(t); err != nil {
			return errWithExit(64, "invalid target: %v", err)
		}
	}

	// Past the guard: there are real targets, so skills are genuinely going
	// to be deployed and that is worth recording. Recording earlier is what
	// let an unanswered prompt count as consent.
	if mode == installer.ModeInstall {
		if !consent.Accepted(installer.ExtraSkills) {
			consent.Record(installer.ExtraSkills, installer.Accepted)
			consentDirty = true
		}
		if consentDirty && consentPath != "" {
			if err := installer.SaveConsent(consentPath, consent); err != nil {
				fmt.Fprintln(errw, style.Warn.Render("WARN: could not record your answers: "+err.Error()))
			}
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

	deployed, err := installer.Run(repoRoot, targets, home, mode)
	if err != nil {
		return errWithExit(1, "%v", err)
	}
	rep = deployed
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

	installExtras(cmd, home, repoRoot, mode, &rep, with, without)
	return finishInstall(cmd, mode, rep)
}

// finishInstall prints the mode epilogue. Extracted so the paths that stop
// early — skills declined, so there is nothing to deploy — end the same way
// as a full run rather than falling off the end silently.
func finishInstall(cmd *cobra.Command, mode installer.Mode, rep installer.Report) error {
	out := cmd.OutOrStdout()
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
func installExtras(cmd *cobra.Command, home, repoRoot string, mode installer.Mode, rep *installer.Report, with, without map[string]bool) {
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

	reportHookState(cmd, home, mode, rep, with[installer.ExtraSessionHook])
	reportDirectiveState(cmd, home, repoRoot, mode, rep)

	// Opt-in extras: interactive install mode only.
	// Load what the human already said. A missing record is a machine that
	// has never been asked, which is a third state — not a decline.
	consentPath := installer.ConsentPath()
	consent, _ := installer.LoadConsent(consentPath)
	dirty := false

	// Declines first: recording one is what stops the prompt below being
	// reached at all, and it must work with no terminal.
	if mode == installer.ModeInstall {
		for _, extra := range []string{
			installer.ExtraSessionHook, installer.ExtraClaudeMD, installer.ExtraDevboardUnit,
		} {
			if without[extra] && !consent.Asked(extra) {
				consent.Record(extra, installer.Declined)
				dirty = true
			}
		}
	}

	// Headless answers first: a flag is an explicit decision and must work
	// without a terminal, which is the whole point of having flags. It is
	// also the one form of consent that cannot be faked by an end-of-input
	// prompt returning its default.
	if mode == installer.ModeInstall {
		if with[installer.ExtraClaudeMD] && !consent.Accepted(installer.ExtraClaudeMD) {
			if err := writeDirective(claudeMDPath(home), repoRoot); err != nil {
				fmt.Fprintln(errw, "CLAUDE.md directive:", err)
			} else {
				fmt.Fprintln(out, "CLAUDE.md directive: written")
				consent.Record(installer.ExtraClaudeMD, installer.Accepted)
				dirty = true
			}
		}
		if with[installer.ExtraDevboardUnit] && !consent.Accepted(installer.ExtraDevboardUnit) {
			if err := installDevboardUnit(); err != nil {
				fmt.Fprintln(errw, "devboard service:", err)
			} else {
				fmt.Fprintln(out, "devboard: running at http://localhost:8484")
				consent.Record(installer.ExtraDevboardUnit, installer.Accepted)
				dirty = true
			}
		}
		if with[installer.ExtraSessionHook] && !consent.Asked(installer.ExtraSessionHook) {
			// reportHookState already installed it above when the flag is
			// set; record that so it is not asked again.
			consent.Record(installer.ExtraSessionHook, installer.Accepted)
			dirty = true
		}
		if dirty && consentPath != "" {
			if err := installer.SaveConsent(consentPath, consent); err != nil {
				fmt.Fprintln(errw, style.Warn.Render("WARN: could not record your answers: "+err.Error()))
			}
			dirty = false
		}
	}

	// The hint names only what is genuinely outstanding. It used to print
	// unconditionally, telling a fully configured machine to rerun
	// interactively for three things it already had.
	if mode != installer.ModeInstall || !promptAllowed() {
		if mode == installer.ModeInstall {
			if pending := unansweredExtras(consent); len(pending) > 0 {
				fmt.Fprintln(out, style.Dim.Render(
					"hint: rerun interactively, or pass "+flagsFor(pending)+", to decide: "+strings.Join(pending, ", ")))
			}
		}
		return
	}

	if !consent.Asked(installer.ExtraSessionHook) {
		// offerSessionStartHook does its own present/absent check; recording
		// the answer is what stops it being asked again next run.
		before := hookPresent(home)
		offerSessionStartHook(cmd, home)
		if hookPresent(home) != before {
			consent.Record(installer.ExtraSessionHook, installer.Accepted)
		} else {
			consent.Record(installer.ExtraSessionHook, installer.Declined)
		}
		dirty = true
	}

	if !consent.Asked(installer.ExtraClaudeMD) {
		var yes bool
		// Value(&yes) leaves the default false on purpose. The form library
		// returns its default at end-of-input without erroring, so a
		// yes-defaulted confirm would silently consent to writing a file the
		// human owns.
		if huh.NewForm(huh.NewGroup(huh.NewConfirm().
			Title("Append the dev-context directive to ~/.claude/CLAUDE.md?").
			Value(&yes))).Run() == nil && yes {
			if err := writeDirective(claudeMDPath(home), repoRoot); err != nil {
				fmt.Fprintln(errw, "CLAUDE.md directive:", err)
			} else {
				fmt.Fprintln(out, "CLAUDE.md directive: written")
				consent.Record(installer.ExtraClaudeMD, installer.Accepted)
				dirty = true
			}
		} else {
			consent.Record(installer.ExtraClaudeMD, installer.Declined)
			dirty = true
		}
	}

	// The gate is the record, not a runtime probe. It used to ask whenever
	// the service was not RUNNING, so stopping the unit made the installer
	// offer to install one that was already there.
	if !consent.Asked(installer.ExtraDevboardUnit) {
		var yes bool
		if huh.NewForm(huh.NewGroup(huh.NewConfirm().
			Title("Install and start the devboard service (systemd user unit running `worklog serve`)?").
			Value(&yes))).Run() == nil && yes {
			if err := installDevboardUnit(); err != nil {
				fmt.Fprintln(errw, "devboard service:", err)
			} else {
				fmt.Fprintln(out, "devboard: running at http://localhost:8484")
				consent.Record(installer.ExtraDevboardUnit, installer.Accepted)
				dirty = true
			}
		} else {
			consent.Record(installer.ExtraDevboardUnit, installer.Declined)
			dirty = true
		}
	}

	if dirty && consentPath != "" {
		if err := installer.SaveConsent(consentPath, consent); err != nil {
			fmt.Fprintln(errw, style.Warn.Render("WARN: could not record your answers: "+err.Error()))
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

// reportDirectiveState is why the deployed directive could go stale for weeks
// without a word: until now the CLAUDE.md path lived entirely inside the
// "install mode AND a terminal" early return, so `--check` never opened that
// file at all and reported everything current while the instructions
// governing every session were months behind.
//
// It follows reportHookState's shape deliberately: absence is a note (the
// directive is opt-in and declining it is a supported end state), while a
// block that IS ours and no longer matches the repo is drift.
func reportDirectiveState(cmd *cobra.Command, home, repoRoot string, mode installer.Mode, rep *installer.Report) {
	out, errw := cmd.OutOrStdout(), cmd.ErrOrStderr()
	path := claudeMDPath(home)

	src, err := os.ReadFile(filepath.Join(repoRoot, "CLAUDE.md"))
	if err != nil {
		return // no directive to compare against
	}
	want := installer.MarkedDirective(src)
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return
	}

	switch installer.InspectDirective(existing, want, src) {
	case installer.DirectiveCurrent:
		return

	case installer.DirectiveStale:
		switch mode {
		case installer.ModeCheck:
			fmt.Fprintln(out, style.Bad.Render("drift: CLAUDE.md directive differs from the repo: "+path))
			rep.Drift = true
		case installer.ModeDryRun:
			fmt.Fprintln(out, "would: refresh the CLAUDE.md directive in "+path)
		case installer.ModeInstall:
			// Inside the markers the repo wins — the same rule the rendered
			// projections follow. The warning names the file because, unlike
			// a projection, a hand-edited directive has no source to be
			// regenerated from: this message is the only trace it existed.
			fmt.Fprintln(errw, style.Warn.Render(
				"WARN: replacing the managed block in "+path+" — it differed from the repo copy. "+
					"A backup is at "+path+".bak"))
			if err := writeDirective(path, repoRoot); err != nil {
				fmt.Fprintln(errw, "CLAUDE.md directive:", err)
			} else {
				fmt.Fprintln(out, "CLAUDE.md directive: refreshed")
			}
		}

	case installer.DirectiveUnmarked:
		// An exact match with the current repo body: adoptable in place, so
		// a later run can manage it and an uninstall can find it.
		switch mode {
		case installer.ModeCheck:
			fmt.Fprintln(out, "note: CLAUDE.md directive is present but unmarked; install will adopt it")
		case installer.ModeDryRun:
			fmt.Fprintln(out, "would: wrap the existing CLAUDE.md directive in markers (content unchanged)")
		case installer.ModeInstall:
			if next, ok := installer.AdoptDirective(existing, src, want); ok {
				if err := installer.WriteFileWithBackup(path, next); err == nil {
					fmt.Fprintln(out, "CLAUDE.md directive: adopted (markers added, content unchanged)")
				}
			}
		}

	case installer.DirectiveForeign:
		// Reported, never rewritten. A stale unmarked block matches no known
		// byte string and nothing recorded which revision was appended, so
		// it cannot be told apart from the human's own prose.
		fmt.Fprintln(out, style.Warn.Render(
			"note: "+path+" carries a workflow section this installer cannot place. "+
				"Left alone. Remove it by hand if you want the managed block instead."))
	}
}

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

// hookPresent reports whether the SessionStart hook entry exists, which is
// how the caller tells an accepted prompt from a declined one.

// chosenExtras collapses the flag set into the extras the caller asked for
// without a prompt.

// claudeMDPath is the human-owned instruction file the directive lives in.
func claudeMDPath(home string) string { return filepath.Join(home, ".claude", "CLAUDE.md") }

func chosenExtras(flags map[string]*bool) map[string]bool {
	out := map[string]bool{}
	for name, v := range flags {
		if v != nil && *v {
			out[name] = true
		}
	}
	return out
}

func hookPresent(home string) bool {
	state, _, err := installer.InspectHook(
		installer.SettingsPath(home), installer.HookCommand(installer.HookBinPath(home)))
	return err == nil && state != installer.HookAbsent
}

// unansweredExtras lists the extras with no decision on record. It is what
// makes the non-interactive hint honest: it used to name all three every
// time, including on a machine that already had all three.
func unansweredExtras(c installer.Consent) []string {
	var out []string
	for _, e := range installer.Extras() {
		if !c.Asked(e) {
			out = append(out, e)
		}
	}
	return out
}

// extraFlag maps an extra to the flag that answers it without a prompt.
var extraFlagNo = map[string]string{
	installer.ExtraSkills:       "--no-skills",
	installer.ExtraSessionHook:  "--no-session-hook",
	installer.ExtraClaudeMD:     "--no-claude-md",
	installer.ExtraDevboardUnit: "--no-devboard-service",
}

var extraFlag = map[string]string{
	installer.ExtraSkills:       "--with-skills",
	installer.ExtraSessionHook:  "--with-session-hook",
	installer.ExtraClaudeMD:     "--with-claude-md",
	installer.ExtraDevboardUnit: "--with-devboard-service",
}

func flagsFor(extras []string) string {
	var out []string
	for _, e := range extras {
		if f, ok := extraFlag[e]; ok {
			out = append(out, f)
		}
	}
	return strings.Join(out, " / ")
}

// writeDirective replaces the managed block in a CLAUDE.md, or appends one.
//
// Everything outside the markers is the human's and is copied through
// untouched; everything inside is ours and is replaced. That split is the
// whole design: the rendered-file rule (the repo is the source, so warn and
// overwrite) applies INSIDE the markers, and the settings-file rule (never
// touch a byte we did not write) applies outside them.
//
// It backs up and replaces atomically rather than appending in place, which
// is what the hook writer already does and what the old append did not.
func writeDirective(path, repoRoot string) error {
	src, err := os.ReadFile(filepath.Join(repoRoot, "CLAUDE.md"))
	if err != nil {
		return err
	}
	block := installer.MarkedDirective(src)

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	next, err := installer.ReplaceDirective(existing, block)
	if err != nil {
		return err
	}
	return installer.WriteFileWithBackup(path, next)
}

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
