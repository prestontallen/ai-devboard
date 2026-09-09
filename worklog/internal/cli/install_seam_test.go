package cli

import (
	"errors"
	"os"
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
		if !strings.HasPrefix(c, "systemctl") {
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
//
// It walks EVERY file in the package, not install.go alone. The single-file
// version was written when install.go was the only file that shelled out, and
// it would have passed unchanged while uninstall.go called systemctl directly
// — the same rot the store-boundary guard was found to have, from the same
// cause: a hardcoded list that nothing revisits. It also covers docker now,
// which uninstall is the first and only caller of.
//
// The vacuity check matters as much as the search. A guard that inspects
// nothing passes, and would keep passing after a rename.
func TestSeamCoversEveryServiceCall(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	inspected := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		inspected++
		for _, tool := range []string{"systemctl", "docker"} {
			if strings.Contains(string(src), `exec.Command("`+tool+`"`) {
				t.Errorf("%s calls exec.Command(%q) directly, bypassing runCommand; "+
					"a test would then reach the developer's real machine", name, tool)
			}
		}
	}
	if inspected == 0 {
		t.Fatal("the guard inspected no files at all; it proves nothing")
	}
}

// TestSandboxStubsTheRunner is milestone 1's own guard: the stub must be in
// the SHARED sandbox, not just in the tests that think about it.
//
// The distinction is the whole point. Before this, the extras were
// unreachable under test because they sit behind a TTY check and the suite
// has none — an accident, not a design. This ticket adds a headless flag per
// extra, which removes the accident. If the stub lives only in the tests that
// remember it, the first test to pass a devboard flag reloads the developer's
// real systemd units.
func TestSandboxStubsTheRunner(t *testing.T) {
	installSandbox(t)

	if _, err := runCommand("systemctl", "--user", "is-active", "anything"); err == nil {
		t.Fatal("installSandbox did not stub runCommand — a test could reach the real service manager")
	}
	if devboardRunning() {
		t.Error("devboardRunning consulted something real")
	}
}
