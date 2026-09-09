package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/prestontallen/ai-devboard/worklog/internal/installer"
	"github.com/prestontallen/ai-devboard/worklog/internal/storepath"
	"github.com/prestontallen/ai-devboard/worklog/internal/style"
)

// removeAll and removeFile are the seams for destruction.
//
// They exist for the same reason runCommand does, and for a sharper version of
// the same reason. The install extras were unreachable under test only BY
// ACCIDENT — they sat behind promptAllowed() and `go test` has no TTY — and the
// moment they gained headless flags that accident evaporated. Uninstall is born
// with headless flags. A package-level seam is the smallest thing that makes a
// test reaching the developer's real files impossible rather than merely
// unlikely, and it is checked by a guard test rather than by discipline.
var (
	removeAll  = os.RemoveAll
	removeFile = os.Remove
)

func newUninstallCmd() *cobra.Command {
	var (
		flagCommit      bool
		flagPurgeData   bool
		flagPurgeLegacy bool
		flagKeepLegacy  bool
	)
	cmd := &cobra.Command{
		Use:   "uninstall",
		Args:  cobra.NoArgs,
		Short: "Remove everything any version of the installer put on this machine",
		Long: `uninstall removes the artifacts this tool deployed: the binary, the
skills in every configured and detected target, the command files, the
SessionStart hook entry, the managed block in CLAUDE.md, the config, the
systemd unit and the retired docker container.

A DRY RUN IS THE DEFAULT. It prints the plan and changes nothing; pass
--commit to act. That is the same shape ` + "`worklog adopt`" + ` uses, for the same
reason: a command that deletes should not do it because somebody typed its
name.

Your data is preserved and listed. --purge-data removes the corpus, the
store and the contracts too.

Superseded pre-store layouts are reported and offered separately, because
they hold real history that nothing reads any more. They are never removed
without an explicit answer.

Needs no checkout, no --repo, and no worklog data directory.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagPurgeLegacy && flagKeepLegacy {
				return errWithExit(64, "cannot combine --purge-legacy and --keep-legacy")
			}
			// Every store-touching verb refuses while the retired variable
			// is set. Uninstall is not the one exception, and it would be a
			// bad one to make: it would be honoring a retired variable for
			// deletion.
			if err := refuseRetiredStoreEnv(); err != nil {
				return err
			}
			return runUninstall(cmd, uninstallOpts{
				commit:      flagCommit,
				purgeData:   flagPurgeData,
				purgeLegacy: flagPurgeLegacy,
				keepLegacy:  flagKeepLegacy,
			})
		},
	}
	cmd.Flags().BoolVar(&flagCommit, "commit", false,
		"perform the removal (default is a dry run that changes nothing)")
	cmd.Flags().BoolVar(&flagPurgeData, "purge-data", false,
		"also remove the worklog corpus, the store and the contracts")
	cmd.Flags().BoolVar(&flagPurgeLegacy, "purge-legacy", false,
		"answer yes to the superseded-layout offer without prompting")
	cmd.Flags().BoolVar(&flagKeepLegacy, "keep-legacy", false,
		"answer no to the superseded-layout offer without prompting")
	return cmd
}

type uninstallOpts struct {
	commit      bool
	purgeData   bool
	purgeLegacy bool
	keepLegacy  bool
}

func runUninstall(cmd *cobra.Command, opts uninstallOpts) error {
	out := cmd.OutOrStdout()

	env, err := resolveUninstallEnv()
	if err != nil {
		return errWithExit(1, "%v", err)
	}

	items := installer.Resolve(env)
	present, absent := partitionPresent(items)

	if !legacyRootsAgree(env.Home, env.WorklogRoot) {
		fmt.Fprintln(out, style.Warn.Render(
			"note: the corpus and the retired store directory are rooted differently "+
				"($XDG_DATA_HOME moves one and not the other). Paths below are the resolved ones."))
	}

	fmt.Fprintln(out, style.SubHeading.Render("artifacts"))
	printClass(out, present, installer.ClassArtifact, "  nothing installed")

	fmt.Fprintln(out, "\n"+style.SubHeading.Render("your data"))
	if opts.purgeData {
		fmt.Fprintln(out, style.Warn.Render("  --purge-data: these will be REMOVED"))
	} else {
		fmt.Fprintln(out, style.Dim.Render("  preserved (pass --purge-data to remove)"))
	}
	printClass(out, present, installer.ClassData, "  none found")

	// Superseded layouts are a third thing, and the report says so rather
	// than folding them into either list.
	legacy := filterClass(present, installer.ClassLegacy)
	if len(legacy) > 0 {
		fmt.Fprintln(out, "\n"+style.SubHeading.Render("superseded layouts"))
		if err := reportLegacy(out, env, legacy); err != nil {
			return errWithExit(1, "%v", err)
		}
	}

	reportOptionalSkills(out, env)

	if len(absent) > 0 {
		fmt.Fprintln(out, "\n"+style.Dim.Render(
			fmt.Sprintf("%d known artifact(s) not present on this machine", len(absent))))
	}

	if !opts.commit {
		fmt.Fprintln(out, "\ndry run — nothing was changed. re-run with --commit to uninstall.")
		return nil
	}
	return commitUninstall(out, env, present, opts)
}

// partitionPresent splits the inventory by what is actually on disk. The
// distinction matters to the reader: "not installed" is ordinary, while an
// artifact the inventory does not know about is a bug, and a report that
// showed only what it found could never reveal the second.
func partitionPresent(items []installer.Item) (present, absent []installer.Item) {
	for _, it := range items {
		switch it.Kind {
		case installer.KindContainer, installer.KindImage:
			// Only docker can answer, and it is asked at removal time.
			present = append(present, it)
		case installer.KindHook, installer.KindDirective:
			// These are entries INSIDE a file the human owns. The file
			// existing does not mean our entry is in it; the removers
			// answer that, and until then the honest report is "checked".
			if installer.Present(it) {
				present = append(present, it)
			} else {
				absent = append(absent, it)
			}
		default:
			if installer.Present(it) {
				present = append(present, it)
			} else {
				absent = append(absent, it)
			}
		}
	}
	return present, absent
}

func filterClass(items []installer.Item, c installer.Class) []installer.Item {
	var out []installer.Item
	for _, it := range items {
		if it.Class == c {
			out = append(out, it)
		}
	}
	return out
}

func printClass(out interface{ Write([]byte) (int, error) }, items []installer.Item, c installer.Class, empty string) {
	sel := filterClass(items, c)
	if len(sel) == 0 {
		fmt.Fprintln(out, style.Dim.Render(empty))
		return
	}
	for _, it := range sel {
		fmt.Fprintf(out, "  %-15s %s\n", it.Kind, it.Path)
	}
}

// reportLegacy describes each superseded layout, and refuses outright when the
// retired store directory still holds the only database this machine has.
//
// That refusal is the whole reason this is not a simple listing. `requireStore`
// treats a database at the old path, with none at the new one, as a live store
// in the WRONG PLACE — it tells the human to run `worklog store relocate` and
// explicitly warns them off `adopt`. A machine in that state has not migrated;
// its "legacy" directory is its only copy, and offering to delete it would be
// offering to delete everything. Supersession has to be proven by a live store
// at the new path, never inferred from a directory's name.
func reportLegacy(out interface{ Write([]byte) (int, error) }, env installer.Env, legacy []installer.Item) error {
	for _, it := range legacy {
		size, files, err := treeSize(it.Path)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "  %s\n    %s\n    %s, %d files\n",
			it.Path, it.Label, humanBytes(size), files)

		if it.Path == env.LegacyMigrationDir {
			if _, err := os.Stat(storepath.DBIn(it.Path)); err == nil {
				if _, err := os.Stat(storepath.DB(env.WorklogRoot)); os.IsNotExist(err) {
					return fmt.Errorf(
						"this machine's store is still at the old location and has NOT been relocated\n"+
							"  old: %s\n  new: %s\n"+
							"run `worklog store relocate` first — that directory is your only store, not leftovers",
						storepath.DBIn(it.Path), storepath.DB(env.WorklogRoot))
				}
			}
			// What the human gives up, named rather than summed. The
			// rollback binaries are most of the bytes and all of the
			// consequence: they are the undo path for the store cutover,
			// and `store relocate` told the human to keep this directory
			// until they were satisfied.
			if names := notableLegacyContents(it.Path); len(names) > 0 {
				fmt.Fprintf(out, "    contains: %s\n", strings.Join(names, ", "))
			}
		}
	}
	fmt.Fprintln(out, style.Dim.Render(
		"  kept unless you say otherwise (--purge-legacy / --keep-legacy)"))
	return nil
}

// notableLegacyContents names the things inside the retired store directory a
// human would regret losing silently, so a single yes is an informed one.
func notableLegacyContents(root string) []string {
	var out []string
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var backups int
	for _, e := range entries {
		name := e.Name()
		switch {
		case e.IsDir() && name == "rollback":
			out = append(out, "rollback/ (runnable pre-cutover binaries)")
		case e.IsDir() && (strings.HasPrefix(name, "staging") ||
			strings.HasPrefix(name, "pre-canon-backup") ||
			strings.HasPrefix(name, "pre-heal")):
			out = append(out, name+"/ (corpus snapshot)")
		case strings.Contains(name, ".db.bak"):
			backups++
		case name == storepath.DBName:
			out = append(out, "the pre-move database")
		}
	}
	if backups > 0 {
		out = append(out, fmt.Sprintf("%d database backup(s)", backups))
	}
	sort.Strings(out)
	return out
}

// treeSize walks a tree once and returns apparent bytes and a file count.
//
// Apparent size, not on-disk blocks, and symlinks are counted but never
// followed. Both choices match what the removal will actually do: RemoveAll
// unlinks a symlink rather than descending it, and a size that disagreed with
// the removal would be a number describing a different act.
func treeSize(root string) (bytes int64, files int, err error) {
	err = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		files++
		if info, err := d.Info(); err == nil {
			bytes += info.Size()
		}
		return nil
	})
	if os.IsNotExist(err) {
		return 0, 0, nil
	}
	return bytes, files, err
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for n/div >= unit && exp < 3 {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}

// optionalSkillsToRemove returns the optional skills uninstall may remove,
// and those it must only report.
//
// Ownership here cannot come from the name. Nothing in this repo creates the
// tone symlink — it is gitignored and hand-made — so a *tone* glob proves only
// that somebody has a skill with "tone" in its name, and acting on that would
// delete a stranger's. A symlink resolving INTO the recorded checkout is the
// one available proof, so that is the test, and everything else is reported
// and left where it is.
func optionalSkillsToRemove(env installer.Env) (ours, theirs []installer.OptionalSkill) {
	for _, o := range installer.ProbeOptionalSkills(env.Targets, env.RepoRoot) {
		if o.InRepo {
			ours = append(ours, o)
			continue
		}
		theirs = append(theirs, o)
	}
	return ours, theirs
}

func reportOptionalSkills(out io.Writer, env installer.Env) {
	ours, theirs := optionalSkillsToRemove(env)
	for _, o := range ours {
		fmt.Fprintf(out, "  %-15s %s\n", installer.KindOptional, o.Path)
	}
	for _, o := range theirs {
		why := "not linked into this checkout"
		if o.Dangling {
			why = "a broken symlink, but not into this checkout"
		}
		fmt.Fprintln(out, style.Dim.Render(
			"  kept: "+o.Path+" ("+why+")"))
	}
}
