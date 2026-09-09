// Package version parses and orders this project's build stamps.
//
// It exists because there was no comparison at all, and the absence cost a
// binary: `./install.sh` replaced a build nine commits ahead of v0.13.1 with
// the v0.13.1 release, because "different from the latest tag" and "older
// than the latest tag" were the same state.
//
// The subtlety that makes this a package rather than a strings.Compare: a
// local build CANNOT be ordered by semver alone. `git describe` renders a
// build nine commits past v0.13.1 as "v0.13.1-9-g9cab728", and under semver
// everything after the hyphen is a PRE-release — so that string sorts BELOW
// plain v0.13.1, and a nine-commits-newer binary reads as older than the tag
// it contains. Every local build shape in this project has that property.
//
// So the ordering key is a pair: the base tag, and how many commits are past
// it. `git describe --long` already produces exactly that pair, which is why
// its shape is the canonical one here.
package version

import (
	"fmt"
	"strconv"
	"strings"
)

// Stamp is a parsed build stamp. Known is false when the string carries no
// orderable information — `make build`'s bare "dev", say — and an unknown
// stamp is never silently resolved in either direction.
type Stamp struct {
	Raw    string
	Major  int
	Minor  int
	Patch  int
	Ahead  int    // commits past the base tag; 0 means the tag itself
	Commit string // may be empty
	Known  bool
}

// Canonical renders a stamp in the shape every producer should emit:
// v<major>.<minor>.<patch>-<ahead>-g<commit>, which is `git describe --long`.
func Canonical(major, minor, patch, ahead int, commit string) string {
	if commit == "" {
		commit = "unknown"
	}
	return fmt.Sprintf("v%d.%d.%d-%d-g%s", major, minor, patch, ahead, commit)
}

// Parse reads a build stamp. It accepts the canonical describe shape, a bare
// release tag with or without a leading v (what goreleaser emits today, and
// what every already-installed binary carries), and the snapshot shape. Any
// stamp it cannot place comes back with Known false rather than a guess.
func Parse(s string) Stamp {
	st := Stamp{Raw: s}
	s = strings.TrimSpace(s)
	// A display string may carry the rest of the stamp: "v0.1.0 (abc, date)".
	if i := strings.IndexByte(s, ' '); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return st
	}
	s = strings.TrimPrefix(s, "v")

	// Split the base from whatever follows the version triple.
	base, rest := s, ""
	if i := strings.IndexByte(s, '-'); i >= 0 {
		base, rest = s[:i], s[i+1:]
	}
	major, minor, patch, ok := triple(base)
	if !ok {
		return st // "dev", or anything else without a version triple
	}
	st.Major, st.Minor, st.Patch, st.Known = major, minor, patch, true

	if rest == "" {
		return st // a plain release tag: zero commits past itself
	}

	// Canonical/describe: "<ahead>-g<sha>". This is the only suffix that
	// carries ordering information; everything else (a -dev or -snapshot
	// marker) means "some build past this tag" and is treated as one commit
	// ahead, which is enough to rank it above the release and below the next.
	head, tail := rest, ""
	if i := strings.IndexByte(rest, '-'); i >= 0 {
		head, tail = rest[:i], rest[i+1:]
	}
	if n, err := strconv.Atoi(head); err == nil && n >= 0 {
		st.Ahead = n
		st.Commit = strings.TrimPrefix(tail, "g")
		return st
	}
	st.Ahead = 1
	st.Commit = tail
	return st
}

// Compare orders two stamps. The bool reports whether they are comparable at
// all; when it is false the caller must stop rather than pick a direction.
//
// Ordering is base triple first, then commits ahead. That second term is the
// whole point: a build past a tag CONTAINS that tag, so it is newer than the
// release, not a pre-release of it.
func Compare(a, b Stamp) (int, bool) {
	if !a.Known || !b.Known {
		return 0, false
	}
	for _, p := range [][2]int{
		{a.Major, b.Major}, {a.Minor, b.Minor}, {a.Patch, b.Patch}, {a.Ahead, b.Ahead},
	} {
		if p[0] != p[1] {
			if p[0] < p[1] {
				return -1, true
			}
			return 1, true
		}
	}
	return 0, true
}

// Relation names a comparison for output. "drift" is deliberately not one of
// these: it does not distinguish an upgrade from a downgrade, which is how a
// downgrade got waved through as routine.
type Relation string

const (
	Same      Relation = "same"
	Upgrade   Relation = "upgrade"
	Downgrade Relation = "downgrade"
	Unknown   Relation = "unknown"
)

// Relate reports how moving from have to want would go.
func Relate(have, want Stamp) Relation {
	c, ok := Compare(have, want)
	switch {
	case !ok:
		return Unknown
	case c < 0:
		return Upgrade
	case c > 0:
		return Downgrade
	default:
		return Same
	}
}

// triple parses "1.2.3". Anything else is not a version.
func triple(s string) (major, minor, patch int, ok bool) {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	out := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return 0, 0, 0, false
		}
		out[i] = n
	}
	return out[0], out[1], out[2], true
}

// IsRelease reports whether this stamp names a published release exactly,
// rather than a build sitting some number of commits past one.
//
// The distinction decides whether a nearby checkout is allowed to rebuild the
// binary out from under the user: a release someone downloaded should not be,
// a local build from that checkout should. An unknown stamp is not a release,
// because claiming otherwise is the guess this package exists to refuse.
func (s Stamp) IsRelease() bool { return s.Known && s.Ahead == 0 }
