package main

import (
	"os"

	"github.com/prestontallen/day2day/internal/cli"
)

// Set by GoReleaser via -ldflags -X main.version / main.commit / main.date.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	cli.SetVersion(version, commit, date)
	os.Exit(cli.Execute())
}
