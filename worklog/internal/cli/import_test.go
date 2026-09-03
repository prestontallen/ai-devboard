package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func invokeImport(t *testing.T, dir string, stdin string, args ...string) (string, error) {
	t.Helper()
	prev := flagDir
	flagDir = dir
	t.Cleanup(func() { flagDir = prev })

	cmd := newImportCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	// Wire stdin via --file workaround: write to a temp file if stdin content given.
	allArgs := args
	if stdin != "" {
		f, err := os.CreateTemp(t.TempDir(), "stdin-*.json")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString(stdin); err != nil {
			t.Fatal(err)
		}
		f.Close()
		allArgs = append(args, "--file", f.Name())
	}
	cmd.SetArgs(allArgs)

	err := cmd.Execute()
	return stdout.String(), err
}

func TestImportStdinJSON(t *testing.T) {
	dir, _, _ := storeWriteFixture(t)
	out, err := invokeImport(t, dir, `{"id":"foo-1","title":"Standalone"}`, "--json")
	if err != nil {
		t.Fatalf("invokeImport: %v\nout: %s", err, out)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("json: %v\nout: %s", err, out)
	}
	imported, _ := res["imported"].([]any)
	if len(imported) != 1 {
		t.Errorf("imported = %d, want 1", len(imported))
	}
	wmd, _ := os.ReadFile(filepath.Join(dir, "WORK.md"))
	if !strings.Contains(string(wmd), "foo-1") {
		t.Errorf("expected foo-1 in WORK.md:\n%s", wmd)
	}
}

func TestImportFileJSON(t *testing.T) {
	dir, _, _ := storeWriteFixture(t)
	f, err := os.CreateTemp(t.TempDir(), "tickets-*.json")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"id":"file-1","title":"From file"}`)
	f.Close()

	prev := flagDir
	flagDir = dir
	t.Cleanup(func() { flagDir = prev })

	cmd := newImportCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--file", f.Name(), "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\nout: %s", err, buf.String())
	}
	wmd, _ := os.ReadFile(filepath.Join(dir, "WORK.md"))
	if !strings.Contains(string(wmd), "file-1") {
		t.Errorf("expected file-1 in WORK.md:\n%s", wmd)
	}
}

func TestImportSectionOverride(t *testing.T) {
	dir, _, _ := storeWriteFixture(t)
	out, err := invokeImport(t, dir, `{"id":"bar-1","title":"Override","section":"next"}`,
		"--section", "someday", "--json")
	if err != nil {
		t.Fatalf("invokeImport: %v\nout: %s", err, out)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("json: %v\nout: %s", err, out)
	}
	imported, _ := res["imported"].([]any)
	if len(imported) != 1 {
		t.Fatalf("imported = %d, want 1", len(imported))
	}
	row, _ := imported[0].(map[string]any)
	if row["section"] != "someday" {
		t.Errorf("section = %q, want someday", row["section"])
	}
}

func TestImportDryRun(t *testing.T) {
	dir, _, _ := storeWriteFixture(t)
	before, _ := os.ReadFile(filepath.Join(dir, "WORK.md"))
	out, err := invokeImport(t, dir, `{"id":"dry-1","title":"Dry run"}`,
		"--dry-run", "--json")
	if err != nil {
		t.Fatalf("invokeImport: %v\nout: %s", err, out)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("json: %v\nout: %s", err, out)
	}
	imported, _ := res["imported"].([]any)
	if len(imported) != 1 {
		t.Errorf("imported (dry-run) = %d, want 1", len(imported))
	}
	after, _ := os.ReadFile(filepath.Join(dir, "WORK.md"))
	if string(before) != string(after) {
		t.Error("--dry-run mutated WORK.md")
	}
}

func TestImportJSONResult(t *testing.T) {
	dir, _, _ := storeWriteFixture(t)
	out, err := invokeImport(t, dir,
		`[{"id":"ok-1","title":"Good"},{"id":"","title":"No ID"}]`,
		"--json")
	if err == nil {
		t.Fatal("expected error due to failed ticket")
	}
	var res map[string]any
	if err2 := json.Unmarshal([]byte(out), &res); err2 != nil {
		t.Fatalf("json: %v\nout: %s", err2, out)
	}
	imported, _ := res["imported"].([]any)
	if len(imported) != 1 {
		t.Errorf("imported = %d, want 1", len(imported))
	}
	failed, _ := res["failed"].([]any)
	if len(failed) != 1 {
		t.Errorf("failed = %d, want 1", len(failed))
	}
}

func TestImportExitCodes(t *testing.T) {
	// All success → exit 0.
	dir, _, _ := storeWriteFixture(t)
	_, err := invokeImport(t, dir, `{"id":"good-1","title":"Good"}`, "--json")
	if err != nil {
		t.Errorf("all-success should exit 0, got: %v", err)
	}

	// Any failure → exit 1.
	_, err = invokeImport(t, dir, `{"id":"good-1","title":"Dup"}`, "--json")
	if err == nil {
		t.Error("failure case should exit 1")
	}
	if ec, ok := err.(exitCoder); ok {
		if ec.ExitCode() != 1 {
			t.Errorf("exit code = %d, want 1", ec.ExitCode())
		}
	}
}
