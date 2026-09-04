package projection

import (
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/store/memstore"
)

// TestFeedbackAlwaysRendered pins FEEDBACK.md as an unconditional surface.
//
// Render used to emit it only when the store held at least one entry, which
// made the file invisible to BOTH EditedIn and RenderTo in the zero-entry
// case — so a hand-edited or merely-unread friction log was neither
// protected from a re-render nor rewritten by one, right up until the first
// entry arrived and replaced it wholesale.
func TestFeedbackAlwaysRendered(t *testing.T) {
	files, err := Render(memstore.New())
	if err != nil {
		t.Fatal(err)
	}
	got, ok := files["FEEDBACK.md"]
	if !ok {
		t.Fatal("Render omitted FEEDBACK.md for a store with no entries; EditedIn cannot protect a file it never renders")
	}
	if len(got) == 0 {
		t.Error("FEEDBACK.md rendered empty; want at least its heading")
	}
}
