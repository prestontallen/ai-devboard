package installer

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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
// plus worklog's SKILL.md, plus the claude-only command file.
var skillDirs = []string{"dev-context", "contract", "fan-out"}

const (
	worklogSkillRel   = "worklog/skill/SKILL.md"
	claudeCommandRel  = "worklog/skill/claude/command.md"
	claudeCommandsDir = ".claude/commands"
)

// VerifyRepo confirms every deploy source exists and is readable BEFORE
// anything destructive runs. A missing source must abort the run, never
// delete a deployed copy (deploying over rm -rf without this check would
// turn a renamed skill into data loss).
func VerifyRepo(repoRoot string) error {
	if fi, err := os.Stat(repoRoot); err != nil || !fi.IsDir() {
		return fmt.Errorf("repo not found at %s; re-run install.sh from a checkout", repoRoot)
	}
	var missing []string
	for _, d := range skillDirs {
		if fi, err := os.Stat(filepath.Join(repoRoot, d, "SKILL.md")); err != nil || !fi.Mode().IsRegular() {
			missing = append(missing, d+"/SKILL.md")
		}
	}
	for _, f := range []string{worklogSkillRel, claudeCommandRel} {
		if fi, err := os.Stat(filepath.Join(repoRoot, f)); err != nil || !fi.Mode().IsRegular() {
			missing = append(missing, f)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("repo at %s is missing skill sources (%v); refusing to deploy", repoRoot, missing)
	}
	return nil
}

// Run deploys every skill to every target per mode. Callers must have run
// VerifyRepo first; Run re-checks as a belt-and-suspenders guard.
func Run(repoRoot string, targets []string, home string, mode Mode) (Report, error) {
	var rep Report
	if err := VerifyRepo(repoRoot); err != nil {
		return rep, err
	}
	for _, t := range targets {
		if err := ValidateTarget(t); err != nil {
			return rep, err
		}
	}
	for _, target := range targets {
		for _, d := range skillDirs {
			deployDir(&rep, filepath.Join(repoRoot, d), filepath.Join(target, d),
				fmt.Sprintf("skill %s -> %s", d, target), mode)
		}
		deployFile(&rep, filepath.Join(repoRoot, worklogSkillRel),
			filepath.Join(target, "worklog", "SKILL.md"),
			fmt.Sprintf("skill worklog -> %s", target), mode)
		if target == filepath.Join(home, ".claude", "skills") {
			deployFile(&rep, filepath.Join(repoRoot, claudeCommandRel),
				filepath.Join(home, claudeCommandsDir, "worklog.md"),
				"command worklog -> ~/.claude/commands", mode)
		}
	}
	return rep, nil
}

func deployDir(rep *Report, src, dst, label string, mode Mode) {
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
	equal, _ := dirsEqual(src, dst)
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
		rep.plan("copy %s -> %s", src, dst)
	case ModeInstall:
		if err := os.RemoveAll(dst); err != nil {
			rep.warn("%s: %v", label, err)
			return
		}
		if err := copyDir(src, dst); err != nil {
			rep.warn("%s: %v", label, err)
			return
		}
		rep.note("%s: copied", label)
	}
}

func deployFile(rep *Report, src, dst, label string, mode Mode) {
	equal, _ := filesEqual(src, dst)
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
		rep.plan("copy %s -> %s", src, dst)
	case ModeInstall:
		data, err := os.ReadFile(src)
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

// dirsEqual reports whether dst mirrors src exactly (same relative file
// set, same bytes). Extra files in dst count as unequal.
func dirsEqual(src, dst string) (bool, error) {
	srcSet := map[string][]byte{}
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		srcSet[rel] = data
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

func filesEqual(a, b string) (bool, error) {
	da, err := os.ReadFile(a)
	if err != nil {
		return false, err
	}
	db, err := os.ReadFile(b)
	if err != nil {
		return false, nil //nolint:nilerr // missing dst = unequal
	}
	return bytes.Equal(da, db), nil
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(out, data, 0o644)
	})
}
