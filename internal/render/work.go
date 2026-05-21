// Package render produces the on-disk form of WORK.md from a parsed model
// via line-level splices, preserving every untouched line byte-for-byte.
package render

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/prestontallen/day2day/internal/model"
)

// ErrBlockNotFound is returned by helpers that look up a block by ID.
var ErrBlockNotFound = fmt.Errorf("render: block not found")

// ErrBlockNotEpic is returned by epic-specific helpers when the block resolves
// to a non-epic.
var ErrBlockNotEpic = fmt.Errorf("render: block is not an epic")

// ErrActiveChildrenMissing is returned by UpdateEpicActiveChildren when the
// epic block has no `**Active children**:` line.
var ErrActiveChildrenMissing = fmt.Errorf("render: epic is missing **Active children**: line")

// RemoveBlock returns a new line slice with the named block excised, plus a
// copy of the parsed Block so callers can reuse its metadata. The removed
// range is the block's inclusive [StartLine, EndLine] window in 1-indexed
// terms (which matches what the parser records). Surrounding lines are
// preserved byte-for-byte.
func RemoveBlock(doc *model.WorkDoc, blockID string) ([]string, *model.Block, error) {
	b := doc.BlockByID(blockID)
	if b == nil {
		return nil, nil, fmt.Errorf("%w: %q", ErrBlockNotFound, blockID)
	}
	// 1-indexed [StartLine, EndLine] → 0-indexed [StartLine-1, EndLine)
	start := b.StartLine - 1
	end := b.EndLine // exclusive in 0-indexed terms

	out := make([]string, 0, len(doc.Lines)-(end-start))
	out = append(out, doc.Lines[:start]...)
	out = append(out, doc.Lines[end:]...)
	// Return a copy of the block so the caller can rely on it independent
	// of any further parsing.
	copyBlock := *b
	return out, &copyBlock, nil
}

var activeChildrenLineRe = regexp.MustCompile(`^(  - \*\*Active children\*\*:\s*)(.*)$`)

var (
	prLineRe           = regexp.MustCompile(`^  - \*\*PR\*\*:(\s.*)?$`)
	tagsLineRe         = regexp.MustCompile(`^  - \*\*Tags\*\*:`)
	startedLineRe      = regexp.MustCompile(`^  - \*\*Started\*\*:`)
	notesLineRe        = regexp.MustCompile(`^  - \*\*Notes\*\*:(\s.*)?$`)
	waitingSinceLineRe = regexp.MustCompile(`^  - \*\*Waiting since\*\*:(\s.*)?$`)
)

// SetBlockPR rewrites (or inserts) the `**PR**:` line for the block identified
// by blockID. The line is always rendered with a trailing space — even when
// value is empty — to keep the field visibly available on disk.
//
// Insertion point when the line is absent: right after the `**Tags**:` line,
// otherwise right before the `**Started**:` line, otherwise at the end of the
// block's metadata range.
func SetBlockPR(doc *model.WorkDoc, blockID, value string) ([]string, error) {
	b := doc.BlockByID(blockID)
	if b == nil {
		return nil, fmt.Errorf("%w: %q", ErrBlockNotFound, blockID)
	}

	out := make([]string, len(doc.Lines))
	copy(out, doc.Lines)

	newLine := "  - **PR**: " + value

	// 0-indexed metadata range: [StartLine, EndLine-1] inclusive. The bullet
	// line itself is at StartLine-1 (1-indexed) → out[StartLine-1] in 0-indexed.
	// Metadata begins at out[StartLine].
	metaStart := b.StartLine // 0-indexed first metadata line
	metaEnd := b.EndLine     // 0-indexed exclusive end

	// 1) Existing PR line → rewrite in place.
	for i := metaStart; i < metaEnd && i < len(out); i++ {
		if prLineRe.MatchString(out[i]) {
			out[i] = newLine
			return out, nil
		}
	}

	// 2) Insert after the last Tags line, else before Started, else at end.
	insertAt := -1
	for i := metaStart; i < metaEnd && i < len(out); i++ {
		if tagsLineRe.MatchString(out[i]) {
			insertAt = i + 1
		}
	}
	if insertAt < 0 {
		for i := metaStart; i < metaEnd && i < len(out); i++ {
			if startedLineRe.MatchString(out[i]) {
				insertAt = i
				break
			}
		}
	}
	if insertAt < 0 {
		insertAt = metaEnd
	}

	res := make([]string, 0, len(out)+1)
	res = append(res, out[:insertAt]...)
	res = append(res, newLine)
	res = append(res, out[insertAt:]...)
	return res, nil
}

// UpdateEpicActiveChildren finds the epic identified by epicID and appends
// addChildID to its `**Active children**:` field. If the field reads
// `<none>` it is replaced with the new id; otherwise the id is comma-
// appended. The operation is idempotent: if addChildID already appears
// in the list, the document is returned unchanged.
func UpdateEpicActiveChildren(doc *model.WorkDoc, epicID, addChildID string) ([]string, error) {
	epic := doc.BlockByID(epicID)
	if epic == nil {
		return nil, fmt.Errorf("%w: %q", ErrBlockNotFound, epicID)
	}
	if !epic.IsEpic() {
		return nil, fmt.Errorf("%w: %q", ErrBlockNotEpic, epicID)
	}

	// Search the epic's metadata window for the Active children line.
	// 0-indexed range: [StartLine, EndLine-1] inclusive, skipping the
	// bullet line at index StartLine-1.
	out := make([]string, len(doc.Lines))
	copy(out, doc.Lines)

	idx := -1
	for i := epic.StartLine; i <= epic.EndLine-1; i++ {
		if activeChildrenLineRe.MatchString(doc.Lines[i]) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, fmt.Errorf("%w: %q", ErrActiveChildrenMissing, epicID)
	}

	m := activeChildrenLineRe.FindStringSubmatch(doc.Lines[idx])
	prefix, current := m[1], strings.TrimSpace(m[2])

	var children []string
	if current != "" && !strings.EqualFold(current, "<none>") {
		for _, c := range strings.Split(current, ",") {
			c = strings.TrimSpace(c)
			if c != "" {
				children = append(children, c)
			}
		}
	}
	// Idempotency check (case-insensitive).
	low := strings.ToLower(addChildID)
	for _, c := range children {
		if strings.EqualFold(c, low) {
			return out, nil // already present; no-op
		}
	}
	children = append(children, low)
	out[idx] = prefix + strings.Join(children, ", ")
	return out, nil
}

// RemoveFromEpicActiveChildren removes removeChildID from the epic's
// `**Active children**:` field. If the resulting list is empty, the field is
// reset to `<none>`. Idempotent: if the child isn't present, the document is
// returned unchanged. Match is case-insensitive on child IDs.
func RemoveFromEpicActiveChildren(doc *model.WorkDoc, epicID, removeChildID string) ([]string, error) {
	epic := doc.BlockByID(epicID)
	if epic == nil {
		return nil, fmt.Errorf("%w: %q", ErrBlockNotFound, epicID)
	}
	if !epic.IsEpic() {
		return nil, fmt.Errorf("%w: %q", ErrBlockNotEpic, epicID)
	}

	out := make([]string, len(doc.Lines))
	copy(out, doc.Lines)

	idx := -1
	for i := epic.StartLine; i <= epic.EndLine-1; i++ {
		if activeChildrenLineRe.MatchString(doc.Lines[i]) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, fmt.Errorf("%w: %q", ErrActiveChildrenMissing, epicID)
	}

	m := activeChildrenLineRe.FindStringSubmatch(doc.Lines[idx])
	prefix, current := m[1], strings.TrimSpace(m[2])

	var children []string
	if current != "" && !strings.EqualFold(current, "<none>") {
		for _, c := range strings.Split(current, ",") {
			c = strings.TrimSpace(c)
			if c == "" {
				continue
			}
			if strings.EqualFold(c, removeChildID) {
				continue
			}
			children = append(children, c)
		}
	}

	if len(children) == 0 {
		out[idx] = prefix + "<none>"
	} else {
		out[idx] = prefix + strings.Join(children, ", ")
	}
	return out, nil
}

// childCheckboxRe matches a notes-file checkbox child line. Captures the
// state char (m[2]) and the child ID (m[4]).
var childCheckboxRe = regexp.MustCompile(`^(- \[)([ ~x])(\] *)([a-zA-Z0-9_-]+)(.*)$`)

// FlipChildCheckbox finds the `- [ ]` (or `- [~]`) line whose first token
// matches childID and rewrites it to `- [x]`. Operates on raw bytes because
// notes files aren't WorkDocs — they're freeform markdown with a checkbox
// list. Idempotent: if the line already has `[x]`, returns unchanged content
// with found=true.
func FlipChildCheckbox(notesBytes []byte, childID string) ([]byte, bool, error) {
	lines := strings.Split(string(notesBytes), "\n")
	for i, line := range lines {
		m := childCheckboxRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if !strings.EqualFold(m[4], childID) {
			continue
		}
		if m[2] == "x" {
			return notesBytes, true, nil
		}
		lines[i] = m[1] + "x" + m[3] + m[4] + m[5]
		return []byte(strings.Join(lines, "\n")), true, nil
	}
	return notesBytes, false, nil
}

// dayHeaderRe matches a `## YYYY-MM-DD` line in an archive file.
var dayHeaderRe = regexp.MustCompile(`^## \d{4}-\d{2}-\d{2}\s*$`)

// AppendToArchive inserts entryLines into the month archive at the right
// position to keep newest-day-at-top + newest-entry-within-day-at-top
// ordering. Creates the file with a `# Archive — <month>` header if it
// doesn't exist. Atomic via WriteAtomic.
//
//   - today is the YYYY-MM-DD date string for the entry (used to find/create
//     the day section).
//   - month is the YYYY-MM string used in the file header on creation.
//   - entryLines should be the output of FormatArchiveEntry — the entry's
//     own lines, no leading or trailing blank.
func AppendToArchive(path string, entryLines []string, today, month string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("render: read archive %s: %w", path, err)
		}
		// New file.
		out := []string{
			"# Archive — " + month,
			"",
			"## " + today,
			"",
		}
		out = append(out, entryLines...)
		return WriteAtomic(path, out)
	}

	s := strings.TrimSuffix(string(data), "\n")
	var lines []string
	if s != "" {
		lines = strings.Split(s, "\n")
	}

	todayHeader := "## " + today
	firstDayIdx := -1
	for i, l := range lines {
		if dayHeaderRe.MatchString(l) {
			firstDayIdx = i
			break
		}
	}

	if firstDayIdx >= 0 && strings.TrimSpace(lines[firstDayIdx]) == todayHeader {
		// Existing today section — insert entry at top of today.
		insertIdx := firstDayIdx + 1
		for insertIdx < len(lines) && lines[insertIdx] == "" {
			insertIdx++
		}
		spliceContent := append([]string{}, entryLines...)
		spliceContent = append(spliceContent, "")
		out := make([]string, 0, len(lines)+len(spliceContent))
		out = append(out, lines[:insertIdx]...)
		out = append(out, spliceContent...)
		out = append(out, lines[insertIdx:]...)
		return WriteAtomic(path, out)
	}

	// New day section (or no day headers at all).
	spliceContent := []string{todayHeader, ""}
	spliceContent = append(spliceContent, entryLines...)
	spliceContent = append(spliceContent, "")

	var insertIdx int
	if firstDayIdx >= 0 {
		insertIdx = firstDayIdx
	} else {
		// No day headers — insert after file header + any blank lines.
		insertIdx = 1
		for insertIdx < len(lines) && lines[insertIdx] == "" {
			insertIdx++
		}
	}

	out := make([]string, 0, len(lines)+len(spliceContent))
	out = append(out, lines[:insertIdx]...)
	out = append(out, spliceContent...)
	out = append(out, lines[insertIdx:]...)
	return WriteAtomic(path, out)
}

// ArchiveOpts captures the fields a caller may set when building an archive
// entry via FormatArchiveEntry. Empty optional fields are omitted from the
// rendered output. ID, Title, Started, Completed, and Summary are required
// in practice (the orchestrator enforces non-empty values).
type ArchiveOpts struct {
	ID        string
	Title     string
	Repo      string
	Tags      []string
	PR        string
	Files     []string
	Parent    string
	Started   string // YYYY-MM-DD
	Completed string // YYYY-MM-DD
	Summary   string
	Feedback  []string
	Time      string
}

// FormatArchiveEntry renders a single archive entry as a slice of lines (no
// leading or trailing blank). Field order is fixed per the spec:
//   ### <id> — <title>
//   Repo, Tags, PR, Files, Parent, Started → Completed, Summary,
//   Feedback / Notes (with sub-bullets), Time.
func FormatArchiveEntry(opts ArchiveOpts) []string {
	lines := []string{fmt.Sprintf("### %s — %s", opts.ID, opts.Title)}

	addStr := func(field, value string) {
		if value == "" {
			return
		}
		lines = append(lines, fmt.Sprintf("- **%s**: %s", field, value))
	}
	addCSV := func(field string, values []string) {
		if len(values) == 0 {
			return
		}
		lines = append(lines, fmt.Sprintf("- **%s**: %s", field, strings.Join(values, ", ")))
	}

	addStr("Repo", opts.Repo)
	addCSV("Tags", opts.Tags)
	addStr("PR", opts.PR)
	addCSV("Files", opts.Files)
	addStr("Parent", opts.Parent)

	if opts.Started != "" || opts.Completed != "" {
		lines = append(lines, fmt.Sprintf("- **Started → Completed**: %s → %s",
			opts.Started, opts.Completed))
	}

	addStr("Summary", opts.Summary)

	if len(opts.Feedback) > 0 {
		lines = append(lines, "- **Feedback / Notes**:")
		for _, fb := range opts.Feedback {
			lines = append(lines, fmt.Sprintf("  - %s", fb))
		}
	}

	addStr("Time", opts.Time)

	return lines
}

// EpicBlockOptions captures the fields a caller may set when constructing an
// epic block via FormatEpicBlock. ID, Title, and NotesRef are required in
// practice; other fields are omitted when empty.
type EpicBlockOptions struct {
	ID             string
	Title          string
	Repo           string
	Tags           []string
	NotesRef       string // typically `notes/<id>.md`
	Plan           string // optional, e.g. `repo/PLAN.md`
	ActiveChildren []string
	Status         string
}

// FormatEpicBlock renders an epic container block as a slice of lines (no
// trailing blank). Epics always emit `**Type**: epic` and an
// `**Active children**:` field — either the supplied list or `<none>`.
func FormatEpicBlock(o EpicBlockOptions) []string {
	bold := strings.ToUpper(o.ID)
	if bold == "" {
		bold = o.Title
	}

	lines := []string{
		fmt.Sprintf("- [ ] **%s** — %s", bold, o.Title),
		fmt.Sprintf("  - **ID**: %s", o.ID),
		"  - **Type**: epic",
	}

	addStr := func(field, value string) {
		if value == "" {
			return
		}
		lines = append(lines, fmt.Sprintf("  - **%s**: %s", field, value))
	}
	addCSV := func(field string, values []string) {
		if len(values) == 0 {
			return
		}
		lines = append(lines, fmt.Sprintf("  - **%s**: %s", field, strings.Join(values, ", ")))
	}

	addStr("Repo", o.Repo)
	addCSV("Tags", o.Tags)
	addStr("Notes", o.NotesRef)
	addStr("Plan", o.Plan)

	if len(o.ActiveChildren) == 0 {
		lines = append(lines, "  - **Active children**: <none>")
	} else {
		lines = append(lines, fmt.Sprintf("  - **Active children**: %s",
			strings.Join(o.ActiveChildren, ", ")))
	}

	addStr("Status", o.Status)
	return lines
}

var (
	childrenHeadingRe = regexp.MustCompile(`^## Children\s*$`)
	anyL2HeadingRe    = regexp.MustCompile(`^## `)
	anyCheckboxRe     = regexp.MustCompile(`^- \[[ ~x]\]\s+`)
)

// AppendChildToNotes appends a new `- [ ] <childID>: <title>` line to a
// notes file. Insertion point heuristic:
//
//  1. If the file has a `## Children` section, insert after the last
//     non-blank line of that section (or directly after the heading if
//     empty).
//  2. Else, if the file has any existing checkbox lines, insert right
//     after the last one.
//  3. Else, append at the end of the file (with a leading blank if the
//     file doesn't end with one already).
//
// Returns the updated bytes (with a single trailing newline).
func AppendChildToNotes(notesBytes []byte, childID, title string) []byte {
	s := strings.TrimSuffix(string(notesBytes), "\n")
	var lines []string
	if s != "" {
		lines = strings.Split(s, "\n")
	}

	newLine := "- [ ] " + childID
	if strings.TrimSpace(title) != "" {
		newLine += ": " + strings.TrimSpace(title)
	}

	// Case 1: ## Children section
	childrenIdx := -1
	for i, l := range lines {
		if childrenHeadingRe.MatchString(l) {
			childrenIdx = i
			break
		}
	}
	if childrenIdx >= 0 {
		// Determine end of section (next ## heading or EOF).
		endIdx := len(lines)
		for i := childrenIdx + 1; i < len(lines); i++ {
			if anyL2HeadingRe.MatchString(lines[i]) {
				endIdx = i
				break
			}
		}
		// Walk back from endIdx to find the last non-blank line within the
		// section; insert immediately after.
		insertAfter := childrenIdx
		for i := endIdx - 1; i > childrenIdx; i-- {
			if strings.TrimSpace(lines[i]) != "" {
				insertAfter = i
				break
			}
		}
		out := make([]string, 0, len(lines)+1)
		out = append(out, lines[:insertAfter+1]...)
		out = append(out, newLine)
		out = append(out, lines[insertAfter+1:]...)
		return []byte(strings.Join(out, "\n") + "\n")
	}

	// Case 2: last checkbox anywhere in the file
	lastCheckbox := -1
	for i, l := range lines {
		if anyCheckboxRe.MatchString(l) {
			lastCheckbox = i
		}
	}
	if lastCheckbox >= 0 {
		out := make([]string, 0, len(lines)+1)
		out = append(out, lines[:lastCheckbox+1]...)
		out = append(out, newLine)
		out = append(out, lines[lastCheckbox+1:]...)
		return []byte(strings.Join(out, "\n") + "\n")
	}

	// Case 3: append at EOF with a leading blank if needed.
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
		lines = append(lines, "")
	}
	lines = append(lines, newLine)
	return []byte(strings.Join(lines, "\n") + "\n")
}

// AppendToSection returns a new line slice with blockLines inserted at the end
// of the named section. A blank line is added before the insert when the
// section's last line isn't already blank, and a blank line is appended after
// the insert for separation from the following section.
func AppendToSection(doc *model.WorkDoc, section model.SectionName, blockLines []string) ([]string, error) {
	sec := doc.Section(section)
	if sec == nil {
		return nil, fmt.Errorf("render: section %q not found in %s", section, doc.Path)
	}

	insert := make([]string, 0, len(blockLines)+2)
	if sec.EndLine > 0 && doc.Lines[sec.EndLine-1] != "" {
		insert = append(insert, "")
	}
	insert = append(insert, blockLines...)
	insert = append(insert, "")

	out := make([]string, 0, len(doc.Lines)+len(insert))
	out = append(out, doc.Lines[:sec.EndLine]...)
	out = append(out, insert...)
	out = append(out, doc.Lines[sec.EndLine:]...)
	return out, nil
}

// SetBlockNotesRef rewrites (or inserts) the `**Notes**:` line for the block
// identified by blockID. Insertion point when the line is absent: after the
// `**PR**:` line if present, otherwise before `**Started**:`, otherwise at the
// end of the block's metadata range.
func SetBlockNotesRef(doc *model.WorkDoc, blockID, value string) ([]string, error) {
	b := doc.BlockByID(blockID)
	if b == nil {
		return nil, fmt.Errorf("%w: %q", ErrBlockNotFound, blockID)
	}

	out := make([]string, len(doc.Lines))
	copy(out, doc.Lines)

	newLine := "  - **Notes**: " + value

	metaStart := b.StartLine
	metaEnd := b.EndLine

	// 1) Existing Notes line → rewrite in place.
	for i := metaStart; i < metaEnd && i < len(out); i++ {
		if notesLineRe.MatchString(out[i]) {
			out[i] = newLine
			return out, nil
		}
	}

	// 2) Insert after PR line, else before Started, else at end of metadata.
	insertAt := -1
	for i := metaStart; i < metaEnd && i < len(out); i++ {
		if prLineRe.MatchString(out[i]) {
			insertAt = i + 1
			break
		}
	}
	if insertAt < 0 {
		for i := metaStart; i < metaEnd && i < len(out); i++ {
			if startedLineRe.MatchString(out[i]) {
				insertAt = i
				break
			}
		}
	}
	if insertAt < 0 {
		insertAt = metaEnd
	}

	res := make([]string, 0, len(out)+1)
	res = append(res, out[:insertAt]...)
	res = append(res, newLine)
	res = append(res, out[insertAt:]...)
	return res, nil
}

// SetBlockWaitingSince rewrites (or inserts/removes) the `**Waiting since**:`
// line for the block identified by blockID. When value is empty the line is
// removed entirely. Insertion point when absent: after `**Started**:`, else at
// end of the block's metadata range.
func SetBlockWaitingSince(doc *model.WorkDoc, blockID, value string) ([]string, error) {
	b := doc.BlockByID(blockID)
	if b == nil {
		return nil, fmt.Errorf("%w: %q", ErrBlockNotFound, blockID)
	}

	out := make([]string, len(doc.Lines))
	copy(out, doc.Lines)

	metaStart := b.StartLine
	metaEnd := b.EndLine

	// 1) Existing line: rewrite (non-empty) or remove (empty).
	for i := metaStart; i < metaEnd && i < len(out); i++ {
		if waitingSinceLineRe.MatchString(out[i]) {
			if value == "" {
				res := make([]string, 0, len(out)-1)
				res = append(res, out[:i]...)
				res = append(res, out[i+1:]...)
				return res, nil
			}
			out[i] = "  - **Waiting since**: " + value
			return out, nil
		}
	}

	if value == "" {
		return out, nil
	}

	// 2) Insert after Started line, else at end of metadata range.
	insertAt := metaEnd
	for i := metaStart; i < metaEnd && i < len(out); i++ {
		if startedLineRe.MatchString(out[i]) {
			insertAt = i + 1
			break
		}
	}

	res := make([]string, 0, len(out)+1)
	res = append(res, out[:insertAt]...)
	res = append(res, "  - **Waiting since**: "+value)
	res = append(res, out[insertAt:]...)
	return res, nil
}

// InsertSectionBefore inserts `## newSection` (and a blank line after it)
// immediately before beforeSection's heading. Returns an error if beforeSection
// is not found.
func InsertSectionBefore(doc *model.WorkDoc, newSection, beforeSection model.SectionName) ([]string, error) {
	sec := doc.Section(beforeSection)
	if sec == nil {
		return nil, fmt.Errorf("render: section %q not found in %s", beforeSection, doc.Path)
	}
	insertIdx := sec.HeadLine - 1
	out := make([]string, 0, len(doc.Lines)+2)
	out = append(out, doc.Lines[:insertIdx]...)
	out = append(out, "## "+string(newSection), "")
	out = append(out, doc.Lines[insertIdx:]...)
	return out, nil
}

// WriteAtomic writes lines to path with a trailing newline, via tempfile +
// rename so a partial write cannot leave the target corrupted.
func WriteAtomic(path string, lines []string) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("render: mkdir %s: %w", dir, err)
	}

	f, err := os.CreateTemp(dir, "."+base+".*.tmp")
	if err != nil {
		return fmt.Errorf("render: create temp in %s: %w", dir, err)
	}
	tmpName := f.Name()
	cleanup := func() { _ = os.Remove(tmpName) }

	content := strings.Join(lines, "\n")
	if len(lines) > 0 || content != "" {
		content += "\n"
	}
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		cleanup()
		return fmt.Errorf("render: write temp: %w", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return fmt.Errorf("render: close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("render: rename to %s: %w", path, err)
	}
	return nil
}

// BlockOptions captures the fields a caller may set when constructing a ticket
// block via FormatTicketBlock. Empty optional fields are omitted from the
// rendered output.
type BlockOptions struct {
	Title      string // required
	ID         string // required
	Type       string // optional ("ticket", "spike", "chore", "epic")
	Parent     string // optional
	Repo       string // optional
	Tags       []string
	Started    string // optional, YYYY-MM-DD
	PR         string // optional
	Source     string // optional upstream URL
	Files      []string
	Acceptance string
	NotesRef     string
	Status       string
	WaitingSince string // YYYY-MM-DD, only set for ## Waiting tickets
	State        model.State // defaults to StatePending
}

// FormatTicketBlock renders a single bullet block plus its indented metadata
// as a slice of lines (no trailing blank). The caller decides where to splice
// the result.
func FormatTicketBlock(o BlockOptions) []string {
	state := o.State
	if state == "" {
		state = model.StatePending
	}

	bold := strings.ToUpper(o.ID)
	if bold == "" {
		bold = o.Title
	}

	lines := []string{
		fmt.Sprintf("- [%s] **%s** — %s", state, bold, o.Title),
		fmt.Sprintf("  - **ID**: %s", o.ID),
	}
	add := func(field, value string) {
		if value == "" {
			return
		}
		lines = append(lines, fmt.Sprintf("  - **%s**: %s", field, value))
	}
	addCSV := func(field string, values []string) {
		if len(values) == 0 {
			return
		}
		lines = append(lines, fmt.Sprintf("  - **%s**: %s", field, strings.Join(values, ", ")))
	}

	add("Type", o.Type)
	add("Parent", o.Parent)
	add("Repo", o.Repo)
	addCSV("Tags", o.Tags)
	// PR is always rendered (even empty) so the field is visibly available
	// for the user / TUI to fill in without re-editing the block manually.
	lines = append(lines, "  - **PR**: "+o.PR)
	add("Source", o.Source)
	add("Notes", o.NotesRef)
	add("Started", o.Started)
	add("Waiting since", o.WaitingSince)
	addCSV("Files", o.Files)
	add("Acceptance", o.Acceptance)
	add("Status", o.Status)

	return lines
}
