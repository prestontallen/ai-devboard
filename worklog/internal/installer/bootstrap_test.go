package installer_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// install.sh is the one part of the installer that no Go test covered, and it
// is the part a fresh machine runs first. Every failure pinned here was live:
// the script aborted on its own first executable line under `curl | bash`,
// --help read a file that a pipe does not have, and the checksum step called a
// command macOS does not ship.
//
// These shell out to bash against the real script rather than re-implementing
// it, because a copy of the logic would pass while the shipped file broke.

// repoRoot walks up to the directory holding install.sh.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "install.sh")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Skip("install.sh not found above the test dir; not a checkout")
	return ""
}

func bashOrSkip(t *testing.T) string {
	t.Helper()
	sh, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not on PATH")
	}
	return sh
}

// scratchEnv isolates HOME *and* XDG_CONFIG_HOME. Overriding HOME alone is not
// enough: the config lookup honors XDG_CONFIG_HOME, so a test that sets only
// HOME reaches the developer's real targets file and deploys to their real
// agent dirs.
func scratchEnv(home string) []string {
	var out []string
	for _, kv := range os.Environ() {
		k := kv[:strings.IndexByte(kv, '=')]
		if k == "HOME" || k == "XDG_CONFIG_HOME" {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"))
}

// TestBootstrapSurvivesPipe is the regression for the abort that made every
// other checkout dependency unreachable: under `set -u`, BASH_SOURCE[0] is
// unset in a pipe, so the script died before doing anything at all.
func TestBootstrapSurvivesPipe(t *testing.T) {
	sh := bashOrSkip(t)
	root := repoRoot(t)
	home := t.TempDir()

	script, err := os.ReadFile(filepath.Join(root, "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(sh, "-s", "--", "--dry-run")
	cmd.Stdin = strings.NewReader(string(script))
	cmd.Env = scratchEnv(home)
	cmd.Dir = home
	out, _ := cmd.CombinedOutput()

	if strings.Contains(string(out), "unbound variable") {
		t.Fatalf("piped run aborted on an unbound variable:\n%s", out)
	}
	// With no binary installed the run must say so, not report the machine
	// current. "absent" folded into "unknown" once, and "unknown" means
	// current, so a bare machine obtained nothing and then exec'd a binary it
	// had never written.
	if !strings.Contains(string(out), "absent") {
		t.Fatalf("piped run on a bare HOME did not report the binary absent:\n%s", out)
	}
}

// TestBootstrapNoCheckoutTouchesNoRepo pins the two arguments that are not
// no-ops when empty: `--repo ""` still consumes the empty string as the flag's
// value, and `git -C ""` runs against the caller's cwd rather than nothing.
func TestBootstrapNoCheckoutTouchesNoRepo(t *testing.T) {
	sh := bashOrSkip(t)
	root := repoRoot(t)
	home := t.TempDir()

	script, err := os.ReadFile(filepath.Join(root, "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(sh, "-xs", "--", "--dry-run")
	cmd.Stdin = strings.NewReader(string(script))
	cmd.Env = scratchEnv(home)
	cmd.Dir = home
	out, _ := cmd.CombinedOutput()

	for _, forbidden := range []string{"--repo", "git -C", "detect-platform"} {
		if strings.Contains(string(out), forbidden) {
			t.Errorf("no-checkout run used %q:\n%s", forbidden, out)
		}
	}
}

// TestBootstrapHelpNeedsNoFile covers --help, which used to be a line-numbered
// slice of $0. A pipe has no $0 to read, and any edit to the header silently
// retargeted the range.
func TestBootstrapHelpNeedsNoFile(t *testing.T) {
	sh := bashOrSkip(t)
	root := repoRoot(t)
	path := filepath.Join(root, "install.sh")

	fromFile, err := exec.Command(sh, path, "--help").CombinedOutput()
	if err != nil {
		t.Fatalf("--help from a file failed: %v\n%s", err, fromFile)
	}
	script, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	piped := exec.Command(sh, "-s", "--", "--help")
	piped.Stdin = strings.NewReader(string(script))
	fromPipe, err := piped.CombinedOutput()
	if err != nil {
		t.Fatalf("--help from a pipe failed: %v\n%s", err, fromPipe)
	}
	if string(fromFile) != string(fromPipe) {
		t.Errorf("--help differs between file and pipe:\nfile:\n%s\npipe:\n%s", fromFile, fromPipe)
	}
	if len(fromPipe) == 0 {
		t.Error("--help produced nothing")
	}
}

// TestBootstrapChecksumFallback exercises verify_checksum with sha256sum
// removed from PATH. The download path published darwin binaries and then
// verified them with a command stock macOS does not have, so a release install
// there could never get past this step.
func TestBootstrapChecksumFallback(t *testing.T) {
	sh := bashOrSkip(t)
	root := repoRoot(t)
	if _, err := exec.LookPath("shasum"); err != nil {
		t.Skip("shasum not on PATH; nothing to fall back to")
	}

	dir := t.TempDir()
	asset := "worklog_linux_amd64"
	if err := os.WriteFile(filepath.Join(dir, asset), []byte("payload\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A near-miss name proves the asset line is selected exactly: an
	// unanchored match would also pull in a longer name sharing this prefix.
	if err := os.WriteFile(filepath.Join(dir, asset+"_extra"), []byte("other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("sha256sum"); err != nil {
		t.Skip("sha256sum unavailable to build the fixture")
	}
	sumsCmd := exec.Command("sha256sum", asset, asset+"_extra")
	sumsCmd.Dir = dir
	sums, err := sumsCmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "checksums.txt"), sums, 0o644); err != nil {
		t.Fatal(err)
	}

	// A PATH with shasum but deliberately without sha256sum.
	fakeBin := t.TempDir()
	for _, c := range []string{"shasum", "grep"} {
		if p, err := exec.LookPath(c); err == nil {
			_ = os.Symlink(p, filepath.Join(fakeBin, c))
		}
	}

	// Extract the real function rather than testing a copy of it. Pulled out
	// in Go, not with sed, because the restricted PATH below deliberately
	// holds almost nothing — including sed.
	fn := extractShellFunc(t, filepath.Join(root, "install.sh"), "verify_checksum")
	prog := `
set -euo pipefail
asset="` + asset + `"
` + fn + `
if command -v sha256sum >/dev/null 2>&1; then echo "FIXTURE: sha256sum still present" >&2; exit 3; fi
verify_checksum "` + dir + `"
`
	cmd := exec.Command(sh, "-c", prog)
	cmd.Env = append(os.Environ(), "PATH="+fakeBin)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("checksum fallback failed without sha256sum: %v\n%s", err, out)
	}

	// And it must still reject tampered bytes.
	if err := os.WriteFile(filepath.Join(dir, asset), []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command(sh, "-c", prog)
	cmd.Env = append(os.Environ(), "PATH="+fakeBin)
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("checksum fallback accepted tampered bytes:\n%s", out)
	}
}

// extractShellFunc returns the named shell function's full text, brace to
// brace, from a script on disk.
func extractShellFunc(t *testing.T, path, name string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(b), "\n")
	start := -1
	for i, ln := range lines {
		if strings.HasPrefix(ln, name+"() {") {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("function %s() not found in %s", name, path)
	}
	for i := start; i < len(lines); i++ {
		if lines[i] == "}" {
			return strings.Join(lines[start:i+1], "\n")
		}
	}
	t.Fatalf("function %s() in %s has no closing brace at column 0", name, path)
	return ""
}
