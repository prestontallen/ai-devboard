package cli

import (
	"os"

	"github.com/mattn/go-isatty"
)

// stdinIsTTY reports whether stdin is connected to a real terminal. Used by
// interactive subcommands to decide whether to launch a form.
func stdinIsTTY() bool {
	return isatty.IsTerminal(os.Stdin.Fd())
}

// stdoutIsTTY reports whether stdout is connected to a real terminal. Used
// by output-styling code (e.g. Glamour rendering) to avoid ANSI sequences
// when piped.
func stdoutIsTTY() bool {
	return isatty.IsTerminal(os.Stdout.Fd())
}
