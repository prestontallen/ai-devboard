package cli

import (
	"fmt"
	"strings"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
	"github.com/prestontallen/ai-devboard/worklog/internal/wait"
)

// runStoreWait is wait.Wait's store-backed twin (adb-cutover M3d): move a
// Now ticket into Waiting. Cap-exempt in both directions, same as legacy
// — only the Now-bound direction (start's resume fast path) cap-checks.
func runStoreWait(wd model.Workdir, id, today string) (wait.WaitOutput, error) {
	normID := strings.ToLower(strings.TrimSpace(id))

	ss, err := openStoreForWrite(wd)
	if err != nil {
		return wait.WaitOutput{}, err
	}
	defer ss.close()

	t, err := ss.ticketBySlugOrErr(normID, wait.ErrIDNotFound)
	if err != nil {
		return wait.WaitOutput{}, err
	}
	if t.Section == store.SectionWaiting {
		return wait.WaitOutput{}, fmt.Errorf("%w: %q", wait.ErrAlreadyWaiting, normID)
	}
	if t.Section != store.SectionNow {
		return wait.WaitOutput{}, fmt.Errorf("%w: %q is in ## %s", wait.ErrNotInNow, normID, t.Section)
	}

	t.Section = store.SectionWaiting
	t.WaitingSince = today
	if err := ss.commit(t); err != nil {
		return wait.WaitOutput{}, err
	}

	return wait.WaitOutput{
		Status:       "waiting",
		ID:           t.Slug,
		WaitingSince: today,
		WorkMD:       wd.WorkMD(),
	}, nil
}
