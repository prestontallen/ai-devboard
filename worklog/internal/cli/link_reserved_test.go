package cli

import (
	"strings"
	"testing"
)

// "PR" is reserved on link because OnPR and OnLink share one label array and
// match labels case-insensitively. Every path that names a link has to refuse
// it: --clear was the one that deleted the mirrored PR entry outright.
func TestLinkCLIRejectsReservedPRName(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"set", []string{"auth-1", "PR", "https://evil.example/other"}},
		{"set lowercase", []string{"auth-1", "pr", "https://evil.example/other"}},
		{"set mixed case", []string{"auth-1", "Pr", "https://evil.example/other"}},
		{"read", []string{"auth-1", "PR"}},
		{"clear", []string{"auth-1", "PR", "--clear"}},
		{"edit", []string{"auth-1", "PR", "--edit"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := linkFixtureDir(t)
			out, err := invokeLink(t, dir, tc.args...)
			if err == nil {
				t.Fatalf("expected refusal, got success\nout: %s", out)
			}
			ec, ok := err.(exitCoder)
			if !ok || ec.ExitCode() != 64 {
				t.Fatalf("exit code = %v (want 64), err: %v", ec, err)
			}
			if !strings.Contains(err.Error(), "reserved") ||
				!strings.Contains(err.Error(), "worklog pr") {
				t.Errorf("error should name the reservation and point at worklog pr: %v", err)
			}
		})
	}
}

// A link whose name merely contains "pr" is not reserved.
func TestLinkCLIAllowsNamesContainingPR(t *testing.T) {
	dir := linkFixtureDir(t)
	out, err := invokeLink(t, dir, "auth-1", "preview", "https://preview.example/1")
	if err != nil {
		t.Fatalf("invokeLink: %v\nout: %s", err, out)
	}
}
