// Package installer implements `worklog install`: target configuration,
// agent-dir detection, and skill deployment with check/dry-run modes. The
// bash install.sh is only a bootstrap that obtains this binary and execs
// `worklog install` — everything decision-shaped lives here.
package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config is what persists between runs. On disk it is a plain text file,
// backward-compatible with the original bash format (one target path per
// line, '#' comments and blanks ignored). The richer format is additive:
// a line "repo <path>" records the checkout the skills deploy from.
type Config struct {
	RepoRoot string
	Targets  []string
}

// ConfPath returns the config file location, honoring XDG_CONFIG_HOME.
func ConfPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "ai-devboard", "targets")
}

// LoadConfig parses the config file. A missing file returns an empty
// Config and ok=false, never an error — absence means "not configured".
// Unparseable content cannot happen by design: every non-comment line is
// either a "repo " directive or a target path (with ~ expanded), so old
// bash-era files load unchanged.
func LoadConfig(path string) (Config, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, false, nil
		}
		return Config{}, false, err
	}
	var cfg Config
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if rest, found := strings.CutPrefix(line, "repo "); found {
			cfg.RepoRoot = ExpandTilde(strings.TrimSpace(rest))
			continue
		}
		cfg.Targets = append(cfg.Targets, ExpandTilde(line))
	}
	return cfg, true, nil
}

// SaveConfig writes the config atomically (temp + rename).
func SaveConfig(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# ai-devboard install config — edit freely, or rerun install.sh interactively.\n")
	b.WriteString("# 'repo <path>' names the checkout; every other line is a skill target dir.\n")
	if cfg.RepoRoot != "" {
		fmt.Fprintf(&b, "repo %s\n", cfg.RepoRoot)
	}
	for _, t := range cfg.Targets {
		b.WriteString(t + "\n")
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".targets-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.WriteString(b.String()); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	return os.Rename(name, path)
}

// ExpandTilde replaces a leading ~ with the home directory.
func ExpandTilde(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	return p
}

// ValidateTarget rejects paths that cannot be deployment targets: empty,
// relative (the class of bug that once created a repo-root dir named "n"),
// or root itself.
func ValidateTarget(p string) error {
	if strings.TrimSpace(p) == "" {
		return fmt.Errorf("empty target path")
	}
	if !filepath.IsAbs(p) {
		return fmt.Errorf("target %q is not an absolute path", p)
	}
	if filepath.Clean(p) == "/" {
		return fmt.Errorf("target %q is the filesystem root", p)
	}
	return nil
}

// DetectTargets returns <agent-dir>/skills for every known local AI agent
// dir that exists under home.
func DetectTargets(home string) []string {
	var out []string
	for _, d := range []string{".claude", ".cursor", ".windsurf", ".codex"} {
		base := filepath.Join(home, d)
		if fi, err := os.Stat(base); err == nil && fi.IsDir() {
			out = append(out, filepath.Join(base, "skills"))
		}
	}
	return out
}
