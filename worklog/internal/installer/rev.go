package installer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// FindRepoRoot walks up from start until it finds a directory containing
// go.mod, returning the absolute path of that directory. Returns an error if
// the filesystem root is reached without finding one.
//
// Note this locates the *module* root (worklog/), not the repo root — callers
// wanting the checkout take its parent. See runInstall's repo resolution.
func FindRepoRoot(start string) (string, error) {
	cur, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(cur, "go.mod")); err == nil {
			return cur, nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", fmt.Errorf("no go.mod found walking up from %s", start)
		}
		cur = parent
	}
}

// RepoRev computes the repo's build stamp with the SAME algorithm the bash
// bootstrap uses — short HEAD, "-dirty" suffix when the worklog/ subtree
// differs from HEAD — so staleness comparison between the binary's
// BuildCommit and this value is byte-exact. Any deviation here causes
// either rebuild-every-run or never-rebuild-after-edits.
func RepoRev(repoRoot string) (string, error) {
	out, err := exec.Command("git", "-C", repoRoot, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "", err
	}
	rev := strings.TrimSpace(string(out))
	dirty := exec.Command("git", "-C", repoRoot, "diff", "--quiet", "HEAD", "--", "worklog")
	if err := dirty.Run(); err != nil {
		rev += "-dirty"
	}
	return rev, nil
}
