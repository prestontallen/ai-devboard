package convert

import "testing"

// TestRerunFeedbackNotDuplicated: converting the same corpus (unchanged
// FEEDBACK.md) twice into one store must not duplicate friction entries —
// copy-forward's whole premise is that re-converting into the SAME store
// is safe to do repeatedly (adb-worklog2-migrate).
func TestRerunFeedbackNotDuplicated(t *testing.T) {
	for name, s := range stores(t) {
		t.Run(name, func(t *testing.T) {
			rep1, err := Load(s, corpus())
			if err != nil {
				t.Fatal(err)
			}
			first, err := s.Feedback()
			if err != nil {
				t.Fatal(err)
			}
			if len(first) != rep1.Feedback {
				t.Fatalf("first run: %d feedback rows, report says %d", len(first), rep1.Feedback)
			}

			rep2, err := Load(s, corpus())
			if err != nil {
				t.Fatal(err)
			}
			second, err := s.Feedback()
			if err != nil {
				t.Fatal(err)
			}
			if len(second) != len(first) {
				t.Errorf("feedback rows duplicated across re-run: %d -> %d", len(first), len(second))
			}
			if rep2.Feedback != rep1.Feedback {
				t.Errorf("report feedback count changed on unchanged input: %d -> %d", rep1.Feedback, rep2.Feedback)
			}
			for i := range first {
				if first[i].ID != second[i].ID {
					t.Errorf("feedback[%d] id changed across re-run: %s -> %s", i, first[i].ID, second[i].ID)
				}
			}
		})
	}
}
