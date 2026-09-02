package cli

import (
	"github.com/prestontallen/ai-devboard/worklog/internal/devboard"
)

// syncDevboard runs a devboard side effect and warns on failure. Devboard
// mirroring must never fail the primary worklog operation: the ticket
// change has already landed by the time these run.
func syncDevboard(fn func() error) {
	if err := fn(); err != nil {
		logger.Warn("devboard sync failed", "err", err)
	}
}

func devboardOnStart(id, title string) {
	syncDevboard(func() error { return devboard.OnStart(id, title) })
}
func devboardOnDone(id string)    { syncDevboard(func() error { return devboard.OnDone(id) }) }
func devboardOnPR(id, url string) { syncDevboard(func() error { return devboard.OnPR(id, url) }) }
