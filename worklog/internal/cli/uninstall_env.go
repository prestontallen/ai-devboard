package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/prestontallen/ai-devboard/worklog/internal/installer"
	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/storepath"
)

// resolveUninstallEnv builds the inventory's inputs through the same code
// every other command uses, so a relocated machine is described correctly
// rather than approximately.
//
// Nothing here consults a compiled-in absolute path. Four mechanisms move
// these directories independently — --dir, WORKLOG_DIR, XDG_DATA_HOME,
// XDG_CONFIG_HOME — and a literal "~/.local/share/worklog" would name a path
// that does not exist on such a machine while the real data sat untouched
// beside it. Under a purge it would be worse than useless: it would delete
// whatever happened to occupy the literal path.
func resolveUninstallEnv() (installer.Env, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return installer.Env{}, fmt.Errorf("resolve home: %w", err)
	}

	// The corpus root, and the store derived from it. resolveWorkdir is the
	// same call the rest of the tool makes, so --dir and WORKLOG_DIR move
	// the listing exactly as they move the data.
	wd, err := resolveWorkdir()
	if err != nil {
		return installer.Env{}, err
	}

	cfgPath := installer.ConfPath()
	cfg, _, _ := installer.LoadConfig(cfgPath)

	// Targets are the UNION of what the config records and what detection
	// finds. Configured alone would miss an agent dir a previous version
	// deployed into before the config existed; detected alone would miss a
	// custom path the human typed. The union is what "any version" means
	// for a directory whose name we do not control.
	seen := map[string]bool{}
	var targets []string
	for _, t := range append(append([]string{}, cfg.Targets...), installer.DetectTargets(home)...) {
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		targets = append(targets, t)
	}

	// The legacy database home is found via storepath, NOT via
	// $WORKLOG_MIGRATION_DATA. That variable is retired and REFUSED rather
	// than honored, everywhere in the tool: honoring it here, uniquely, and
	// honoring it for deletion, would invert the reason it is refused.
	// Callers run refuseRetiredStoreEnv before getting this far.
	legacyStore, err := storepath.LegacyDir()
	if err != nil {
		return installer.Env{}, err
	}

	return installer.Env{
		Home:        home,
		ConfigDir:   filepath.Dir(cfgPath),
		WorklogRoot: wd.Root,
		StoreDir:    storepath.Dir(wd.Root),
		// Contracts are a sibling of the corpus, not a child of it.
		ContractsDir:       filepath.Join(filepath.Dir(wd.Root), "contracts"),
		Targets:            targets,
		RepoRoot:           cfg.RepoRoot,
		LegacyBoardDir:     legacyBoardDir(home),
		LegacyMigrationDir: legacyStore,
	}, nil
}

// legacyBoardDir is the retired standalone rendered-task tree.
//
// $DEVBOARD_DATA is honored here where $WORKLOG_MIGRATION_DATA is not, and the
// asymmetry is deliberate: DEVBOARD_DATA was the documented way to place that
// tree and is still what `worklog serve --help` names, so a machine that set it
// really does have the directory somewhere else. Nothing refuses it, because
// nothing reads it any more either.
func legacyBoardDir(home string) string {
	if d := os.Getenv("DEVBOARD_DATA"); d != "" {
		return d
	}
	return filepath.Join(home, ".local", "share", "devboard")
}

// legacyRootsAgree reports whether the legacy store lookup and the corpus
// lookup are rooted the same way.
//
// They can disagree, and silently: storepath.LegacyDir builds from $HOME while
// model.NewWorkdir prefers $XDG_DATA_HOME. On a machine that sets XDG_DATA_HOME
// the live corpus and the legacy directory sit under different roots, so a
// report that showed both without saying so would look like two peers when one
// of them is simply somewhere else. The caller says so rather than searching
// twice and guessing which answer was meant.
func legacyRootsAgree(home, worklogRoot string) bool {
	def, err := model.NewWorkdir("")
	if err != nil {
		return true // cannot tell; do not manufacture a warning
	}
	return filepath.Dir(def.Root) == filepath.Dir(worklogRoot) &&
		filepath.Dir(def.Root) == filepath.Join(home, ".local", "share")
}
