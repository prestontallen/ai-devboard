package version

import "testing"

// TestTheIncidentStamp is the regression test for the thing that actually
// happened: a build nine commits past v0.13.1 was replaced by the v0.13.1
// release. The stamp is the real one captured off the machine, not a
// synthetic example, because the shape is the whole bug.
func TestTheIncidentStamp(t *testing.T) {
	have := Parse("v0.13.1-9-g9cab728 (9cab728, 2026-09-08T20:16:02Z)")
	want := Parse("0.13.1")

	if !have.Known || !want.Known {
		t.Fatalf("both stamps must parse: have=%+v want=%+v", have, want)
	}
	if have.Ahead != 9 {
		t.Errorf("Ahead = %d, want 9", have.Ahead)
	}
	if got := Relate(have, want); got != Downgrade {
		t.Errorf("Relate = %q, want %q — installing the release over this build is a downgrade", got, Downgrade)
	}
}

func TestOrdering(t *testing.T) {
	cases := []struct {
		have, want string
		rel        Relation
	}{
		{"v0.13.1", "0.13.2", Upgrade},
		{"v0.13.2", "0.13.1", Downgrade},
		{"v0.13.1", "0.13.1", Same},
		{"v0.13.1-0-gabc", "0.13.1", Same},      // describe of an exact tag
		{"v0.13.1-9-gabc", "0.13.1", Downgrade}, // the incident
		{"v0.13.1", "0.13.1-9-gabc", Upgrade},   // and its mirror
		{"v0.13.1-9-gabc", "0.14.0", Upgrade},   // base beats ahead
		{"v0.13.2-snapshot-abc", "0.13.1", Downgrade},
		{"v0.13.1-dev", "0.13.1", Downgrade}, // a build past the tag
		{"dev", "0.13.1", Unknown},
		{"v0.13.1", "", Unknown},
	}
	for _, c := range cases {
		if got := Relate(Parse(c.have), Parse(c.want)); got != c.rel {
			t.Errorf("Relate(%q, %q) = %q, want %q", c.have, c.want, got, c.rel)
		}
	}
}

// TestEveryProducerParses covers criterion 4. Each string is the shape one of
// the five build paths emits. A producer whose stamp does not parse is a
// permanent hole in the guard, which is exactly what `make build` was.
func TestEveryProducerParses(t *testing.T) {
	for name, stamp := range map[string]string{
		"goreleaser release":  "0.13.1",
		"goreleaser snapshot": "0.13.2-snapshot-708d412",
		"install.sh build":    "0.13.1-dev",
		"git describe":        "v0.13.1-9-g9cab728",
		"canonical":           Canonical(0, 13, 1, 9, "9cab728"),
	} {
		if !Parse(stamp).Known {
			t.Errorf("%s stamp %q does not parse", name, stamp)
		}
	}
}

// TestUnknownIsNeverResolved: the guard's whole job is to stop rather than
// guess. `make build` emits a bare "dev" with no version in it at all.
func TestUnknownIsNeverResolved(t *testing.T) {
	unknown := Parse("dev (none, unknown)")
	if unknown.Known {
		t.Fatal(`"dev" must not parse as a version`)
	}
	if _, ok := Compare(unknown, Parse("0.13.1")); ok {
		t.Error("an unknown stamp reported itself comparable")
	}
	if got := Relate(unknown, Parse("0.13.1")); got != Unknown {
		t.Errorf("Relate = %q, want %q", got, Unknown)
	}
}

func TestCanonicalRoundTrips(t *testing.T) {
	s := Parse(Canonical(1, 2, 3, 4, "deadbee"))
	if !s.Known || s.Major != 1 || s.Minor != 2 || s.Patch != 3 || s.Ahead != 4 || s.Commit != "deadbee" {
		t.Errorf("round trip lost information: %+v", s)
	}
}

// TestIsRelease pins which shapes count as "a published release, exactly".
// The old code answered this with a substring test for "-dev" or "-snapshot",
// which put a git-describe stamp and a bare "dev" on the release side — the
// first got downgraded, the second can never be judged at all.
func TestIsRelease(t *testing.T) {
	for stamp, want := range map[string]bool{
		"0.13.1":              true,  // goreleaser release
		"v0.13.1":             true,  // same, with the v
		"v0.13.1-0-gabc":      true,  // describe of an exact tag
		"v0.13.1-9-gabc":      false, // nine commits past it
		"0.13.1-dev":          false, // install.sh's build
		"0.13.2-snapshot-abc": false, // goreleaser snapshot
		"dev":                 false, // unstamped: not a release, not anything
	} {
		if got := Parse(stamp).IsRelease(); got != want {
			t.Errorf("Parse(%q).IsRelease() = %v, want %v", stamp, got, want)
		}
	}
}
