// Package cli wires up cobra commands for the worklog CLI.
package cli

import (
	"fmt"
	"os"

	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"

	"github.com/prestontallen/day2day/internal/model"
)

// global state populated by root persistent flags.
var (
	flagDir  string
	logger   *log.Logger
	rootCmd  *cobra.Command
)

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
		Use:   "worklog",
		Args:  cobra.NoArgs,
		Short: "Personal worklog: cross-session task journal with cap, archive, and index",
		Long: `worklog is the canonical tool for Preston's personal worklog system.

It reads and (eventually) writes a small markdown corpus at
$HOME/.local/share/worklog/ — a front-page WORK.md, a per-month archive, an
INDEX.md spine, and per-epic notes files. The tool replaces a set of bash
scripts that handled validation, sync, and lint.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		// Default action: run "status" so `worklog` alone shows the front page.
		// The bare invocation is the human path; agents should call
		// `worklog status --json` explicitly when they need machine-readable
		// output.
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(cmd, false)
		},
	}

	cmd.PersistentFlags().StringVar(&flagDir, "dir", "",
		"worklog data directory (default $WORKLOG_DIR or ~/.local/share/worklog)")

	cmd.AddCommand(
		newValidateCmd(),
		newStatusCmd(),
		newTUICmd(),
		newAddCmd(),
		newStartCmd(),
		newDoneCmd(),
		newPrCmd(),
		newReindexCmd(),
		newSearchCmd(),
		newSyncCmd(),
		newLintSpecsCmd(),
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

func (e *codedError) Error() string  { return e.msg }
func (e *codedError) ExitCode() int  { return e.code }
func errWithExit(code int, format string, a ...any) *codedError {
	return &codedError{code: code, msg: fmt.Sprintf(format, a...)}
}
