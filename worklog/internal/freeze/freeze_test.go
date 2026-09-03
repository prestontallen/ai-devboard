package freeze

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAcquireCreatesSentinelWithInfo(t *testing.T) {
	dir := t.TempDir()
	info, err := Acquire(dir, "cutover")
	if err != nil {
		t.Fatal(err)
	}
	if info.PID != os.Getpid() {
		t.Errorf("PID = %d, want %d", info.PID, os.Getpid())
	}
	if info.Reason != "cutover" {
		t.Errorf("Reason = %q, want %q", info.Reason, "cutover")
	}
	if info.Acquired.IsZero() {
		t.Error("Acquired is zero")
	}
	if _, err := os.Stat(filepath.Join(dir, sentinelName)); err != nil {
		t.Errorf("sentinel not on disk: %v", err)
	}
}

func TestAcquireTwiceFails(t *testing.T) {
	dir := t.TempDir()
	if _, err := Acquire(dir, "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(dir, "second"); !errors.Is(err, ErrAlreadyFrozen) {
		t.Errorf("second Acquire error = %v, want ErrAlreadyFrozen", err)
	}
}

func TestReleaseThenAcquireSucceeds(t *testing.T) {
	dir := t.TempDir()
	if _, err := Acquire(dir, "first"); err != nil {
		t.Fatal(err)
	}
	if err := Release(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(dir, "second"); err != nil {
		t.Errorf("re-acquire after release: %v", err)
	}
}

func TestReleaseWithoutFreezeIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	if err := Release(dir); err != nil {
		t.Errorf("Release with no sentinel: %v", err)
	}
}

func TestCheckUnfrozen(t *testing.T) {
	dir := t.TempDir()
	frozen, _, err := Check(dir)
	if err != nil {
		t.Fatal(err)
	}
	if frozen {
		t.Error("Check reported frozen with no sentinel present")
	}
}

func TestCheckFrozenReturnsInfo(t *testing.T) {
	dir := t.TempDir()
	if _, err := Acquire(dir, "cutover window"); err != nil {
		t.Fatal(err)
	}
	frozen, info, err := Check(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !frozen {
		t.Fatal("Check reported unfrozen with sentinel present")
	}
	if info.Reason != "cutover window" {
		t.Errorf("Reason = %q, want %q", info.Reason, "cutover window")
	}
}

// TestCheckUnparseableSentinelStillReadsFrozen is the load-bearing case: an
// unparseable sentinel must never be treated as "no freeze held".
func TestCheckUnparseableSentinelStillReadsFrozen(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, sentinelName), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	frozen, _, err := Check(dir)
	if err != nil {
		t.Fatalf("unparseable sentinel returned an error instead of frozen=true: %v", err)
	}
	if !frozen {
		t.Fatal("unparseable sentinel read as unfrozen — a corrupt sentinel must fail safe as frozen")
	}
}

func TestCheckEmptySentinelStillReadsFrozen(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, sentinelName), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	frozen, _, err := Check(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !frozen {
		t.Fatal("empty sentinel read as unfrozen")
	}
}

func TestRefusalErrorNamesReasonAndPID(t *testing.T) {
	err := RefusalError(Info{PID: 4242, Reason: "cutover"})
	msg := err.Error()
	for _, want := range []string{"cutover", "4242", "frozen"} {
		if !strings.Contains(msg, want) {
			t.Errorf("RefusalError message missing %q: %q", want, msg)
		}
	}
}
