package memstore

import (
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/store"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/storetest"
)

func TestConformance(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Store { return New() })
}
