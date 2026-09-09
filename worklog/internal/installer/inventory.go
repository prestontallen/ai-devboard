package installer

import (
	"os"
	"path/filepath"
	"strings"
)

// The uninstall inventory: what any version of this installer has ever put on
// a machine, and what uninstall does with each thing.
//
// Two properties matter more than completeness, because completeness is not
// checkable from the tree.
//
// First, this is a list of NAMES AND DERIVATION RULES, never absolute paths.
// Four separate mechanisms relocate these directories — --dir, WORKLOG_DIR,
// XDG_DATA_HOME, XDG_CONFIG_HOME — so a compiled-in "~/.local/share/worklog"
// would list a path that does not exist on a relocated machine while the real
// data sat untouched beside it, and under --purge-data would delete whatever
// happened to be at the literal path instead.
//
// Second, every historical entry carries the revision that produced it. The
// deployers themselves are gone: sync.sh was deleted in e8591ca and the whole
// compose fallback in cfc1b31, so nothing in the working tree records what
// they wrote. A name with no traceable producer is stated as such rather than
// quietly asserted — this epic has three recorded instances of a claim that
// read as sourced and was not.
//
// Deliberately NOT covered: whatever a future `worklog update` might leave
// behind, such as backup binaries or a download cache. adb-self-update is
// unstarted and its shape is unknown; guessing at names now would put
// unverifiable entries in a list whose entire value is that its entries are
// verified. Considered and declined, 2026-09-09.

// Class is what uninstall does with an item.
type Class int

const (
	// ClassArtifact is ours and is removed. Nothing a human authored is
	// ever in this class.
	ClassArtifact Class = iota
	// ClassData is the human's and is preserved, listed by resolved path,
	// and removed only under --purge-data.
	ClassData
	// ClassLegacy is a superseded layout that no current code reads. It is
	// neither silently kept nor silently removed: it is reported with its
	// size and offered, because it holds real historical content but no
	// version will ever read it again.
	ClassLegacy
)

// Item is one thing on the machine.
type Item struct {
	Class Class
	// Kind drives how the item is removed, not merely how it prints.
	Kind string
	// Path is absolute for anything on disk. For a container or an image it
	// is the name docker knows it by.
	Path string
	// Label is what the plan prints.
	Label string
	// Since names the revision or era that produced this, or says plainly
	// that no producer is traceable.
	Since string
}

// Item kinds.
const (
	KindBinary    = "binary"
	KindSkillDir  = "skill"
	KindCommand   = "command"
	KindOptional  = "optional-skill"
	KindConfig    = "config"
	KindUnit      = "unit"
	KindWants     = "wants-link"
	KindHook      = "settings-hook"
	KindDirective = "claude-md-block"
	KindDir       = "directory"
	KindContainer = "container"
	KindImage     = "image"
)

// HistoricalSkillNames is every skill directory name any version has ever
// deployed, and the deployed name of the worklog skill alongside them.
//
// It is the same set the current deployer uses, which is a verified fact
// rather than an assumption: `git log -p -- worklog/internal/installer/deploy.go`
// shows skillDirs introduced with these three names and never edited, no
// top-level <name>/SKILL.md has ever been deleted from the repo, and the
// bash-era install.sh looped over the identical three. What DID change is the
// deployment mechanism — the bash era linked where the Go era copies — which
// is why removal has to handle a symlink as readily as a directory.
var HistoricalSkillNames = []string{"dev-context", "contract", "fan-out", "worklog"}

// HistoricalCommandFiles are the files any version has written into the
// Claude commands directory.
//
// uc:worklog.md is the reason this list is not simply derived from what the
// current deployer writes. It exists on the maintainer's machine, no commit
// in this repo has ever contained the string, and the current deployer writes
// only worklog.md — so nothing in the tree can prove it is ours. Reading it
// does: its front matter names it uc:worklog and its body points at this
// project's own skill paths. That is what "every artifact any version
// deployed" means in practice, and why the list is written down rather than
// computed.
var HistoricalCommandFiles = []string{"worklog.md", "uc:worklog.md"}

// TargetDirName is the subdirectory of an agent directory that holds skills.
const TargetDirName = "skills"

// AgentDirs are the agent directories any version has deployed into. The
// first two are also the bash era's hardcoded destinations, which is why
// removal visits them whether or not they are in the targets config.
var AgentDirs = []string{".claude", ".cursor", ".windsurf", ".codex"}

// Env is everything the inventory derives from. Every field is resolved by
// the caller through the same code the rest of the tool uses, so a relocated
// machine is described correctly rather than approximately.
//
// It is a struct rather than a set of package-level lookups so the inventory
// is a pure function: the resolution can be tested without mutating the
// process environment, which is what lets these tests run in parallel and
// what keeps them from ever depending on the developer's real home.
type Env struct {
	Home string
	// ConfigDir holds targets and consent.
	ConfigDir string
	// WorklogRoot is the markdown corpus, resolved via model.NewWorkdir.
	WorklogRoot string
	// StoreDir is derived from WorklogRoot via storepath.Dir.
	StoreDir string
	// ContractsDir is the sibling store of contract documents.
	ContractsDir string
	// Targets is the union of configured and detected skill directories.
	Targets []string
	// RepoRoot is the checkout recorded in the config, or empty. It is the
	// only thing that can prove an optional-skill symlink is ours.
	RepoRoot string
	// LegacyBoardDir is the retired rendered-YAML task tree.
	LegacyBoardDir string
	// LegacyMigrationDir is the retired pre-move database home.
	LegacyMigrationDir string
}

// BinPath is where every version has installed the binary.
func (e Env) BinPath() string { return filepath.Join(e.Home, ".local", "bin", "worklog") }

// CommandsDir is where the Claude command files land.
func (e Env) CommandsDir() string { return filepath.Join(e.Home, ".claude", "commands") }

// UnitPath is the systemd user unit.
func (e Env) UnitPath() string {
	return filepath.Join(e.Home, ".config", "systemd", "user", UnitName)
}

// WantsPath is the enable symlink systemd creates beside the unit. It must be
// gone before the unit file is, or systemd complains on every daemon-reload
// about a symlink pointing at nothing.
func (e Env) WantsPath() string {
	return filepath.Join(e.Home, ".config", "systemd", "user", "default.target.wants", UnitName)
}

// UnitName is the systemd user unit's name.
const UnitName = "devboard.service"

// The retired docker deployment's names. They are not guessed: the surviving
// container on the maintainer's machine carries compose labels recording
// project "devboard" at the repo's devboard/ directory, which is compose's
// default naming and produces exactly these two strings. The compose files
// themselves were deleted in cfc1b31.
const (
	ContainerName = "devboard-devboard-1"
	ImageName     = "devboard-devboard:latest"
)

// Resolve returns every item the inventory knows about, whether or not it is
// present on this machine. Presence is a separate question, asked by the
// caller, so that the plan can distinguish "not installed" from "not known
// about" — the second being a bug and the first being ordinary.
func Resolve(e Env) []Item {
	var items []Item

	for _, t := range e.Targets {
		for _, name := range HistoricalSkillNames {
			items = append(items, Item{
				Class: ClassArtifact, Kind: KindSkillDir,
				Path:  filepath.Join(t, name),
				Label: "skill " + name + " in " + t,
				Since: "deployed by every version; a symlink before e8591ca, a copy after",
			})
		}
	}

	for _, name := range HistoricalCommandFiles {
		since := "sync.sh era, deployer deleted in e8591ca; still written today"
		if name == "uc:worklog.md" {
			since = "no producing revision in this repo; identified as ours by its own content"
		}
		items = append(items, Item{
			Class: ClassArtifact, Kind: KindCommand,
			Path:  filepath.Join(e.CommandsDir(), name),
			Label: "command " + name,
			Since: since,
		})
	}

	items = append(items,
		Item{
			Class: ClassArtifact, Kind: KindHook,
			Path:  SettingsPath(e.Home),
			Label: "SessionStart hook entry in settings.json",
			Since: "hooks.go; removed at handler granularity, foreign hooks untouched",
		},
		Item{
			Class: ClassArtifact, Kind: KindDirective,
			Path:  filepath.Join(e.Home, ".claude", "CLAUDE.md"),
			Label: "managed directive block in CLAUDE.md",
			Since: "marker scheme shipped with adb-installer-consent; only a marked block is removable",
		},
		Item{
			Class: ClassArtifact, Kind: KindConfig,
			Path:  filepath.Join(e.ConfigDir, "targets"),
			Label: "config targets",
			Since: "bash-era format, still parsed today",
		},
		Item{
			Class: ClassArtifact, Kind: KindConfig,
			Path:  filepath.Join(e.ConfigDir, "consent"),
			Label: "config consent (recorded declines are discarded with it)",
			Since: "adb-installer-consent",
		},
		Item{
			Class: ClassArtifact, Kind: KindWants,
			Path:  e.WantsPath(),
			Label: "systemd enable symlink",
			Since: "install.go installDevboardUnit",
		},
		Item{
			Class: ClassArtifact, Kind: KindUnit,
			Path:  e.UnitPath(),
			Label: "systemd user unit " + UnitName,
			Since: "install.go installDevboardUnit",
		},
		Item{
			Class: ClassArtifact, Kind: KindContainer,
			Path:  ContainerName,
			Label: "docker container " + ContainerName,
			Since: "compose fallback, deleted in cfc1b31; the container outlives it",
		},
		Item{
			Class: ClassArtifact, Kind: KindImage,
			Path:  ImageName,
			Label: "docker image " + ImageName,
			Since: "compose fallback, deleted in cfc1b31",
		},
	)

	// The binary is last in the list because it is last in the removal, and
	// the two orders must not be able to drift apart. The session hook names
	// it by absolute path, so a run that deleted it before repairing
	// settings.json would leave every future session invoking a file that is
	// not there.
	items = append(items, Item{
		Class: ClassArtifact, Kind: KindBinary,
		Path:  e.BinPath(),
		Label: "worklog binary",
		Since: "every version",
	})

	items = append(items,
		Item{Class: ClassData, Kind: KindDir, Path: e.WorklogRoot,
			Label: "worklog corpus", Since: "the human's markdown"},
		Item{Class: ClassData, Kind: KindDir, Path: e.StoreDir,
			Label: "store database, sidecars and adopt snapshots", Since: "derived from the corpus root"},
		Item{Class: ClassData, Kind: KindDir, Path: e.ContractsDir,
			Label: "contract documents", Since: "adb-contracts-central"},
	)

	items = append(items,
		Item{Class: ClassLegacy, Kind: KindDir, Path: e.LegacyBoardDir,
			Label: "retired rendered task tree (<repo>/<slug>.yaml)",
			Since: "created by install.sh at f53c3df, retired at 5f1bf60"},
		Item{Class: ClassLegacy, Kind: KindDir, Path: e.LegacyMigrationDir,
			Label: "retired pre-move database home, with its backups and rollback binaries",
			Since: "pre-move store home, superseded by adb-store-adapter"},
	)

	return items
}

// Present reports whether an on-disk item exists. Lstat rather than Stat: a
// dangling symlink is present and must be removable, which is the whole point
// of the optional-skill category.
func Present(it Item) bool {
	switch it.Kind {
	case KindContainer, KindImage:
		return false // only docker can answer; the caller asks it
	}
	_, err := os.Lstat(it.Path)
	return err == nil
}

// OptionalSkillPattern matches a personal skill this tool notices but does not
// deploy. Today that is the tone skill the dev-context ship phase looks for.
const OptionalSkillPattern = "*tone*"

// OptionalSkill is one match, and whether it actually resolves.
type OptionalSkill struct {
	Path string
	// Dangling is a symlink whose target is gone. It is the state worth
	// having a category for: the file is THERE by every existence check a
	// glob can make, and reading it fails. Nothing detected that before,
	// so a tone skill could rot away while install reported all clear.
	Dangling bool
	// InRepo is true when the entry is a symlink resolving inside the
	// recorded checkout. It is the only available proof that this tool's
	// checkout is what the link points at, which is what makes it safe for
	// uninstall to remove: nothing in the repo CREATES this link, so the
	// name alone proves nothing and a *tone* glob would eat a stranger's
	// tone skill.
	InRepo bool
}

// ProbeOptionalSkills looks for optional skills across EVERY target rather
// than one hardcoded directory.
//
// The old probe globbed ~/.claude/skills alone, so a Cursor-only or
// Codex-only machine warned forever about a skill it had. Walking the same
// target list the deployer uses is the whole fix.
func ProbeOptionalSkills(targets []string, repoRoot string) []OptionalSkill {
	var out []OptionalSkill
	seen := map[string]bool{}
	for _, t := range targets {
		matches, _ := filepath.Glob(filepath.Join(t, OptionalSkillPattern))
		for _, m := range matches {
			if seen[m] {
				continue
			}
			seen[m] = true
			s := OptionalSkill{Path: m}
			// Stat follows the link; Lstat does not. A symlink that
			// Lstats fine and Stats badly is precisely a dangling one.
			if fi, err := os.Lstat(m); err == nil && fi.Mode()&os.ModeSymlink != 0 {
				if _, err := os.Stat(m); err != nil {
					s.Dangling = true
				}
				if repoRoot != "" {
					if dest, err := filepath.EvalSymlinks(m); err == nil {
						if rel, err := filepath.Rel(repoRoot, dest); err == nil &&
							!strings.HasPrefix(rel, "..") {
							s.InRepo = true
						}
					}
				}
			}
			out = append(out, s)
		}
	}
	return out
}
