package installer

import (
	"os"
	"path/filepath"
	"testing"
)

// TestNeverAskedIsNotDeclined is the distinction the whole record exists for.
// Collapsing the two is what makes "ask once" impossible: a machine that has
// never been asked and a machine that said no look identical, so either you
// re-ask forever or you never ask at all.
func TestNeverAskedIsNotDeclined(t *testing.T) {
	c, err := LoadConsent(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatalf("a missing record is a legitimate state, not an error: %v", err)
	}
	for _, extra := range Extras() {
		if c.Asked(extra) {
			t.Errorf("%s reported as asked on a fresh machine", extra)
		}
		if c.Accepted(extra) {
			t.Errorf("%s reported as accepted on a fresh machine", extra)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "consent")
	c, _ := LoadConsent(path)
	c.Record(ExtraSessionHook, Accepted)
	c.Record(ExtraDevboardUnit, Declined)
	if err := SaveConsent(path, c); err != nil {
		t.Fatal(err)
	}

	got, err := LoadConsent(path)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Accepted(ExtraSessionHook) {
		t.Error("accepted decision did not survive")
	}
	if !got.Asked(ExtraDevboardUnit) || got.Accepted(ExtraDevboardUnit) {
		t.Error("declined decision did not survive as declined-and-asked")
	}
	if got.Asked(ExtraClaudeMD) {
		t.Error("an extra nobody answered came back asked")
	}
}

// TestOlderBinaryIsUnaffected is why this is a sibling file rather than lines
// in the targets config. That parser turns any unrecognised line into a
// target path, which then fails validation and aborts the install — so a
// decision line there would break every previously released binary. The
// record must be invisible to it.
func TestOlderBinaryIsUnaffected(t *testing.T) {
	dir := t.TempDir()
	confPath := filepath.Join(dir, "targets")
	if err := os.WriteFile(confPath, []byte("/home/someone/.claude/skills\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := Consent{Decisions: map[string]Decision{ExtraSessionHook: Declined}}
	if err := SaveConsent(filepath.Join(dir, "consent"), c); err != nil {
		t.Fatal(err)
	}

	cfg, ok, err := LoadConfig(confPath)
	if err != nil || !ok {
		t.Fatalf("the targets config stopped loading: ok=%v err=%v", ok, err)
	}
	if len(cfg.Targets) != 1 {
		t.Errorf("the consent record leaked into the targets config: %v", cfg.Targets)
	}
	for _, tgt := range cfg.Targets {
		if err := ValidateTarget(tgt); err != nil {
			t.Errorf("target %q became invalid: %v", tgt, err)
		}
	}
}

// TestUnreadableLinesAreIgnored: the record is machine-written but a human
// can open it, and a mangled line must not be guessed at in either direction.
func TestUnreadableLinesAreIgnored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "consent")
	body := "# a comment\n\nsession-hook accepted\nnonsense\nclaude-md maybe\ndevboard-unit declined\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := LoadConsent(path)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Accepted(ExtraSessionHook) {
		t.Error("a good line before the bad ones was lost")
	}
	if !c.Asked(ExtraDevboardUnit) {
		t.Error("a good line after the bad ones was lost")
	}
	if c.Asked(ExtraClaudeMD) {
		t.Error(`"maybe" was interpreted as an answer`)
	}
}
