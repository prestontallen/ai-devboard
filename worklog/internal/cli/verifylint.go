package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/prestontallen/ai-devboard/worklog/internal/devboard"
)

// A scorecard's verify cell is the only record of how a criterion was proven.
// Three ways it can lie:
//
//   - it names a test that does not exist. `go test ./pkg -run TestNope`
//     prints "ok" and exits 0, so a stale or typo'd name is indistinguishable
//     from a pass.
//   - it hedges ("or manual"), so a ✅ does not say which branch ran.
//   - it names a category ("CLI test", "browser") rather than anything a
//     reader could re-run.
//
// This file warns about all three. It never changes an exit code: a warning
// the author ignores is strictly better than a refusal that blocks a
// legitimate cell the heuristics misjudged.

// listTestsTimeout bounds the child process. `go test -list` compiles the
// package, so the cost is a build, not a query.
const listTestsTimeout = 15 * time.Second

var hedgePhrases = []string{"or manual", "+ manual", "spot-check", "spot check"}

// runnerPrefixes are first tokens that make a cell re-runnable. The list is
// deliberately short: anything unrecognized is warned about, and the author
// silences it by writing a real command or an explicit "manual:" procedure.
var runnerPrefixes = map[string]bool{
	"go": true, "gofmt": true, "git": true, "grep": true, "rg": true,
	"sed": true, "awk": true, "find": true, "diff": true, "ls": true,
	"cat": true, "test": true, "sha256sum": true, "cmp": true,
	"make": true, "bash": true, "sh": true, "zsh": true, "env": true,
	"npm": true, "npx": true, "yarn": true, "pnpm": true, "node": true,
	"python": true, "python3": true, "pytest": true, "uv": true,
	"cargo": true, "docker": true, "curl": true, "worklog": true,
}

// lintVerify returns warnings about a verify cell's wording. Pure: it never
// touches the filesystem or spawns anything.
func lintVerify(text string) []string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	var out []string
	lower := strings.ToLower(trimmed)

	for _, h := range hedgePhrases {
		if strings.Contains(lower, h) {
			out = append(out, "verify hedges ("+h+"): a ✅ won't say which branch ran. "+
				"Name the one check that settles it.")
			break
		}
	}
	if !isRunnableCell(trimmed) {
		out = append(out, "verify is not re-runnable: write a command, or prefix an "+
			"explicit procedure with \"manual:\".")
	}
	return out
}

func isRunnableCell(text string) bool {
	lower := strings.ToLower(text)
	if strings.HasPrefix(lower, "manual:") {
		return true
	}
	// A pipeline or command substitution is a command whatever it starts with.
	if strings.ContainsAny(text, "|&;`") || strings.Contains(text, "$(") {
		return true
	}
	first := text
	if i := strings.IndexAny(first, " \t"); i >= 0 {
		first = first[:i]
	}
	if strings.HasPrefix(first, "./") || strings.HasPrefix(first, "/") {
		return true
	}
	return runnerPrefixes[strings.ToLower(first)]
}

// goTestRun is a `go test … -run <pattern>` invocation recovered from a cell.
type goTestRun struct {
	Packages []string
	Pattern  string // as written
}

// parseGoTestRun finds the first `go test` command in text that carries a
// -run pattern. Returns ok=false when there is nothing to check.
func parseGoTestRun(text string) (goTestRun, bool) {
	toks := shellTokens(text)
	for i := 0; i+1 < len(toks); i++ {
		if toks[i] != "go" || toks[i+1] != "test" {
			continue
		}
		var got goTestRun
		rest := toks[i+2:]
		for j := 0; j < len(rest); j++ {
			t := rest[j]
			switch {
			case t == "-run" || t == "--run":
				if j+1 < len(rest) {
					got.Pattern = rest[j+1]
					j++
				}
			case strings.HasPrefix(t, "-run="), strings.HasPrefix(t, "--run="):
				got.Pattern = t[strings.Index(t, "=")+1:]
			case strings.HasPrefix(t, "-"):
				// Some flags take a value; none we care about, and a stray
				// value simply is not a package path.
			case t == "&&" || t == ";" || t == "|":
				j = len(rest)
			default:
				got.Packages = append(got.Packages, t)
			}
		}
		if got.Pattern == "" {
			return goTestRun{}, false
		}
		return got, true
	}
	return goTestRun{}, false
}

// shellTokens splits on whitespace while honouring single and double quotes,
// so `-run 'TestA|TestB'` stays one token.
func shellTokens(s string) []string {
	var (
		out   []string
		cur   strings.Builder
		quote rune
	)
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"' || r == '`':
			quote = r
		case r == ' ' || r == '\t' || r == '\n':
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}

// topLevelPattern strips the subtest half of a -run pattern. `go help
// testflag`: -list "will only list top-level tests", so querying
// "TestFoo/case_b" always yields nothing and would flag a valid cell. Slashes
// inside a character class are not separators.
func topLevelPattern(pattern string) string {
	depth := 0
	for i, r := range pattern {
		switch r {
		case '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		case '/':
			if depth == 0 {
				return pattern[:i]
			}
		}
	}
	return pattern
}

// listTests is the toolchain seam. Tests replace it; nothing else does.
var listTests = runGoTestList

// runGoTestList returns the top-level test names matching pattern. An error
// means "could not evaluate" — a missing toolchain, a build failure, a
// timeout, or any non-zero exit. Callers stay silent on error rather than
// claiming zero matches, since those are not the same thing.
func runGoTestList(dir string, pkgs []string, pattern string) ([]string, error) {
	if _, err := exec.LookPath("go"); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), listTestsTimeout)
	defer cancel()

	args := append([]string{"test", "-list", pattern}, pkgs...)
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	// -local keeps a target module's toolchain directive from triggering a
	// network download; readonly keeps the lint from editing go.mod.
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=-mod=readonly")

	// Compiler diagnostics go to stderr and are never surfaced: the author
	// asked about a criterion, not for a build log.
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return parseListedTests(string(out)), nil
}

// parseListedTests picks test names out of `go test -list` output, which
// interleaves them with per-package status lines and carries no package
// attribution at all.
func parseListedTests(out string) []string {
	var names []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.ContainsAny(line, " \t") {
			continue // "ok  pkg 0.01s", "? pkg [no test files]", "FAIL pkg [setup failed]"
		}
		switch {
		case strings.HasPrefix(line, "Test"),
			strings.HasPrefix(line, "Benchmark"),
			strings.HasPrefix(line, "Example"),
			strings.HasPrefix(line, "Fuzz"):
			names = append(names, line)
		}
	}
	return names
}

// verifyLintHook returns the mutateTask warning hook for a scorecard verb, or
// nil when this verb has nothing to lint. The returned function runs after the
// mutation, outside devboard.Mutate's flock, so a package compile never blocks
// another writer.
//
// add and edit carry the cell on --verify. pass names an index instead, so the
// cell is read back from the file the mutation just wrote.
func verifyLintHook(verb, flagVerify, indexArg, child string) func(string) []string {
	switch verb {
	case "add", "edit":
		if strings.TrimSpace(flagVerify) == "" {
			return nil
		}
		return func(string) []string { return lintVerify(flagVerify) }
	case "pass":
		return func(taskPath string) []string {
			text := verifyCellAt(taskPath, child, indexArg)
			if strings.TrimSpace(text) == "" {
				return nil
			}
			return append(lintVerify(text), lintTestExistence(taskPath, text)...)
		}
	}
	return nil
}

// verifyCellAt reads the verify text of a 1-based scorecard item, from the
// child's entry when child is set and the top-level scorecard otherwise.
func verifyCellAt(taskPath, child, indexArg string) string {
	raw, err := os.ReadFile(taskPath)
	if err != nil {
		return ""
	}
	var t devboard.Task
	if err := yaml.Unmarshal(raw, &t); err != nil {
		return ""
	}
	score := t.Score
	if child != "" {
		score = nil
		for _, c := range t.Children {
			if strings.EqualFold(c.ID, child) {
				score = c.Score
				break
			}
		}
	}
	i, err := index1(indexArg, len(score), "scorecard")
	if err != nil {
		return ""
	}
	return score[i].Verify
}

// lintTestExistence checks that a cell's `go test -run` pattern matches
// something. Silent whenever it cannot answer.
func lintTestExistence(taskPath, text string) []string {
	run, ok := parseGoTestRun(text)
	if !ok {
		return nil
	}
	dir := lintWorkingDir(taskPath)
	if dir == "" {
		return nil
	}
	pattern := topLevelPattern(run.Pattern)
	if pattern == "" {
		return nil
	}
	names, err := listTests(dir, run.Packages, pattern)
	if err != nil {
		return nil // could not evaluate: never a zero-match claim
	}
	if len(names) == 0 {
		return []string{"verify names -run " + run.Pattern +
			", which matches no test: `go test` exits 0 on zero matches, so this " +
			"criterion would pass green without running anything."}
	}
	return []string{"verify matched: " + strings.Join(names, ", ")}
}

// lintWorkingDir picks the directory to run the toolchain in: cwd when it
// resolves to the repo the task file is grouped under (which is also right
// inside a linked worktree), otherwise the repo_path the ticket recorded.
// Empty means "don't guess" — running against the wrong tree is worse than
// not running.
func lintWorkingDir(taskPath string) string {
	group := filepath.Base(filepath.Dir(taskPath))
	if strings.EqualFold(devboard.RepoName(), group) {
		if wd, err := os.Getwd(); err == nil {
			return wd
		}
	}
	raw, err := os.ReadFile(taskPath)
	if err != nil {
		return ""
	}
	var meta struct {
		RepoPath string `yaml:"repo_path"`
	}
	if err := yaml.Unmarshal(raw, &meta); err != nil || meta.RepoPath == "" {
		return ""
	}
	if fi, err := os.Stat(meta.RepoPath); err != nil || !fi.IsDir() {
		return ""
	}
	return meta.RepoPath
}
