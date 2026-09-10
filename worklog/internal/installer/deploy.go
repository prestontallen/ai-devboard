package installer

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Mode selects install / check / dry-run behavior.
type Mode int

const (
	ModeInstall Mode = iota
	ModeCheck
	ModeDryRun
)

// Action is one reportable step. Kind drives styling and drift accounting.
type Action struct {
	Kind string // "note" | "plan" | "stale" | "warn"
	Text string
}

// Report accumulates actions; Drift is true when any "stale" was recorded.
type Report struct {
	Actions []Action
	Drift   bool
}

func (r *Report) note(f string, a ...any) {
	r.Actions = append(r.Actions, Action{"note", fmt.Sprintf(f, a...)})
}
func (r *Report) plan(f string, a ...any) {
	r.Actions = append(r.Actions, Action{"plan", fmt.Sprintf(f, a...)})
}
func (r *Report) warn(f string, a ...any) {
	r.Actions = append(r.Actions, Action{"warn", fmt.Sprintf(f, a...)})
}
func (r *Report) stale(f string, a ...any) {
	r.Actions = append(r.Actions, Action{"stale", fmt.Sprintf(f, a...)})
	r.Drift = true
}

// RepoSkills describes what a checkout deploys: three whole skill dirs,
// plus worklog's SKILL.md and its references/ dir, plus the claude-only
// command file. worklog is the odd one out because its source dir also
// holds claude/command.md, which deploys somewhere else entirely — so it
// gets explicit file and dir steps instead of one whole-dir copy.
var skillDirs = []string{"dev-context", "contract", "fan-out"}

const (
	// skillsRel is where skill sources live inside a checkout. They moved
	// here from the repo root so go:embed could reach them: the module is
	// rooted at worklog/, and embed cannot escape its own module. Deploy
	// DESTINATIONS are unaffected — a skill still lands at <target>/<name>,
	// so installed machines and the uninstall inventory see no change.
	skillsRel = "worklog/skills"

	// These are relative to the SOURCE (the skills dir), not to a repo root,
	// so the same strings address a checkout and the embedded FS alike.
	worklogSkillRel   = "worklog/SKILL.md"
	worklogRefsRel    = "worklog/references"
	claudeCommandRel  = "worklog/claude/command.md"
	directiveRel      = "CLAUDE.md"
	claudeCommandsDir = ".claude/commands"
)

// Source is where deploy content comes from: a checkout's skills dir, or the
// copy compiled into the binary.
//
// Both are an fs.FS rooted at the skills directory, so every path below is the
// same either way ("dev-context", "worklog/SKILL.md", "CLAUDE.md"). That is the
// whole reason the tree moved under worklog/ — go:embed cannot reach outside
// its module, so before the move there was no embedded source to be a peer of
// the checkout one.
type Source struct {
	// FS is rooted at the skills dir. Never nil for a usable Source.
	FS fs.FS
	// Root is the checkout this came from, or "" when embedded. It is the
	// ONLY signal that a real repo is present: git operations, self-rebuild
	// and tone-symlink ownership all gate on it being non-empty. Do not
	// synthesize a path here for the embedded case.
	Root string
	// Label names the source in output, e.g. "the checkout at /x" or
	// "the copy built into this binary".
	Label string
}

// Embedded returns a Source backed by the binary's own copy.
func Embedded(fsys fs.FS) Source {
	return Source{FS: fsys, Label: "the copy built into this binary"}
}

// Checkout returns a Source reading a checkout's skills dir. It does not
// verify; call VerifySource.
func Checkout(repoRoot string) Source {
	return Source{
		FS:    os.DirFS(filepath.Join(repoRoot, skillsRel)),
		Root:  repoRoot,
		Label: "the checkout at " + repoRoot,
	}
}

// VerifySource confirms every deploy source exists and is readable BEFORE
// anything destructive runs. A missing source must abort the run, never
// delete a deployed copy (deploying over rm -rf without this check would
// turn a renamed skill into data loss).
//
// The embedded source is checked with exactly the same rules as a checkout.
// It cannot realistically fail — the manifest test in package skills would
// have caught it first — but skipping the check for one source and not the
// other is how the rename-becomes-deletion hole gets reopened.
func VerifySource(src Source) error {
	if src.FS == nil {
		return fmt.Errorf("no skill source available")
	}
	var missing []string
	for _, d := range skillDirs {
		if fi, err := fs.Stat(src.FS, path.Join(d, "SKILL.md")); err != nil || !fi.Mode().IsRegular() {
			missing = append(missing, d+"/SKILL.md")
		}
	}
	for _, f := range []string{worklogSkillRel, claudeCommandRel} {
		if fi, err := fs.Stat(src.FS, f); err != nil || !fi.Mode().IsRegular() {
			missing = append(missing, f)
		}
	}
	if fi, err := fs.Stat(src.FS, worklogRefsRel); err != nil || !fi.IsDir() {
		missing = append(missing, worklogRefsRel)
	}
	if len(missing) > 0 {
		return fmt.Errorf("%s is missing skill sources (%v); refusing to deploy", src.Label, missing)
	}
	return nil
}

// Run deploys every skill to every target per mode. Callers must have run
// VerifySource first; Run re-checks as a belt-and-suspenders guard.
func Run(src Source, targets []string, home string, mode Mode) (Report, error) {
	var rep Report
	if err := VerifySource(src); err != nil {
		return rep, err
	}
	for _, t := range targets {
		if err := ValidateTarget(t); err != nil {
			return rep, err
		}
	}
	for _, target := range targets {
		for _, d := range skillDirs {
			deployDir(&rep, src, d, filepath.Join(target, d),
				fmt.Sprintf("skill %s -> %s", d, target), mode)
		}
		deployFile(&rep, src, worklogSkillRel,
			filepath.Join(target, "worklog", "SKILL.md"),
			fmt.Sprintf("skill worklog -> %s", target), mode)
		deployDir(&rep, src, worklogRefsRel,
			filepath.Join(target, "worklog", "references"),
			fmt.Sprintf("skill worklog references -> %s", target), mode)
		if target == filepath.Join(home, ".claude", "skills") {
			deployFile(&rep, src, claudeCommandRel,
				filepath.Join(home, claudeCommandsDir, "worklog.md"),
				"command worklog -> ~/.claude/commands", mode)
		}
	}
	return rep, nil
}

// Directive returns the CLAUDE.md body this source carries. It is read the
// same way the skills are, which is the point: the directive used to be read
// from <repoRoot>/CLAUDE.md directly, so --with-claude-md silently did
// nothing on a machine with no checkout.
func Directive(src Source) ([]byte, error) {
	if src.FS == nil {
		return nil, fmt.Errorf("no skill source available")
	}
	return fs.ReadFile(src.FS, directiveRel)
}

func deployDir(rep *Report, src Source, srcPath, dst, label string, mode Mode) {
	if fi, err := os.Lstat(dst); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		switch mode {
		case ModeCheck:
			rep.stale("%s: legacy symlink (re-run install to convert to a copy)", label)
			return
		case ModeDryRun:
			rep.plan("replace symlink %s with a copy", dst)
			return
		case ModeInstall:
			if err := os.Remove(dst); err != nil {
				rep.warn("%s: cannot remove legacy symlink: %v", label, err)
				return
			}
		}
	}
	equal, _ := dirsEqual(src.FS, srcPath, dst)
	if equal {
		if mode == ModeInstall {
			rep.note("%s: up to date", label)
		}
		return
	}
	switch mode {
	case ModeCheck:
		rep.stale("%s: missing or differs from repo", label)
	case ModeDryRun:
		rep.plan("copy %s -> %s", srcPath, dst)
	case ModeInstall:
		if err := os.RemoveAll(dst); err != nil {
			rep.warn("%s: %v", label, err)
			return
		}
		if err := copyDir(src.FS, srcPath, dst); err != nil {
			rep.warn("%s: %v", label, err)
			return
		}
		rep.note("%s: copied", label)
	}
}

func deployFile(rep *Report, src Source, srcPath, dst, label string, mode Mode) {
	equal, _ := filesEqual(src.FS, srcPath, dst)
	if equal {
		if mode == ModeInstall {
			rep.note("%s: up to date", label)
		}
		return
	}
	switch mode {
	case ModeCheck:
		rep.stale("%s: missing or differs from repo", label)
	case ModeDryRun:
		rep.plan("copy %s -> %s", srcPath, dst)
	case ModeInstall:
		data, err := fs.ReadFile(src.FS, srcPath)
		if err != nil {
			rep.warn("%s: %v", label, err)
			return
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			rep.warn("%s: %v", label, err)
			return
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			rep.warn("%s: %v", label, err)
			return
		}
		rep.note("%s: copied", label)
	}
}

// dirsEqual reports whether dst mirrors the source subtree exactly (same
// relative file set, same bytes). Extra files in dst count as unequal.
//
// The source side walks an fs.FS with slash paths; the destination side is
// the real filesystem with OS paths. Mixing the two is the easy mistake here,
// so the joins are deliberately different: path.Join above, filepath.Join
// below. Modes are not compared, and need not be: embed.FS reports every file
// as 0444 while a checkout reports 0644, but copyDir writes 0644 either way,
// so what lands on disk is identical regardless of which source produced it.
func dirsEqual(fsys fs.FS, srcPath, dst string) (bool, error) {
	srcSet := map[string][]byte{}
	err := fs.WalkDir(fsys, srcPath, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(p, srcPath), "/")
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		srcSet[filepath.FromSlash(rel)] = data
		return nil
	})
	if err != nil {
		return false, err
	}
	seen := 0
	err = filepath.WalkDir(dst, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dst, p)
		want, ok := srcSet[rel]
		if !ok {
			return fmt.Errorf("extra")
		}
		got, err := os.ReadFile(p)
		if err != nil || !bytes.Equal(got, want) {
			return fmt.Errorf("differs")
		}
		seen++
		return nil
	})
	if err != nil {
		return false, nil //nolint:nilerr // any walk error means "not equal"
	}
	return seen == len(srcSet), nil
}

func filesEqual(fsys fs.FS, srcPath, dst string) (bool, error) {
	da, err := fs.ReadFile(fsys, srcPath)
	if err != nil {
		return false, err
	}
	db, err := os.ReadFile(dst)
	if err != nil {
		return false, nil //nolint:nilerr // missing dst = unequal
	}
	return bytes.Equal(da, db), nil
}

func copyDir(fsys fs.FS, srcPath, dst string) error {
	return fs.WalkDir(fsys, srcPath, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(p, srcPath), "/")
		out := filepath.Join(dst, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		// 0644 regardless of source: embed.FS reports 0444, and inheriting
		// that would leave a read-only skill tree that the next install
		// could not overwrite.
		return os.WriteFile(out, data, 0o644)
	})
}
