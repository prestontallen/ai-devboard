package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/huh"

	"github.com/prestontallen/ai-devboard/worklog/internal/freeze"
	"github.com/prestontallen/ai-devboard/worklog/internal/installer"
	"github.com/prestontallen/ai-devboard/worklog/internal/lockfile"
	"github.com/prestontallen/ai-devboard/worklog/internal/storepath"
	"github.com/prestontallen/ai-devboard/worklog/internal/style"
)

// commitUninstall performs the removal.
//
// The ORDER is the design, not an implementation detail, and it runs strictly
// back-to-front against the dependencies:
//
//  1. Release the freeze, before any disposition, so --purge-data and the
//     legacy removal both see an unfrozen corpus and the summary line is
//     printed even when the corpus is subsequently purged.
//  2. Stop and disable the service, then remove the unit and its enable
//     symlink. Disable BEFORE deleting the unit file, or the wants symlink is
//     left dangling and systemd complains on every daemon-reload afterwards.
//  3. Docker, best effort.
//  4. Skills, commands, config: files with no other file pointing at them.
//  5. The two human-owned files, whose entries name the binary.
//  6. The binary, LAST.
//
// Step 6 is last because the SessionStart hook names the binary by absolute
// path. A run that deleted the binary first and then failed to repair
// settings.json would leave every future Claude Code session invoking a file
// that is not there — so a failure anywhere in 1-5 aborts before step 6 and
// says what was left, rather than pressing on.
func commitUninstall(out io.Writer, env installer.Env, items []installer.Item, opts uninstallOpts) error {
	var failures []string
	note := func(f string, a ...any) { fmt.Fprintf(out, "  "+f+"\n", a...) }
	fail := func(what string, err error) {
		failures = append(failures, fmt.Sprintf("%s: %v", what, err))
		fmt.Fprintln(out, style.Bad.Render("  failed: "+what+": "+err.Error()))
	}

	fmt.Fprintln(out, "\n"+style.SubHeading.Render("removing"))

	releaseFreeze(out, env)

	// --- services ---
	if hasKind(items, installer.KindUnit) || hasKind(items, installer.KindWants) {
		stopDevboardUnit(out)
	}
	for _, kind := range []string{installer.KindWants, installer.KindUnit} {
		for _, it := range byKind(items, kind) {
			if err := removeFile(it.Path); err != nil && !os.IsNotExist(err) {
				fail(it.Label, err)
			} else {
				note("removed %s", it.Path)
			}
		}
	}
	if hasKind(items, installer.KindUnit) {
		if _, err := runCommand("systemctl", "--user", "daemon-reload"); err != nil {
			note("note: daemon-reload unavailable (%v); the unit files are gone regardless", err)
		}
	}

	removeDockerArtifacts(out, items)

	// --- plain files ---
	for _, kind := range []string{installer.KindSkillDir, installer.KindCommand, installer.KindConfig} {
		for _, it := range byKind(items, kind) {
			if err := removeAll(it.Path); err != nil {
				fail(it.Label, err)
			} else {
				note("removed %s", it.Path)
			}
		}
	}
	pruneEmptyDir(out, env.ConfigDir)

	// The optional skill goes with the rest of the deployed files, but only
	// when it points into the checkout we recorded. See optionalSkillsToRemove.
	ourOptional, theirOptional := optionalSkillsToRemove(env)
	for _, o := range ourOptional {
		if err := removeFile(o.Path); err != nil && !os.IsNotExist(err) {
			fail("optional skill "+o.Path, err)
		} else {
			note("removed %s", o.Path)
		}
	}
	for _, o := range theirOptional {
		note("kept %s (not linked into this checkout)", o.Path)
	}

	// --- the human's own files ---
	for _, it := range byKind(items, installer.KindHook) {
		removed, err := installer.RemoveHook(it.Path)
		switch {
		case err != nil:
			// An unreadable settings.json is the case that must NOT be
			// papered over: the hook still names the binary, so removing
			// the binary anyway would break every future session start.
			fail("SessionStart hook in "+it.Path, err)
		case removed:
			note("removed the SessionStart hook from %s", it.Path)
		default:
			note("no SessionStart hook of ours in %s", it.Path)
		}
	}
	for _, it := range byKind(items, installer.KindDirective) {
		if err := removeDirectiveBlock(out, it.Path); err != nil {
			fail(it.Label, err)
		}
	}

	if opts.purgeData {
		purgeDataDirs(out, items, note, fail)
	}
	if err := handleLegacy(out, env, items, opts, note, fail); err != nil {
		return err
	}

	// --- the binary, last ---
	if len(failures) > 0 {
		fmt.Fprintln(out, style.Bad.Render(
			fmt.Sprintf("\n%d step(s) failed; the binary was NOT removed", len(failures))))
		fmt.Fprintln(out, style.Dim.Render(
			"  fix the above and re-run, or remove "+env.BinPath()+" by hand once you are satisfied"))
		return errWithExit(1, "uninstall incomplete: %s", strings.Join(failures, "; "))
	}
	for _, it := range byKind(items, installer.KindBinary) {
		if err := removeFile(it.Path); err != nil && !os.IsNotExist(err) {
			return errWithExit(1, "removing %s: %v", it.Path, err)
		}
		note("removed %s", it.Path)
		// A binary somewhere else is not a failure, but saying nothing
		// about it would make the summary a lie: the machine still has a
		// worklog on it.
		if self, err := selfPath(); err == nil {
			if resolved, _ := filepath.EvalSymlinks(self); resolved != "" && resolved != it.Path {
				fmt.Fprintln(out, style.Warn.Render(
					"  note: this process is running from "+resolved+
						", which is not the installed path and was left alone"))
			}
		}
	}

	fmt.Fprintln(out, style.Good.Render("\nuninstalled."))
	if !opts.purgeData {
		fmt.Fprintln(out, style.Dim.Render("  your data was left in place; see the list above"))
	}
	return nil
}

// releaseFreeze lifts a freeze this machine is holding, and refuses to lift
// one somebody else is actively holding.
//
// freeze.Release is an unconditional remove with no ownership check, which is
// right for the release command a human types and wrong here: `adopt --commit`
// holds a freeze across its whole conversion with a deferred release, so
// lifting it mid-window would silently unblock every other writer against a
// store being rewritten, and adopt's own deferred release would then find
// nothing and say nothing.
func releaseFreeze(out io.Writer, env installer.Env) {
	frozen, info, err := freeze.Check(env.WorklogRoot)
	if err != nil {
		// Check fails safe by reporting frozen AND an error. Aborting here
		// would brick uninstall on a permission-damaged sentinel, so this
		// is reported and the run continues.
		fmt.Fprintln(out, style.Warn.Render(
			"  note: a write freeze may be held and could not be read ("+err.Error()+"); left alone"))
		return
	}
	if !frozen {
		return
	}
	if info.PID != 0 && info.PID != os.Getpid() && processAlive(info.PID) {
		fmt.Fprintln(out, style.Warn.Render(fmt.Sprintf(
			"  note: a write freeze is held by a running process (pid %d, %q) and was left in place",
			info.PID, info.Reason)))
		return
	}
	if err := freeze.Release(env.WorklogRoot); err != nil {
		fmt.Fprintln(out, style.Warn.Render("  note: could not release the write freeze: "+err.Error()))
		return
	}
	fmt.Fprintf(out, "  released the write freeze left by pid %d (%s)\n", info.PID, info.Reason)
}

// processAlive reports whether a pid names a live process. Signal 0 performs
// the permission and existence checks without delivering anything.
var processAlive = func(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(nil) == nil
}

// stopDevboardUnit stops and disables before anything is unlinked.
func stopDevboardUnit(out io.Writer) {
	if _, err := runCommand("systemctl", "--user", "disable", "--now", installer.UnitName); err != nil {
		// Over ssh or in CI there is no user D-Bus session and every call
		// fails. That is a skip, not a failure: the unit files are removed
		// either way and systemd reconciles on next login.
		fmt.Fprintln(out, style.Dim.Render(
			"  note: systemctl unavailable or the unit was not running; removing its files anyway"))
		return
	}
	fmt.Fprintln(out, "  stopped and disabled "+installer.UnitName)
}

// removeDockerArtifacts is best effort by design: docker may not be installed,
// the daemon may be down, and the container is an orphan of a code path that
// was deleted from this repo entirely.
//
// It never passes -v. The container bind-mounts two real data directories, and
// -v would reach past the container into them.
func removeDockerArtifacts(out io.Writer, items []installer.Item) {
	for _, it := range byKind(items, installer.KindContainer) {
		if _, err := runCommand("docker", "rm", "-f", it.Path); err != nil {
			fmt.Fprintln(out, style.Dim.Render("  note: no docker container "+it.Path+" to remove"))
			continue
		}
		fmt.Fprintln(out, "  removed container "+it.Path)
	}
	for _, it := range byKind(items, installer.KindImage) {
		if _, err := runCommand("docker", "rmi", it.Path); err != nil {
			fmt.Fprintln(out, style.Dim.Render("  note: no docker image "+it.Path+" to remove"))
			continue
		}
		fmt.Fprintln(out, "  removed image "+it.Path)
	}
}

// removeDirectiveBlock cuts our block out, and deletes the file when the block
// was all it held.
//
// Leaving a zero-byte CLAUDE.md would be worse than it sounds: the harness
// still loads it, no install --check would ever mention it, and the human is
// left with an invisible empty instruction file. On this machine the file IS
// nothing but the managed block, so this is the ordinary case, not the corner.
func removeDirectiveBlock(out io.Writer, path string) error {
	existing, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !installer.HasManagedDirective(existing) {
		fmt.Fprintln(out, style.Dim.Render(
			"  note: "+path+" holds no managed block; left untouched"))
		return nil
	}
	next, removed := installer.RemoveDirective(existing)
	if !removed {
		return nil
	}
	if len(strings.TrimSpace(string(next))) == 0 {
		if err := removeFile(path); err != nil {
			return err
		}
		fmt.Fprintln(out, "  removed "+path+" (it held nothing but our block)")
		return nil
	}
	if err := installer.WriteFileWithBackup(path, next); err != nil {
		return err
	}
	fmt.Fprintln(out, "  removed our block from "+path+", keeping your own text")
	return nil
}

func purgeDataDirs(out io.Writer, items []installer.Item, note func(string, ...any), fail func(string, error)) {
	for _, it := range items {
		if it.Class != installer.ClassData {
			continue
		}
		if err := removeAll(it.Path); err != nil {
			fail(it.Label, err)
			continue
		}
		note("purged %s", it.Path)
	}
}

// handleLegacy applies the answer to the superseded-layout offer.
//
// The prompt's DEFAULT IS KEEP, and a yes cannot come from a widget alone. No
// prompt shape in this codebase is end-of-input-proof: a Confirm returns its
// default and a Select returns its first option without erroring, which is
// measured rather than assumed — an earlier version of the installer recorded
// "accepted" for a prompt nobody saw. For an ordinary extra that is a nuisance.
// For a delete it is unacceptable, so the only paths that remove anything are
// an explicit flag or a typed confirmation.
func handleLegacy(out io.Writer, env installer.Env, items []installer.Item, opts uninstallOpts,
	note func(string, ...any), fail func(string, error)) error {
	legacy := filterClass(items, installer.ClassLegacy)
	if len(legacy) == 0 {
		return nil
	}
	// A flag is an explicit decision and is honoured as given. With no flag
	// the human is asked, and a refusal to answer keeps the data.
	switch {
	case opts.keepLegacy:
		fmt.Fprintln(out, style.Dim.Render("  superseded layouts kept"))
		return nil
	case opts.purgeLegacy:
		// asked and answered on the command line
	default:
		var confirmed []installer.Item
		for _, it := range legacy {
			if confirmLegacyRemoval(out, it.Path) {
				confirmed = append(confirmed, it)
			}
		}
		if len(confirmed) == 0 {
			fmt.Fprintln(out, style.Dim.Render("  superseded layouts kept"))
			return nil
		}
		legacy = confirmed
	}
	for _, it := range legacy {
		// The store's own lock file guards the database against a
		// concurrent writer. Deleting a database out from under a held
		// flock is corruption rather than data loss, which is the shape
		// storepath's whole layout rule exists to prevent.
		if it.Path == env.LegacyMigrationDir {
			rel, err := lockfile.AcquireWithin(storepath.DBIn(it.Path)+".lock", legacyLockWait)
			if err != nil {
				fail("legacy store is locked by another process", err)
				continue
			}
			err = removeAll(it.Path)
			rel()
			if err != nil {
				fail(it.Label, err)
				continue
			}
			note("removed %s", it.Path)
			continue
		}
		if err := removeAll(it.Path); err != nil {
			fail(it.Label, err)
			continue
		}
		note("removed %s", it.Path)
	}
	return nil
}

// pruneEmptyDir removes a directory we own once nothing is left in it, and
// leaves it alone otherwise. A skills/ target is never passed here: those are
// the human's directories that merely held our files.
func pruneEmptyDir(out io.Writer, dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) > 0 {
		return
	}
	if err := removeFile(dir); err == nil {
		fmt.Fprintln(out, "  removed "+dir)
	}
}

func byKind(items []installer.Item, kind string) []installer.Item {
	var out []installer.Item
	for _, it := range items {
		if it.Kind == kind {
			out = append(out, it)
		}
	}
	return out
}

func hasKind(items []installer.Item, kind string) bool {
	return len(byKind(items, kind)) > 0
}

// legacyLockWait is how long the legacy purge waits for a concurrent writer
// before refusing. It mirrors relocate's window: the same lock, the same
// database, and the same reason to prefer refusing over racing.
var legacyLockWait = 3 * time.Second

// confirmLegacyRemoval asks, and treats the answer as worthless unless it
// carries information a form cannot invent.
//
// The hazard is measured, not theoretical. huh returns a Confirm's default and
// a Select's first option at end-of-input WITHOUT erroring, and an earlier
// version of the installer recorded "accepted" for a prompt nobody ever saw.
// For an opt-in extra that is a nuisance; for a delete it is unacceptable.
//
// Two things make a fabricated yes impossible here rather than unlikely. The
// default is the safe answer, so an unanswered widget keeps the data. And the
// answer is a TYPED STRING compared against a word the zero value cannot
// equal, so silence produces "" and "" is not consent. A bool could not carry
// that distinction at all.
func confirmLegacyRemoval(out io.Writer, path string) bool {
	if !promptAllowed() {
		fmt.Fprintln(out, style.Dim.Render(
			"  no terminal to ask on; superseded layouts kept "+
				"(pass --purge-legacy or --keep-legacy to answer without one)"))
		return false
	}
	var typed string
	err := huh.NewForm(huh.NewGroup(huh.NewInput().
		Title("Permanently delete " + path + "?").
		Description("This is not recoverable. Type " + legacyConfirmWord + " to delete, or press enter to keep it.").
		Value(&typed))).Run()
	if err != nil {
		return false
	}
	return legacyAnswerMeansDelete(typed)
}

// legacyAnswerMeansDelete is the decision, split out from the widget on
// purpose: under `go test` the form errors before it ever returns a value, so
// a test driving confirmLegacyRemoval exercises the error branch and proves
// nothing about what an answer MEANS. A mutation probe caught exactly that —
// making the comparison return true unconditionally left the integration test
// green. The rule is only testable once it is a function of the string.
//
// Only the whole word counts. "y", "yes" and "" are all refusals, because the
// values a form can invent at end-of-input are precisely the short and empty
// ones.
func legacyAnswerMeansDelete(typed string) bool {
	return strings.EqualFold(strings.TrimSpace(typed), legacyConfirmWord)
}

// legacyConfirmWord is what a human types to mean yes. It is deliberately not
// "y": a single keystroke is exactly what an accidental buffer can supply.
const legacyConfirmWord = "delete"
