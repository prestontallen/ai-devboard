package skills_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/skills"
)

// The embedded tree is the only copy a clone-free install ever sees, so a file
// that silently fails to embed is invisible until a user on a fresh machine
// gets a skill with a missing reference. Two ways that happens, both recorded
// in this repo: //go:embed skips names beginning with "." or "_" without
// complaint, and a directory glob absorbs whatever else appears in the tree.
//
// Comparing the embedded set against the on-disk set catches both directions —
// a file that should be embedded and is not, and a file that got embedded and
// should not have.

func embeddedFiles(t *testing.T) []string {
	t.Helper()
	var out []string
	err := fs.WalkDir(skills.FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the embedded FS: %v", err)
	}
	sort.Strings(out)
	return out
}

func onDiskFiles(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		// The package's own Go files are source, not deploy content.
		if strings.HasSuffix(p, ".go") {
			return nil
		}
		out = append(out, filepath.ToSlash(p))
		return nil
	})
	if err != nil {
		t.Fatalf("walking the source dir: %v", err)
	}
	sort.Strings(out)
	return out
}

// manifest is the exact deploy payload. It is a literal, not a walk of the
// tree, because comparing the tree against itself cannot catch the absorption
// direction: a stray node_modules inside a skill dir appears in BOTH the
// embedded FS and the on-disk walk, so a self-comparison matches and ships it.
// Adding a skill file means adding a line here, on purpose.
var manifest = []string{
	"CLAUDE.md",
	"contract/SKILL.md",
	"contract/references/contract-template.md",
	"contract/references/spike-contract.md",
	"dev-context/SKILL.md",
	"fan-out/SKILL.md",
	"fan-out/references/research.md",
	"fan-out/references/risk-scout.md",
	"worklog/SKILL.md",
	"worklog/claude/command.md",
	"worklog/references/adoption.md",
	"worklog/references/cli.md",
	"worklog/references/feedback-capture.md",
	"worklog/references/formats.md",
	"worklog/references/import.md",
}

func TestEmbeddedSkillManifest(t *testing.T) {
	embedded := embeddedFiles(t)

	want := append([]string(nil), manifest...)
	sort.Strings(want)
	if strings.Join(embedded, "\n") != strings.Join(want, "\n") {
		t.Errorf("embedded tree is not the pinned manifest\nembedded (%d):\n  %s\nmanifest (%d):\n  %s",
			len(embedded), strings.Join(embedded, "\n  "),
			len(want), strings.Join(want, "\n  "))
	}
	if len(embedded) == 0 {
		t.Fatal("nothing is embedded; the go:embed directive is not matching")
	}
}

// The manifest alone cannot see a file that exists on disk and failed to
// embed, because it is compared against the embedded set. This catches that
// direction: //go:embed skips names beginning with "." or "_" in silence.
func TestEmbeddedMatchesSourceDir(t *testing.T) {
	embedded := embeddedFiles(t)
	disk := onDiskFiles(t)

	if strings.Join(embedded, "\n") != strings.Join(disk, "\n") {
		t.Errorf("embedded tree does not match the source dir on disk\nembedded (%d):\n  %s\non disk (%d):\n  %s",
			len(embedded), strings.Join(embedded, "\n  "),
			len(disk), strings.Join(disk, "\n  "))
	}
}

// Every skill this installer deploys must actually be in the binary. Named
// explicitly rather than derived from the tree, so deleting a skill directory
// fails here instead of quietly shrinking the manifest to match itself.
func TestEmbeddedCarriesEverySkill(t *testing.T) {
	for _, want := range []string{
		"CLAUDE.md",
		"dev-context/SKILL.md",
		"contract/SKILL.md",
		"fan-out/SKILL.md",
		"worklog/SKILL.md",
		"worklog/claude/command.md",
	} {
		if _, err := fs.Stat(skills.FS, want); err != nil {
			t.Errorf("%s is not embedded: %v", want, err)
		}
	}
	refs, err := fs.ReadDir(skills.FS, "worklog/references")
	if err != nil || len(refs) == 0 {
		t.Errorf("worklog/references is empty or missing: %v", err)
	}
}

// go:embed refuses to follow symlinks and fails the build on one inside the
// tree, so the real directive file has to live here and the repo-root copy has
// to be the link. Reversing that direction breaks every build, which is a loud
// failure — but this asserts the arrangement so nobody "tidies" it back.
func TestDirectiveSourceIsARegularFile(t *testing.T) {
	fi, err := os.Lstat("CLAUDE.md")
	if err != nil {
		t.Fatalf("CLAUDE.md is missing from the skills dir: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Fatal("skills/CLAUDE.md is a symlink; go:embed cannot follow it. " +
			"The real file belongs here and the repo-root copy is the link.")
	}
	body, err := fs.ReadFile(skills.FS, "CLAUDE.md")
	if err != nil {
		t.Fatalf("reading the embedded directive: %v", err)
	}
	if !strings.Contains(string(body), "dev-context") {
		t.Error("the embedded directive does not mention dev-context; wrong file?")
	}
}
