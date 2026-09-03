package cli

import (
	"time"

	"github.com/prestontallen/ai-devboard/worklog/internal/feedback"
	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

// runStoreFeedbackAppend is feedback.Append's store-backed twin
// (adb-cutover M3d). FEEDBACK.md isn't ticket-scoped, so there's no
// ticket to fetch — just a new store.FeedbackEntry, PutFeedback (which
// mints the ID), and a render.
func runStoreFeedbackAppend(wd model.Workdir, e feedback.Entry) (feedback.Entry, error) {
	ss, err := openStoreForWrite(wd)
	if err != nil {
		return feedback.Entry{}, err
	}
	defer ss.close()

	entry := &store.FeedbackEntry{
		Seconds: e.Timestamp,
		Signal:  string(e.Signal),
		Trigger: e.Trigger,
		Excerpt: e.Excerpt,
		Context: e.Context,
	}
	if entry.Seconds == 0 {
		entry.Seconds = time.Now().Unix()
	}
	if err := ss.commitFeedback(entry); err != nil {
		return feedback.Entry{}, err
	}

	return feedback.Entry{
		Timestamp: entry.Seconds,
		Signal:    feedback.Signal(entry.Signal),
		Trigger:   entry.Trigger,
		Excerpt:   entry.Excerpt,
		Context:   entry.Context,
	}, nil
}

// runStoreFeedbackResolve is feedback.Resolve's store-backed twin. An
// already-resolved entry is a no-op, matching legacy exactly.
func runStoreFeedbackResolve(wd model.Workdir, ts int64) (resolved int64, already bool, err error) {
	ss, sErr := openStoreForWrite(wd)
	if sErr != nil {
		return 0, false, sErr
	}
	defer ss.close()

	entries, fErr := ss.s.Feedback()
	if fErr != nil {
		return 0, false, fErr
	}
	var found *store.FeedbackEntry
	for _, e := range entries {
		if e.Seconds == ts {
			found = e
			break
		}
	}
	if found == nil {
		return 0, false, feedback.ErrEntryNotFound
	}
	if found.Resolved != 0 {
		return found.Resolved, true, nil
	}

	found.Resolved = time.Now().Unix()
	if err := ss.commitFeedback(found); err != nil {
		return 0, false, err
	}
	return found.Resolved, false, nil
}
