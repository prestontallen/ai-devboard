// Package validate runs the worklog's structural invariants over a parsed
// WORK.md plus any cross-file references (INDEX.md, notes/<id>.md).
package validate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/prestontallen/day2day/internal/model"
	"github.com/prestontallen/day2day/internal/parse"
	"github.com/prestontallen/day2day/internal/reindex"
)

// CheckID is a stable identifier for each rule. CLI output prints
// `VIOLATION [check-id] message` using these.
type CheckID string

const (
	CheckWorkMDExists          CheckID = "work-md-exists"
	CheckNowCap                CheckID = "now-cap"
	CheckNoTopLevelX           CheckID = "no-top-level-x"
	CheckStartedOnActive       CheckID = "started-on-active"
	CheckIndexRefsExist        CheckID = "index-refs-exist"
	CheckNotesFileExists       CheckID = "notes-file-exists"
	CheckThreePlaceConsistency CheckID = "three-place-consistency"
)

const NowCap = 5

// Violation is a single rule failure.
type Violation struct {
	Check   CheckID
	Message string
}

func (v Violation) String() string {
	return fmt.Sprintf("VIOLATION [%s] %s", v.Check, v.Message)
}

// Result is what Run returns. WorkMDMissing being true means no checks beyond
// CheckWorkMDExists were attempted.
type Result struct {
	Violations    []Violation
	Infos         []string
	WorkMDMissing bool
}

// HasViolations reports whether the result contains any violations.
func (r *Result) HasViolations() bool { return len(r.Violations) > 0 }

// Run executes every check against the worklog data dir.
func Run(wd model.Workdir) (*Result, error) {
	res := &Result{}

	doc, err := parse.File(wd.WorkMD())
	if err != nil {
		if errors.Is(err, model.ErrWorkMDMissing) {
			res.WorkMDMissing = true
			res.Violations = append(res.Violations, Violation{
				Check:   CheckWorkMDExists,
				Message: fmt.Sprintf("WORK.md not found at %s", wd.WorkMD()),
			})
			return res, nil
		}
		return nil, err
	}

	checkNowCap(res, doc)
	checkNoTopLevelX(res, doc)
	checkStartedOnActive(res, doc)
	checkNotesFileExists(res, wd, doc)
	checkThreePlaceConsistency(res, wd, doc)

	if err := checkIndexRefsExist(res, wd); err != nil {
		return nil, err
	}
	return res, nil
}

// --- individual checks -------------------------------------------------------

func checkNowCap(res *Result, doc *model.WorkDoc) {
	now := doc.Section(model.SectionNow)
	if now == nil {
		return
	}
	if n := len(now.Blocks); n > NowCap {
		res.Violations = append(res.Violations, Violation{
			Check:   CheckNowCap,
			Message: fmt.Sprintf("## Now has %d tickets, cap is %d", n, NowCap),
		})
	}
}

func checkNoTopLevelX(res *Result, doc *model.WorkDoc) {
	for _, sec := range doc.Sections {
		for _, b := range sec.Blocks {
			if b.IsDone() {
				res.Violations = append(res.Violations, Violation{
					Check: CheckNoTopLevelX,
					Message: fmt.Sprintf(
						"%s:%d: %s%s has top-level [x] (transient state; should be archived)",
						doc.Path, b.StartLine, b.ID, idTitle(b)),
				})
			}
		}
	}
}

func checkStartedOnActive(res *Result, doc *model.WorkDoc) {
	now := doc.Section(model.SectionNow)
	if now == nil {
		return
	}
	for _, b := range now.Blocks {
		if b.IsActive() && b.Started == "" {
			res.Violations = append(res.Violations, Violation{
				Check: CheckStartedOnActive,
				Message: fmt.Sprintf(
					"%s:%d: %s is [~] in ## Now but has no **Started**: line",
					doc.Path, b.StartLine, b.ID),
			})
		}
	}
}

func checkNotesFileExists(res *Result, wd model.Workdir, doc *model.WorkDoc) {
	for _, sec := range doc.Sections {
		for _, b := range sec.Blocks {
			if b.NotesRef == "" {
				continue
			}
			if !strings.HasPrefix(b.NotesRef, "notes/") {
				continue
			}
			target := filepath.Join(wd.Root, b.NotesRef)
			if _, err := os.Stat(target); err != nil {
				res.Violations = append(res.Violations, Violation{
					Check: CheckNotesFileExists,
					Message: fmt.Sprintf(
						"%s:%d: block %s references %s but file does not exist",
						doc.Path, b.StartLine, b.ID, b.NotesRef),
				})
			}
		}
	}
}

func checkThreePlaceConsistency(res *Result, wd model.Workdir, doc *model.WorkDoc) {
	now := doc.Section(model.SectionNow)
	next := doc.Section(model.SectionNext)
	if now == nil {
		return
	}

	// Build a lookup of epics in Next: id -> *Block
	epics := map[string]*model.Block{}
	if next != nil {
		for i := range next.Blocks {
			b := &next.Blocks[i]
			if b.IsEpic() && b.ID != "" {
				epics[b.ID] = b
			}
		}
	}

	for _, child := range now.Blocks {
		if child.Parent == "" {
			continue
		}
		epic, ok := epics[child.Parent]
		if !ok {
			cause := "no matching epic block found in ## Next"
			if hint := reindex.ArchivedHint(wd.ArchiveDir(), child.Parent); hint != "" {
				cause = "parent epic was " + hint + " — an archived epic cannot have active children"
			}
			res.Violations = append(res.Violations, Violation{
				Check: CheckThreePlaceConsistency,
				Message: fmt.Sprintf(
					"%s:%d: child %s has Parent:%s but %s",
					doc.Path, child.StartLine, child.ID, child.Parent, cause),
			})
			continue
		}

		if !containsFold(epic.ActiveChildren, child.ID) {
			res.Violations = append(res.Violations, Violation{
				Check: CheckThreePlaceConsistency,
				Message: fmt.Sprintf(
					"%s:%d: child %s not listed in epic %s **Active children**: %v",
					doc.Path, child.StartLine, child.ID, epic.ID, epic.ActiveChildren),
			})
		}

		notesPath := wd.NotesFile(child.Parent)
		matched, err := notesHasOpenChild(notesPath, child.ID)
		switch {
		case errors.Is(err, os.ErrNotExist):
			res.Violations = append(res.Violations, Violation{
				Check: CheckThreePlaceConsistency,
				Message: fmt.Sprintf(
					"%s missing (referenced as parent of %s)",
					notesPath, child.ID),
			})
		case err != nil:
			res.Violations = append(res.Violations, Violation{
				Check: CheckThreePlaceConsistency,
				Message: fmt.Sprintf("%s: read failed: %v", notesPath, err),
			})
		case !matched:
			res.Violations = append(res.Violations, Violation{
				Check: CheckThreePlaceConsistency,
				Message: fmt.Sprintf(
					"%s has no `- [ ]` line mentioning %s", notesPath, child.ID),
			})
		}
	}
}

var notesChildRe = regexp.MustCompile(`(?i)^- \[ \].*`) // any open checkbox; we then check for id substring

func notesHasOpenChild(path, childID string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	low := strings.ToLower(childID)
	for _, line := range strings.Split(string(data), "\n") {
		if !notesChildRe.MatchString(line) {
			continue
		}
		if strings.Contains(strings.ToLower(line), low) {
			return true, nil
		}
	}
	return false, nil
}

var indexRefRe = regexp.MustCompile(`(archive/[0-9]{4}-[0-9]{2}\.md|notes/[a-zA-Z0-9_-]+\.md)`)

func checkIndexRefsExist(res *Result, wd model.Workdir) error {
	data, err := os.ReadFile(wd.IndexMD())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			res.Infos = append(res.Infos,
				fmt.Sprintf("INDEX.md not present at %s; skipping index-refs-exist", wd.IndexMD()))
			return nil
		}
		return err
	}
	seen := map[string]bool{}
	for _, m := range indexRefRe.FindAllString(string(data), -1) {
		if seen[m] {
			continue
		}
		seen[m] = true
		target := filepath.Join(wd.Root, m)
		if _, err := os.Stat(target); err != nil {
			res.Violations = append(res.Violations, Violation{
				Check:   CheckIndexRefsExist,
				Message: fmt.Sprintf("INDEX.md references %s but file does not exist", m),
			})
		}
	}
	return nil
}

// --- helpers -----------------------------------------------------------------

func containsFold(haystack []string, needle string) bool {
	for _, h := range haystack {
		if strings.EqualFold(h, needle) {
			return true
		}
	}
	return false
}

func idTitle(b model.Block) string {
	if b.Title != "" {
		return " — " + b.Title
	}
	return ""
}

// SortedViolations returns violations sorted by check ID then message, for
// stable test assertions.
func SortedViolations(vs []Violation) []Violation {
	out := append([]Violation(nil), vs...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Check != out[j].Check {
			return out[i].Check < out[j].Check
		}
		return out[i].Message < out[j].Message
	})
	return out
}
