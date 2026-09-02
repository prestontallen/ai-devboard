package installer

import (
	"os/exec"
	"strings"
)

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
