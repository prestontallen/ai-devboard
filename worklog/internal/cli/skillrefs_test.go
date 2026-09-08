package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestSkillsNameOnlyRealCommands walks the process skills that ship with
// this repo and asserts that every `worklog <subcommand>` they mention is a
// command the binary actually registers.
//
// This exists because the failure mode is silent and has now happened
// repeatedly in this epic: a command is deleted, the prose that instructs an
// agent to run it is missed, and the installer deploys the stale text to
// every target without complaint. The installer only checks that files
// exist, and nothing anywhere compares skill prose against the CLI surface.
//
// It is deliberately a repo test rather than an install-time check: the
// skills areversioned here, and catching it at `go test` is earlier than
// catching it at deploy.
func TestSkillsNameOnlyRealCommands(t *testing.T) {
	known := registeredCommands()

	// Only count references that are actually written as commands: inside
	// backticks, or as the first token of a line in a fenced block. A prose
	// blocklist was tried first and was hopeless — "the worklog data",
	// "worklog items", "a worklog entry" all look like invocations to a
	// naive matcher, and the list of exceptions never converges.
	inline := regexp.MustCompile("`\\s*worklog\\s+([a-z][a-z-]*)(?:\\s+([a-z][a-z-]*))?")
	block := regexp.MustCompile(`(?m)^\s*worklog\s+([a-z][a-z-]*)(?:\s+([a-z][a-z-]*))?`)

	roots := []string{"../../../dev-context", "../../../contract", "../../../fan-out", "../../skill"}
	checked := 0
	for _, root := range roots {
		if _, err := os.Stat(root); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			checked++
			seen := map[string]bool{}
			for _, re := range []*regexp.Regexp{inline, block} {
				for _, m := range re.FindAllStringSubmatch(string(data), -1) {
					verb, sub := m[1], m[2]
					full := verb
					if sub != "" && hasSubcommands[verb] {
						full = verb + " " + sub
					}
					if seen[full] {
						continue
					}
					seen[full] = true
					if !known[full] {
						t.Errorf("%s names `worklog %s`, which the binary does not register", path, full)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if checked == 0 {
		t.Fatal("no skill files were checked; the walk is not finding them")
	}
}

// hasSubcommands names the parent commands whose second word is checked.
var hasSubcommands = map[string]bool{"task": true, "store": true, "hook": true}

func registeredCommands() map[string]bool {
	known := map[string]bool{}
	var walk func(c *cobra.Command, prefix string)
	walk = func(c *cobra.Command, prefix string) {
		for _, sub := range c.Commands() {
			name := strings.Fields(sub.Use)[0]
			full := strings.TrimSpace(prefix + " " + name)
			known[full] = true
			walk(sub, full)
		}
	}
	walk(newRoot(), "")
	return known
}
