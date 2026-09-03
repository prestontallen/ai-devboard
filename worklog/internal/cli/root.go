// Package cli wires up cobra commands for the worklog CLI.
package cli

import (
	"fmt"
	"os"

	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"

	"github.com/prestontallen/ai-devboard/worklog/internal/freeze"
	"github.com/prestontallen/ai-devboard/worklog/internal/model"
)

// freezeExemptCommands are the top-level commands that keep running while a
// write freeze is held — everything else is refused. This is a default-deny
// list on purpose: a new write verb added later without an entry here is
// blocked during a freeze by default, rather than silently allowed.
var freezeExemptCommands = map[string]bool{
	"validate":  true,
	"status":    true,
	"standup":   true,
	"tui":       true,
	"search":    true,
	"summarize": true,
	"verify":    true,
	"migrate":   true,
	"install":   true,
	"hook":      true,
	"serve":     true,
	"freeze":    true,
}

// topLevelName returns the name of cmd's ancestor that is a direct child of
// the root command, or "" for the bare `worklog` invocation (root itself),
// which runs the read-only default status action.
func topLevelName(cmd *cobra.Command) string {
	root := cmd.Root()
	if cmd == root {
		return ""
	}
	node := cmd
	for node.Parent() != root {
		node = node.Parent()
	}
	return node.Name()
}

// global state populated by root persistent flags.
var (
	flagDir    string
	logger     *log.Logger
	rootCmd    *cobra.Command
	versionStr string // set by main via SetVersion

	// raw build-stamp parts, exposed for the installer's staleness check
	// (comparing the joined display string is how drift bugs happen).
	buildVersion = "dev"
	buildCommit  = "none"
	buildDate    = "unknown"
)

// SetVersion wires the build-time version string (injected via ldflags) into
// the root command before Execute is called.
func SetVersion(version, commit, date string) {
	buildVersion, buildCommit, buildDate = version, commit, date
	versionStr = version + " (" + commit + ", " + date + ")"
}

// BuildCommit returns the raw commit stamp (e.g. "8f6deba" or
// "8f6deba-dirty"), for rev comparison against a repo checkout.
func BuildCommit() string { return buildCommit }

// BuildVersion returns the raw version stamp (e.g. "0.2.0-dev" or a
// release tag like "v0.3.0").
func BuildVersion() string { return buildVersion }

// resolveWorkdir reads the --dir flag, falling back to $WORKLOG_DIR, then to
// model.NewWorkdir's default.
func resolveWorkdir() (model.Workdir, error) {
	dir := flagDir
	if dir == "" {
		dir = os.Getenv("WORKLOG_DIR")
	}
	return model.NewWorkdir(dir)
}

// newRoot constructs the root cobra command and attaches all subcommands.
func newRoot() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "worklog",
		Args:    cobra.NoArgs,
		Version: versionStr,
		Short:   "Personal worklog: cross-session task journal with cap, archive, and index",
		Long: `worklog is the canonical tool for Preston's personal worklog system.

It reads and (eventually) writes a small markdown corpus at
$HOME/.local/share/worklog/ — a front-page WORK.md, a per-month archive, an
INDEX.md spine, and per-epic notes files. The tool replaces a set of bash
scripts that handled validation and skill deployment.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		// Default action: run "status" so `worklog` alone shows the front page.
		// The bare invocation is the human path; agents should call
		// `worklog status --json` explicitly when they need machine-readable
		// output.
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(cmd, false)
		},
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if freezeExemptCommands[topLevelName(cmd)] {
				return nil
			}
			wd, err := resolveWorkdir()
			if err != nil {
				return err
			}
			frozen, info, err := freeze.Check(wd.Root)
			if err != nil {
				return err
			}
			if !frozen {
				return nil
			}
			return freeze.RefusalError(info)
		},
	}

	cmd.PersistentFlags().StringVar(&flagDir, "dir", "",
		"worklog data directory (default $WORKLOG_DIR or ~/.local/share/worklog)")

	cmd.AddCommand(
		newValidateCmd(),
		newStatusCmd(),
		newStandupCmd(),
		newTUICmd(),
		newAddCmd(),
		newImportCmd(),
		newStartCmd(),
		newEditCmd(),
		newDoneCmd(),
		newPrCmd(),
		newLinkCmd(),
		newReindexCmd(),
		newSearchCmd(),
		newServeCmd(),
		newSummarizeCmd(),
		newFeedbackCmd(),
		newNoteCmd(),
		newWaitCmd(),
		newTaskCmd(),
		newHookCmd(),
		newInstallCmd(),
		newMigrateCmd(),
		newVerifyCmd(),
		newFreezeCmd(),
	)
	return cmd
}

// Execute is the entrypoint called from main. It returns a process exit code.
func Execute() int {
	logger = log.New(os.Stderr)
	logger.SetReportTimestamp(false)
	logger.SetLevel(log.InfoLevel)

	rootCmd = newRoot()
	if err := rootCmd.Execute(); err != nil {
		// Some subcommands return a typed error to set a non-default exit
		// code; honor those first.
		if ec, ok := err.(exitCoder); ok {
			fmt.Fprintln(os.Stderr, ec.Error())
			return ec.ExitCode()
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

// exitCoder lets subcommands signal a specific process exit code via error.
type exitCoder interface {
	error
	ExitCode() int
}

type codedError struct {
	code int
	msg  string
}

func (e *codedError) Error() string { return e.msg }
func (e *codedError) ExitCode() int { return e.code }
func errWithExit(code int, format string, a ...any) *codedError {
	return &codedError{code: code, msg: fmt.Sprintf(format, a...)}
}
