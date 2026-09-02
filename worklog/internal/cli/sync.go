package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/prestontallen/ai-devboard/worklog/internal/installer"
	"github.com/prestontallen/ai-devboard/worklog/internal/style"
	"github.com/prestontallen/ai-devboard/worklog/internal/sync"
)

func newSyncCmd() *cobra.Command {
	var (
		checkMode  bool
		dryRunMode bool
	)
	cmd := &cobra.Command{
		Use:   "sync",
		Args:  cobra.NoArgs,
		Short: "Deploy skill files to both agents' expected locations",
		Long: `sync copies this repo's skill files to where Cursor and Claude Code expect
them:

  skill/SKILL.md         → ~/.cursor/skills/worklog/SKILL.md
  skill/SKILL.md         → ~/.claude/skills/worklog/SKILL.md
  skill/claude/command.md → ~/.claude/commands/worklog.md

Modes:
  (default)   copy + verify with diff
  --check     diff only; exit 0 if all match, 1 if any differ
  --dry-run   print what would happen; do nothing`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if checkMode && dryRunMode {
				return errWithExit(64, "cannot combine --check and --dry-run")
			}
			return runSync(cmd, checkMode, dryRunMode)
		},
	}
	cmd.Flags().BoolVar(&checkMode, "check", false, "diff sources against targets; exit 1 if any differ")
	cmd.Flags().BoolVar(&dryRunMode, "dry-run", false, "print intended actions; do nothing")
	return cmd
}

func runSync(cmd *cobra.Command, checkMode, dryRunMode bool) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return errWithExit(1, "resolve home: %v", err)
	}
	// When an install config exists, sync respects it: deploy only to the
	// targets the user chose (never to a declined agent), using the repo
	// the config records — no go.mod walk needed, so this works from the
	// repo root and from anywhere else. The fixed-pair behavior below
	// remains only for configless worklog-standalone use.
	if cfg, ok, _ := installer.LoadConfig(installer.ConfPath()); ok && len(cfg.Targets) > 0 {
		mode := installer.ModeInstall
		if checkMode {
			mode = installer.ModeCheck
		}
		if dryRunMode {
			mode = installer.ModeDryRun
		}
		root := cfg.RepoRoot
		if root == "" {
			wl, err := resolveRepoRoot()
			if err != nil {
				return errWithExit(1, "config has no repo line and no checkout found: %v", err)
			}
			root = filepath.Dir(wl)
		}
		rep, err := installer.RunWorklogSkill(root, cfg.Targets, home, mode)
		if err != nil {
			return errWithExit(1, "%v", err)
		}
		w := cmd.OutOrStdout()
		for _, a := range rep.Actions {
			switch a.Kind {
			case "plan":
				fmt.Fprintln(w, "would: "+a.Text)
			case "stale":
				fmt.Fprintln(w, style.Bad.Render("drift: "+a.Text))
			default:
				fmt.Fprintln(w, a.Text)
			}
		}
		if checkMode && rep.Drift {
			return errWithExit(1, "sync: targets differ from repo")
		}
		if len(rep.Actions) == 0 {
			fmt.Fprintln(w, style.Good.Render("sync: all configured targets current"))
		}
		return nil
	}
	repoRoot, err := resolveRepoRoot()
	if err != nil {
		return errWithExit(1, "%v", err)
	}
	pairs := sync.DefaultPairs(repoRoot, home)

	if err := sync.VerifySources(pairs); err != nil {
		return errWithExit(1, "%v", err)
	}

	mode := sync.ModeDefault
	switch {
	case dryRunMode:
		mode = sync.ModeDryRun
	case checkMode:
		mode = sync.ModeCheck
	}

	out := cmd.OutOrStdout()
	mismatch := false

	for _, p := range pairs {
		if mode == sync.ModeDryRun {
			fmt.Fprintln(out, style.Dim.Render("would: mkdir -p "+dirOf(p.Dst)))
			fmt.Fprintln(out, style.Dim.Render("would: cp "+p.Src+" -> "+p.Dst))
			continue
		}

		r, err := sync.ProcessPair(mode, p)
		if err != nil {
			if errors.Is(err, sync.ErrTargetIsDir) {
				return errWithExit(3, "%v", err)
			}
			if errors.Is(err, sync.ErrPostCopyDiff) {
				return errWithExit(2, "%v", err)
			}
			return errWithExit(1, "%v", err)
		}

		var label string
		switch r.Status {
		case sync.StatusSynced:
			label = style.Good.Render("synced:   ")
		case sync.StatusUnchanged:
			label = style.Dim.Render("unchanged:")
		case sync.StatusMatch:
			label = style.Good.Render("match:    ")
		case sync.StatusDiffer:
			label = style.Bad.Render("differ:   ")
			mismatch = true
		default:
			label = string(r.Status) + ":"
		}
		fmt.Fprintln(out, label+" "+r.Src+style.Dim.Render(" -> ")+r.Dst)
	}

	if mode == sync.ModeCheck && mismatch {
		return errWithExit(1, "")
	}
	return nil
}

func resolveRepoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return sync.FindRepoRoot(cwd)
}

func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[:i]
		}
	}
	return "."
}
