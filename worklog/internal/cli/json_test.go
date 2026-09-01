package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestEmitJSONShape(t *testing.T) {
	var buf bytes.Buffer
	payload := map[string]any{
		"hello": "world",
		"nums":  []int{1, 2, 3},
	}
	if err := emitJSON(&buf, payload); err != nil {
		t.Fatal(err)
	}
	got := buf.String()

	// must end with a single newline
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("expected trailing newline, got %q", got)
	}
	// must be valid JSON
	var back map[string]any
	if err := json.Unmarshal([]byte(got), &back); err != nil {
		t.Fatalf("invalid JSON: %v\nbody: %s", err, got)
	}
	if back["hello"] != "world" {
		t.Errorf("roundtrip mismatch: %v", back)
	}
	// must be indented (not compact)
	if !strings.Contains(got, "\n  ") {
		t.Errorf("expected two-space indent in output:\n%s", got)
	}
}

func TestJSONErrorShape(t *testing.T) {
	var buf bytes.Buffer
	if err := emitJSON(&buf, jsonError{Error: "boom"}); err != nil {
		t.Fatal(err)
	}
	var back map[string]string
	if err := json.Unmarshal(buf.Bytes(), &back); err != nil {
		t.Fatal(err)
	}
	if back["error"] != "boom" {
		t.Errorf("error roundtrip = %v, want boom", back)
	}
}
