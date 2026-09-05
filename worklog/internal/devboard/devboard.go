// Package devboard holds the devboard task file's wire shape (Task,
// ChildEntry, and their sub-item types — see devboard/schema.md at the
// repo root) plus read-side helpers for locating and grouping task files
// on disk. Writing is store-backed now (internal/projection.BoardTask/
// ApplyBoardTask render and apply this same shape against a
// store.Ticket; adb-cutover M4 retired this package's own file-mutation
// side (Mutate/OnStart/OnDone/OnPR/OnLink/SyncEpicRoster/MutateChild) —
// there is nothing left here that writes a devboard file.
//
// Ownership rule (schema.md): every shared field has exactly one author;
// mirroring flows worklog→devboard only.
package devboard

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Schema mirror of devboard/schema.md v1. Extra preserves unknown
// top-level fields across read-modify-write cycles (comments are not
// preserved — documented limitation).
type Task struct {
	Schema int    `yaml:"schema"`
	Title  string `yaml:"title,omitempty"`
	// "epic" marks an epic container; "spike" marks investigation-first
	// work, which the UI renders on a short phase track.
	Type   string `yaml:"type,omitempty"`
	Branch string `yaml:"branch,omitempty"`
	// RepoPath is the repository's working-tree root, recorded at start so
	// tooling has a real directory to work in instead of inferring one from
	// cwd. Absent whenever it could not be established with confidence —
	// see RepoRootFor.
	RepoPath   string        `yaml:"repo_path,omitempty"`
	Session    string        `yaml:"session,omitempty"`
	Worklog    string        `yaml:"worklog,omitempty"`
	Tier       *int          `yaml:"tier,omitempty"`
	Complexity string        `yaml:"complexity,omitempty"` // low|medium|high; throttles fan-out
	Phase      string        `yaml:"phase,omitempty"`
	Plan       []PlanItem    `yaml:"plan,omitempty"`
	Score      []ScoreItem   `yaml:"scorecard,omitempty"`
	Decision   []Decision    `yaml:"decisions,omitempty"`
	Code       []CodeRef     `yaml:"code,omitempty"`
	NeedsYou   []NeedsItem   `yaml:"needs_you,omitempty"`
	WaitingOn  []WaitingItem `yaml:"waiting_on,omitempty"`
	Links      []Link        `yaml:"links,omitempty"`
	// Scout records whether the contract-phase risk scout ran. Absence is
	// itself the state the gate reports on.
	Scout *Scout `yaml:"scout,omitempty"`
	// Children carries one entry per child ticket when Type == "epic". An
	// epic file's own Branch/Session/Phase/Plan/Score/Decision/Code/
	// NeedsYou/WaitingOn/Links are unused — that in-flight detail lives
	// per child here instead, since more than one child can be active in
	// ## Now at once and a single shared surface would let them overwrite
	// each other.
	Children []ChildEntry   `yaml:"children,omitempty"`
	Extra    map[string]any `yaml:",inline"`
}

// Child state values for ChildEntry.State.
const (
	ChildPending = "pending"
	ChildActive  = "active"
	ChildDone    = "done"
)

// ChildEntry is one child ticket's roster row plus its own in-flight detail
// — the same shape a standalone ticket file carries, nested instead of
// shared, so concurrently active children never collide.
type ChildEntry struct {
	ID         string        `yaml:"id"`
	Title      string        `yaml:"title,omitempty"`
	State      string        `yaml:"state"` // pending|active|done
	Branch     string        `yaml:"branch,omitempty"`
	Session    string        `yaml:"session,omitempty"`
	Tier       *int          `yaml:"tier,omitempty"`
	Complexity string        `yaml:"complexity,omitempty"`
	Phase      string        `yaml:"phase,omitempty"`
	Plan       []PlanItem    `yaml:"plan,omitempty"`
	Score      []ScoreItem   `yaml:"scorecard,omitempty"`
	Decision   []Decision    `yaml:"decisions,omitempty"`
	Code       []CodeRef     `yaml:"code,omitempty"`
	NeedsYou   []NeedsItem   `yaml:"needs_you,omitempty"`
	WaitingOn  []WaitingItem `yaml:"waiting_on,omitempty"`
	Links      []Link        `yaml:"links,omitempty"`
	Scout      *Scout        `yaml:"scout,omitempty"`
	// Extra closes adb-childentry-extra: without it an unrecognised key
	// under children[] was destroyed by the next epic write, the one
	// guarantee Task already had and ChildEntry did not.
	Extra map[string]any `yaml:",inline"`
}

// ChildIdentity is the roster input to SyncEpicRoster: just enough to
// place a child on the epic card, sourced from notes/<epicID>.md and
// WORK.md's Active children — never the child's in-flight detail, which
// SyncEpicRoster must not disturb.
type ChildIdentity struct {
	ID    string
	Title string
	State string // pending|active|done
}

// Ident carries a sub-item's store identity through a mutation without
// ever reaching the file. The yaml:"-" is load-bearing: these values are
// the store's, and a task subcommand's closure must be able to reorder,
// edit or delete items without the identity following the wrong one.
// Zero on an item the closure just appended, which is how PutTicket knows
// to mint a fresh ULID for it.
type Ident struct {
	ID   string
	Rank int
}

type PlanItem struct {
	Ident `yaml:"-"`
	Text  string `yaml:"text"`
	State string `yaml:"state"` // pending|in_progress|done|blocked
	// Extra keeps keys this struct does not model, so a producer's own
	// annotation survives a rewrite instead of being silently dropped.
	Extra map[string]any `yaml:",inline"`
}

type ScoreItem struct {
	Ident  `yaml:"-"`
	Text   string         `yaml:"text"`
	Verify string         `yaml:"verify,omitempty"`
	Status string         `yaml:"status"` // pending|pass|fail
	Extra  map[string]any `yaml:",inline"`
}

// Scout attests what happened to the contract-phase risk scout. Mode is
// self-reported, so this is an audit record rather than an enforcement
// mechanism: what it makes visible is the case where nothing was recorded.
type Scout struct {
	Mode string `yaml:"mode"` // ran|inline|skipped
	Why  string `yaml:"why,omitempty"`
	When string `yaml:"when,omitempty"`
}

type Decision struct {
	Ident `yaml:"-"`
	What  string `yaml:"what"`
	Why   string `yaml:"why,omitempty"`
	When  string `yaml:"when,omitempty"`
	// Complexity records a re-rate that came with a contract amendment, as
	// "medium → high" or "low (unchanged)". Set only by `task amend`; a plain
	// decision leaves it empty. It lives here rather than on a separate
	// amendments list because decisions are already the rendered timeline for
	// both, and this field travels with the slice the epic child view copies.
	Complexity string         `yaml:"complexity,omitempty"`
	Extra      map[string]any `yaml:",inline"`
}

type CodeRef struct {
	Ident   `yaml:"-"`
	File    string         `yaml:"file"`
	Lines   string         `yaml:"lines,omitempty"`
	Lang    string         `yaml:"lang,omitempty"`
	Note    string         `yaml:"note,omitempty"`
	Snippet string         `yaml:"snippet,omitempty"`
	Extra   map[string]any `yaml:",inline"`
}

type NeedsItem struct {
	Ident  `yaml:"-"`
	Type   string         `yaml:"type,omitempty"` // question|checkpoint
	Text   string         `yaml:"text"`
	Detail string         `yaml:"detail,omitempty"`
	Extra  map[string]any `yaml:",inline"`
}

type Link struct {
	Ident `yaml:"-"`
	Label string         `yaml:"label,omitempty"`
	URL   string         `yaml:"url"`
	Extra map[string]any `yaml:",inline"`
}

// WaitingItem is a question blocked on an EXTERNAL party — another team or
// person, expected to sit for days. Distinct from NeedsItem (blocked on the
// task's own human, resolvable in minutes).
type WaitingItem struct {
	Ident  `yaml:"-"`
	Text   string         `yaml:"text"`
	Who    string         `yaml:"who"`             // who owes the answer; required
	Asked  string         `yaml:"asked,omitempty"` // YYYY-MM-DD; age renders from this
	Link   string         `yaml:"link,omitempty"`  // where it was asked
	Detail string         `yaml:"detail,omitempty"`
	Extra  map[string]any `yaml:",inline"`
}

// UnansweredAtClose is the decision text an outstanding waiting_on entry
// becomes when its task closes. Exported so both close-out paths phrase it
// identically — this package's file-shape one below, and the store-backed
// done path, which holds a store.Ticket and so cannot call CloseWaitingOn.
func UnansweredAtClose(text, who string) string {
	return "unanswered at close: " + text + " (" + who + ")"
}

// CloseWaitingOn converts every remaining waiting_on entry into an
// "unanswered at close" decision and clears the list, so closing a task
// never silently drops an outstanding question.
//
// Caller: the ticketless CLI path (waiting-on resolve all). The ticketed
// done path was the other one, through this package's OnDone, until
// adb-cutover retired it; it now closes out store-side in
// cli.runStoreDone against the same UnansweredAtClose phrasing.
func CloseWaitingOn(t *Task, when string) {
	for _, w := range t.WaitingOn {
		t.Decision = append(t.Decision, Decision{
			What: UnansweredAtClose(w.Text, w.Who),
			When: when,
		})
	}
	t.WaitingOn = nil
}

// DataDir returns the devboard data directory: $DEVBOARD_DATA, defaulting
// to ~/.local/share/devboard.
func DataDir() string {
	if d := os.Getenv("DEVBOARD_DATA"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "devboard")
}

// Enabled reports whether the data dir exists. Callers must treat a false
// result as "do nothing, succeed" — devboard is opt-in by dir presence.
func Enabled() bool {
	d := DataDir()
	if d == "" {
		return false
	}
	fi, err := os.Stat(d)
	return err == nil && fi.IsDir()
}

// Find locates an existing task file <data>/<repo>/<slug>.{yaml,yml} across
// all repo groups. Returns "" (no error) when absent.
func Find(slug string) (string, error) {
	root := DataDir()
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		for _, ext := range []string{".yaml", ".yml"} {
			p := filepath.Join(root, e.Name(), slug+ext)
			if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
				return p, nil
			}
		}
	}
	return "", nil
}

// List returns every task file path grouped under the data dir.
func List() ([]string, error) {
	root := DataDir()
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		files, err := os.ReadDir(filepath.Join(root, e.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			name := strings.ToLower(f.Name())
			if f.Type().IsRegular() && (strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")) {
				out = append(out, filepath.Join(root, e.Name(), f.Name()))
			}
		}
	}
	return out, nil
}

// RepoName derives the grouping directory for new task files: the basename
// of the repository, falling back to the cwd basename outside a repo.
//
// Resolution goes through the *common* git dir rather than the worktree
// root. `git rev-parse --show-toplevel` answers with the linked worktree's
// own path, so every task file created from a worktree used to be filed
// under a group named after the worktree instead of the repo — silently, in
// a directory that only ever held that one file.
func RepoName() string {
	if name := repoNameFromCommonDir(); name != "" {
		return name
	}
	// Older git has no --path-format (added in 2.31). Fall back to the
	// worktree root, which is correct everywhere except inside a linked
	// worktree — i.e. those users keep the old behavior rather than gaining
	// a new failure.
	if out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output(); err == nil {
		if top := strings.TrimSpace(string(out)); top != "" {
			return filepath.Base(top)
		}
	}
	wd, err := os.Getwd()
	if err != nil {
		return "unknown"
	}
	return filepath.Base(wd)
}

// repoNameFromCommonDir resolves the repository name via the common git dir,
// which points at the main repository from anywhere — main checkout, linked
// worktree, or any subdirectory of either. Returns "" when git can't answer,
// leaving the caller to fall back.
func repoNameFromCommonDir() string {
	out, err := exec.Command("git", "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return ""
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" || !filepath.IsAbs(dir) {
		return ""
	}
	dir = filepath.Clean(dir)

	// Normal repo: the common dir is <root>/.git, so the repo is its parent.
	if filepath.Base(dir) == ".git" {
		return filepath.Base(filepath.Dir(dir))
	}
	// Bare repo: the common dir *is* the repository (e.g. …/foo.git).
	return strings.TrimSuffix(filepath.Base(dir), ".git")
}

// PendingNewGroup returns the repo group name when devboard is enabled and
// that group has no directory yet — i.e. the next task file written will
// create it. Empty otherwise.
//
// A brand-new group is usually just the first ticket in a new repo, but it is
// also exactly what a misresolved repo name looks like. The two are
// indistinguishable here, so this reports rather than decides: callers surface
// it and let the human notice a name that isn't their repo.
func PendingNewGroup() string {
	if !Enabled() {
		return ""
	}
	repo := RepoName()
	if repo == "" {
		return ""
	}
	if fi, err := os.Stat(filepath.Join(DataDir(), repo)); err == nil && fi.IsDir() {
		return ""
	}
	return repo
}

// RepoRoot resolves the repository's working-tree root through the same git
// common dir RepoName() uses, so the recorded path and the group name can
// never describe different repositories. Returns "" when git cannot answer
// and for a bare repo, which has no working tree to record.
func RepoRoot() string {
	out, err := exec.Command("git", "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return ""
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" || !filepath.IsAbs(dir) {
		return ""
	}
	dir = filepath.Clean(dir)
	// Normal repo: the common dir is <root>/.git, so the root is its parent.
	// A bare repo's common dir *is* the repository and has no working tree.
	if filepath.Base(dir) != ".git" {
		return ""
	}
	return filepath.Dir(dir)
}

// RepoRootFor returns the root to record for a ticket that declares
// declaredRepo in its **Repo**: field, or "" to record nothing.
//
// A path resolved from the wrong directory is worse than no path: a consumer
// would run against the wrong tree instead of staying silent. So when the
// ticket names a repo and it disagrees with the one cwd resolves to, this
// records nothing. declaredRepo is free text in WORK.md and is sometimes
// owner-qualified ("prestontallen/nole"), so only its final element is
// compared.
func RepoRootFor(declaredRepo string) string {
	root := RepoRoot()
	if root == "" {
		return ""
	}
	declared := strings.TrimSpace(declaredRepo)
	if declared == "" {
		return root
	}
	if !strings.EqualFold(filepath.Base(filepath.ToSlash(declared)), RepoName()) {
		return ""
	}
	return root
}

func GitBranch() string {
	out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
