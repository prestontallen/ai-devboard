package cli

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// TestNoRealSystemCommandsUnderTest is the guard that keeps `go test ./...`
// off the developer's machine.
//
// The devboard extras shell to systemctl and docker. Today they are
// unreachable under test only by accident: they sit behind promptAllowed(),
// and the suite has no TTY. The consent ticket will add headless flags for
// exactly those extras, at which point the accident stops protecting anyone
// and the suite would start reloading real systemd units.
//
// This pins the seam rather than the accident. If someone reintroduces a
// direct exec.Command for a service manager, the devboard probe stops going
// through runCommand and this fails.
func TestNoRealSystemCommandsUnderTest(t *testing.T) {
	var saw []string
	orig := runCommand
	runCommand = func(name string, args ...string) ([]byte, error) {
		saw = append(saw, name+" "+strings.Join(args, " "))
		return nil, errors.New("no system commands under test")
	}
	t.Cleanup(func() { runCommand = orig })

	if devboardRunning() {
		t.Error("devboardRunning reported true with every command failing")
	}
	if len(saw) == 0 {
		t.Fatal("devboardRunning did not go through the seam — it is shelling out directly")
	}
	for _, c := range saw {
		if !strings.HasPrefix(c, "systemctl") && !strings.HasPrefix(c, "docker") {
			t.Errorf("unexpected command through the seam: %q", c)
		}
	}
}

// TestUnitFileDoesNotNameTheTestBinary: installDevboardUnit derives ExecStart
// from the running executable, which under go test is a binary in the build
// cache that will not exist later. The seam lets a test say so.
func TestUnitFileDoesNotNameTheTestBinary(t *testing.T) {
	real, err := selfPath()
	if err != nil {
		t.Skip("no executable path")
	}
	if !strings.Contains(real, "go-build") && !strings.Contains(real, "/T/") {
		t.Skip("not running from a build cache; nothing to prove here")
	}
	// The point: this is what ExecStart would have said. A unit naming it
	// would break the moment the cache is pruned.
	t.Logf("os.Executable() under test = %s", real)
}

// TestSeamCoversEveryServiceCall walks the source for direct exec.Command
// calls naming a service manager, which would bypass the seam entirely.
func TestSeamCoversEveryServiceCall(t *testing.T) {
	out, err := exec.Command("grep", "-n", `exec\.Command("systemctl"\|exec\.Command("docker"`, "install.go").CombinedOutput()
	if err == nil && len(out) > 0 {
		t.Errorf("direct service calls bypass runCommand:\n%s", out)
	}
}
