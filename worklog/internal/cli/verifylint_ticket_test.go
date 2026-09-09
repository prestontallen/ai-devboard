package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/devboard"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/sqlitestore"
	"github.com/prestontallen/ai-devboard/worklog/internal/storepath"
)

// Both mutateTask warn hooks used to re-read the rendered YAML the mutation
// had just written, which made the devboard directory a second reader of
// state the command already held. These pin them to the ticket instead
// (adb-retire-devboard-dir-2).

// ticketFixture seeds a store with one board-tracked ticket and NO devboard
// directory anywhere, which is the state the retirement leaves behind.
func ticketFixture(t *testing.T, tk *store.Ticket) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("WORKLOG_DIR", dir)
	t.Setenv("DEVBOARD_DATA", filepath.Join(dir, "nowhere"))
	path := storepath.DB(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PutTicket(tk); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	return dir
}

func hedgedTicket(repo, repoPath string) *store.Ticket {
	return &store.Ticket{
		Slug: "tkt", Title: "T", Type: store.TypeTicket,
		State: store.StateActive, Section: store.SectionNow,
		Repo: repo, RepoPath: repoPath, BoardTracked: true,
		Scorecard: []store.ScoreItem{{Text: "c1", Verify: "it should basically work", Status: "pending"}},
	}
}

// The lint reads the cell off the mutated ticket, with no rendered file to
// read it from.
func TestScorecardLintReadsTheTicket(t *testing.T) {
	ticketFixture(t, hedgedTicket(devboard.RepoName(), ""))
	out, _, err := runTask(t, "scorecard", "pass", "1", "--id", "tkt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "NOTE:") {
		t.Errorf("a hedged verify cell produced no warning:\n%s", out)
	}
}

// The latent bug this repairs. The old lint built its path from the group
// and slug with no _archive segment, so for a board-archived ticket the
// read failed and the lint silently did nothing. Nothing reported it,
// because a lint that finds nothing and a lint that ran nothing look the
// same from outside.
func TestScorecardLintFiresForABoardArchivedTicket(t *testing.T) {
	tk := hedgedTicket(devboard.RepoName(), "")
	tk.BoardArchived = true
	ticketFixture(t, tk)
	out, _, err := runTask(t, "scorecard", "pass", "1", "--id", "tkt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "NOTE:") {
		t.Errorf("the lint skipped a board-archived ticket, which is the bug:\n%s", out)
	}
}

// The working directory comes from the ticket's recorded checkout when the
// process is not already sitting in that repo.
func TestLintWorkingDirComesFromTheTicket(t *testing.T) {
	repoDir := t.TempDir()
	f := &fakeLister{names: []string{"TestSomething"}}
	f.install(t)
	tk := hedgedTicket("some-other-repo", repoDir)
	tk.Scorecard[0].Verify = "go test ./... -run TestSomething"
	ticketFixture(t, tk)

	if _, _, err := runTask(t, "scorecard", "pass", "1", "--id", "tkt"); err != nil {
		t.Fatal(err)
	}
	if f.calls != 1 {
		t.Fatalf("toolchain queries = %d, want 1", f.calls)
	}
	if f.dirs[0] != repoDir {
		t.Errorf("ran in %q, want the ticket's recorded checkout %q", f.dirs[0], repoDir)
	}
}

// No recorded checkout and not in the ticket's repo means "don't guess":
// running the toolchain against the wrong tree is worse than not running.
func TestLintDeclinesToGuessAWorkingDir(t *testing.T) {
	f := &fakeLister{names: []string{"TestSomething"}}
	f.install(t)
	tk := hedgedTicket("some-other-repo", "")
	tk.Scorecard[0].Verify = "go test ./... -run TestSomething"
	ticketFixture(t, tk)

	if _, _, err := runTask(t, "scorecard", "pass", "1", "--id", "tkt"); err != nil {
		t.Fatal(err)
	}
	if f.calls != 0 {
		t.Errorf("guessed a working directory and ran %d toolchain queries", f.calls)
	}
}

// The scout gate is the other hook on the same seam, and `phase` is the
// most frequently run subcommand there is.
func TestScoutGateWarnsWithoutADirectory(t *testing.T) {
	tk := hedgedTicket(devboard.RepoName(), "")
	tk.Complexity = "high"
	ticketFixture(t, tk)

	out, _, err := runTask(t, "phase", "implementing", "--id", "tkt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "scout") {
		t.Errorf("high-complexity work past contract drew no scout warning:\n%s", out)
	}
}

// And it stays quiet when it should: an attestation exists.
func TestScoutGateQuietWhenAttested(t *testing.T) {
	tk := hedgedTicket(devboard.RepoName(), "")
	tk.Complexity = "high"
	tk.Scout = &store.Scout{Mode: "ran", Why: "four lenses"}
	ticketFixture(t, tk)

	out, _, err := runTask(t, "phase", "implementing", "--id", "tkt")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "no scout attestation") {
		t.Errorf("warned despite an attestation:\n%s", out)
	}
}
