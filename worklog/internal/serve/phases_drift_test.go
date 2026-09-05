package serve

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

// The phase vocabulary is spelled out in four places that no compiler can
// reconcile: Go (store.Phases, canonical), two browser copies that cannot
// import from Go because the front-end has no build step, and prose in
// devboard/schema.md. They agree today by diligence alone.
//
// The failure they guard against is quiet rather than loud: add a phase to
// store.Phases and forget phases.js, and the CLI accepts it while every task
// sitting on it renders as "unknown phase". Nothing breaks, nothing errors,
// the board is just wrong.
//
// So this reads the other three as text and holds them against the canonical
// list. It is the same shape as TestVendorChecksums — a file on disk checked
// against embedded truth.

// jsList pulls a flat JS string-array literal out of source text.
func jsList(t *testing.T, src, decl string) []string {
	t.Helper()
	re := regexp.MustCompile(regexp.QuoteMeta(decl) + `\s*=\s*\[([^\]]*)\]`)
	m := re.FindStringSubmatch(src)
	if m == nil {
		t.Fatalf("could not find %q — the declaration was renamed, so this guard is no longer watching anything", decl)
	}
	var out []string
	for _, raw := range strings.Split(m[1], ",") {
		if s := strings.Trim(strings.TrimSpace(raw), `"'`); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPhaseVocabularyAgreesAcrossCopies(t *testing.T) {
	want := store.Phases

	for _, c := range []struct{ name, path, decl string }{
		{"the Lens Board's copy", "static/assets/src/phases.js", "export const PHASES"},
		{"the outgoing board's copy", "static/index.html", "const PHASES"},
	} {
		got := jsList(t, read(t, c.path), c.decl)
		if !slicesEqual(got, want) {
			t.Errorf("%s (%s) has drifted from store.Phases:\n  got  %v\n  want %v", c.name, c.path, got, want)
		}
	}

	// schema.md documents the vocabulary as prose, pipe-separated, wrapped
	// across comment lines. Readers trust it; drift here misleads a human
	// rather than the code.
	doc := read(t, "../../../devboard/schema.md")
	for _, p := range want {
		if !strings.Contains(doc, p+"|") && !strings.Contains(doc, "|"+p) {
			t.Errorf("devboard/schema.md does not document the %q phase", p)
		}
	}
}

// The spike short track must be a subset of the full one, or a spike's phase
// sorts and validates as unknown everywhere outside the renderer that special
// cases it — the reason index.html says so in a comment.
func TestSpikeTrackIsASubsetOfTheFullTrack(t *testing.T) {
	full := map[string]bool{}
	for _, p := range store.Phases {
		full[p] = true
	}
	for _, c := range []struct{ path, decl string }{
		{"static/assets/src/phases.js", "export const SPIKE_PHASES"},
		{"static/index.html", "const SPIKE_PHASES"},
	} {
		for _, p := range jsList(t, read(t, c.path), c.decl) {
			if !full[p] {
				t.Errorf("%s: spike phase %q is not in store.Phases", c.path, p)
			}
		}
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
