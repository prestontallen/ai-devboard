package installer

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Consent records what the human said about each optional piece of the
// install, so it is asked once rather than every run.
//
// It lives in its OWN file, beside the targets config rather than inside it,
// and that is not tidiness. The targets parser has no concept of an unknown
// line: anything that is not blank, not a comment and not the one recognised
// prefix becomes a target path, which then fails validation for not being
// absolute and aborts the whole install. So a decision line added to that
// file would break every previously released binary outright. Worse, saving
// that file is not a round trip — it is rebuilt from the parsed struct — so
// an older binary doing an ordinary save would erase the record silently and
// the human would simply be asked everything again.
//
// A sibling file is invisible to those binaries and cannot be destroyed by
// them. The cost is that a machine can carry targets without decisions, so
// nothing here may assume the two appear together.
type Consent struct {
	// Decisions maps an extra's name to what the human said. Absent means
	// never asked, which is a THIRD state and must not be collapsed into
	// declined — the difference is exactly what makes "ask once" possible.
	Decisions map[string]Decision
}

// Decision is one answer.
type Decision string

const (
	Accepted Decision = "accepted"
	Declined Decision = "declined"
)

// The extras this record covers. Names are stable identifiers written to
// disk; changing one silently resets that decision for every machine.
const (
	ExtraSkills       = "skills"
	ExtraSessionHook  = "session-hook"
	ExtraClaudeMD     = "claude-md"
	ExtraDevboardUnit = "devboard-unit"
)

// Extras is the canonical set, so a caller cannot iterate a stale list.
func Extras() []string {
	return []string{ExtraSkills, ExtraSessionHook, ExtraClaudeMD, ExtraDevboardUnit}
}

// ConsentPath is the record's location: a sibling of the targets config.
func ConsentPath() string {
	conf := ConfPath()
	if conf == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(conf), "consent")
}

// LoadConsent reads the record. A missing file is not an error — it is a
// machine that has never been asked, which is a legitimate state.
func LoadConsent(path string) (Consent, error) {
	c := Consent{Decisions: map[string]Decision{}}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, answer, ok := strings.Cut(line, " ")
		if !ok {
			continue // unreadable line: ignore rather than guess
		}
		switch Decision(strings.TrimSpace(answer)) {
		case Accepted:
			c.Decisions[name] = Accepted
		case Declined:
			c.Decisions[name] = Declined
		}
	}
	return c, sc.Err()
}

// SaveConsent writes the record atomically, preserving nothing it did not
// write — unlike the targets config, this file has one author.
func SaveConsent(path string, c Consent) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# ai-devboard: what you said about each optional piece.\n")
	b.WriteString("# Delete a line to be asked again. Written by `worklog install`.\n")
	names := make([]string, 0, len(c.Decisions))
	for n := range c.Decisions {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(&b, "%s %s\n", n, c.Decisions[n])
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".consent-*")
	if err != nil {
		return err
	}
	if _, err := tmp.WriteString(b.String()); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Sync(); err != nil { // the record must survive a crash
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Asked reports whether this extra has an answer on record.
func (c Consent) Asked(extra string) bool {
	_, ok := c.Decisions[extra]
	return ok
}

// Accepted reports whether the human said yes. A never-asked extra is not
// accepted, and is also not declined; callers that care must use Asked.
func (c Consent) Accepted(extra string) bool {
	return c.Decisions[extra] == Accepted
}

// Record sets an answer.
func (c Consent) Record(extra string, d Decision) { c.Decisions[extra] = d }
