package cli

import (
	"bytes"
	"strings"
	"testing"
)

func runPing(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	cmd := newRoot()
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(append([]string{"ping"}, args...))
	err = cmd.Execute()
	return out.String(), errBuf.String(), err
}

func TestPingHappy(t *testing.T) {
	out, _, err := runPing(t)
	if err != nil {
		t.Fatalf("ping: %v", err)
	}
	if out != "pong\n" {
		t.Fatalf("stdout = %q, want %q", out, "pong\n")
	}
}

func TestPingExtraArgs(t *testing.T) {
	out, _, err := runPing(t, "nope")
	if err == nil {
		t.Fatal("expected error for extra args")
	}
	if strings.Contains(out, "pong") {
		t.Fatalf("success pong on extra args, stdout=%q", out)
	}
}

func TestPingRegistered(t *testing.T) {
	found := newRoot().Commands()
	for _, c := range found {
		if c.Name() == "ping" {
			return
		}
	}
	t.Fatal("root is missing ping")
}
