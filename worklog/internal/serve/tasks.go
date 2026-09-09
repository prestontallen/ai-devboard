package serve

import (
	"strings"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
)

const archiveDir = "_archive"

var taskExts = []string{".yaml", ".yml", ".json"}

func hasTaskExt(name string) bool {
	lower := strings.ToLower(name)
	for _, ext := range taskExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// feedbackView is the frontend's feedback entry shape. It marshals
// independently of feedback.Entry so `resolved` is always present
// (Entry's omitempty would drop resolved:0, which the old server emitted).
type feedbackView struct {
	Timestamp int64  `json:"timestamp"`
	Signal    string `json:"signal"`
	Trigger   string `json:"trigger"`
	Excerpt   string `json:"excerpt"`
	Context   string `json:"context"`
	Resolved  int64  `json:"resolved"`
}

// backlogSection is one WORK.md section as the backlog lens draws it.
type backlogSection struct {
	Name  string         `json:"name"`
	Items []*model.Block `json:"items"`
}

// backlogSections are the two the lens shows, in bar order. `Now` is
// deliberately absent: started work already reaches the board as task
// files, and listing it twice would double it against the Board chip.
// `Waiting` is absent for the same reason — it has its own lens.
var backlogSections = []model.SectionName{model.SectionNext, model.SectionSomeday}
