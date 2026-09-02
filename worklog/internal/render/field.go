package render

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
)

// ErrUnknownField is returned by SetBlockField for a field name that isn't in
// the canonical order table.
var ErrUnknownField = fmt.Errorf("render: unknown metadata field")

// ErrNoTitleSeparator is returned by SetBlockTitle when a block's bullet line
// isn't in `**ID** — Title` form, so there's no title to replace.
var ErrNoTitleSeparator = fmt.Errorf("render: bullet line has no ID/title separator")

// fieldOrder is the order metadata lines appear in — a supersequence of what
// FormatTicketBlock and FormatEpicBlock emit, so one table covers both.
// SetBlockField places a newly inserted line by this table, which is what
// keeps hand-inserted fields landing where a freshly rendered block would put
// them.
var fieldOrder = []string{
	"ID",
	"Type",
	"Parent",
	"Repo",
	"Tags",
	"PR",
	"Source",
	"Notes",
	"Plan",
	"Started",
	"Waiting since",
	"Files",
	"Acceptance",
	"Active children",
	"Status",
}

var (
	// metaFieldRe captures the field name of an indented metadata line.
	metaFieldRe = regexp.MustCompile(`^  - \*\*(.+?)\*\*:`)
	// bulletTitleRe splits a bullet line into everything through the title
	// separator, plus the title itself. Both em dash and `--` are accepted,
	// matching the parser.
	bulletTitleRe = regexp.MustCompile(`^(- \[[ ~x]\]\s*\*\*.+?\*\*\s*(?:—|--)\s*)(.*)$`)
)

// fieldRank returns a field's position in canonical order (case-insensitive),
// or -1 for a field the table doesn't know.
func fieldRank(field string) int {
	for i, f := range fieldOrder {
		if strings.EqualFold(f, field) {
			return i
		}
	}
	return -1
}

// CanonicalField returns the table's spelling of field ("acceptance" →
// "Acceptance") and whether the field is known.
func CanonicalField(field string) (string, bool) {
	if r := fieldRank(field); r >= 0 {
		return fieldOrder[r], true
	}
	return "", false
}

// EditableFields returns the canonical names `worklog edit` is allowed to
// write, in canonical order. Everything else in fieldOrder is owned by a
// lifecycle command (ID/Type/Parent are structural, Started and Waiting since
// are stamped by start/wait, PR belongs to `worklog pr`, Active children to
// the epic machinery).
func EditableFields() []string {
	return []string{"Repo", "Tags", "Notes", "Files", "Acceptance", "Status"}
}

// metaBounds returns the half-open, 0-indexed range of a block's metadata
// lines. The parser keeps trailing blank lines inside a block's range, so they
// are trimmed here: without that, a field appended at the end would land after
// the blank separating this block from the next.
func metaBounds(lines []string, b *model.Block) (int, int) {
	start, end := b.StartLine, b.EndLine
	if end > len(lines) {
		end = len(lines)
	}
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return start, end
}

// SetBlockField rewrites, inserts, or removes one `**Field**:` metadata line
// on the block identified by blockID, returning the full line slice with every
// untouched line preserved byte-for-byte.
//
// An empty value removes the line (and is a no-op when the field is already
// absent). A field the block doesn't have yet is inserted in canonical
// position: before the first line that outranks it, else after the block's
// last metadata line. Metadata lines the table doesn't know are stepped over
// rather than treated as a boundary, so a hand-written field can't displace a
// canonical one.
func SetBlockField(doc *model.WorkDoc, blockID, field, value string) ([]string, error) {
	b := doc.BlockByID(blockID)
	if b == nil {
		return nil, fmt.Errorf("%w: %q", ErrBlockNotFound, blockID)
	}
	name, ok := CanonicalField(field)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownField, field)
	}

	out := make([]string, len(doc.Lines))
	copy(out, doc.Lines)
	start, end := metaBounds(out, b)

	for i := start; i < end; i++ {
		m := metaFieldRe.FindStringSubmatch(out[i])
		if m == nil || !strings.EqualFold(strings.TrimSpace(m[1]), name) {
			continue
		}
		if value == "" {
			return append(out[:i:i], out[i+1:]...), nil
		}
		out[i] = metaLine(name, value)
		return out, nil
	}

	if value == "" {
		return out, nil
	}

	rank := fieldRank(name)
	insertAt := end
	for i := start; i < end; i++ {
		m := metaFieldRe.FindStringSubmatch(out[i])
		if m == nil {
			continue
		}
		if fieldRank(strings.TrimSpace(m[1])) > rank {
			insertAt = i
			break
		}
	}

	res := make([]string, 0, len(out)+1)
	res = append(res, out[:insertAt]...)
	res = append(res, metaLine(name, value))
	res = append(res, out[insertAt:]...)
	return res, nil
}

// SetBlockTitle replaces the title on a block's bullet line, leaving the
// checkbox state and the bold ID exactly as they were.
func SetBlockTitle(doc *model.WorkDoc, blockID, title string) ([]string, error) {
	b := doc.BlockByID(blockID)
	if b == nil {
		return nil, fmt.Errorf("%w: %q", ErrBlockNotFound, blockID)
	}

	idx := b.StartLine - 1
	m := bulletTitleRe.FindStringSubmatch(doc.Lines[idx])
	if m == nil {
		return nil, fmt.Errorf("%w: %q", ErrNoTitleSeparator, blockID)
	}

	out := make([]string, len(doc.Lines))
	copy(out, doc.Lines)
	out[idx] = m[1] + title
	return out, nil
}

func metaLine(field, value string) string {
	return "  - **" + field + "**: " + value
}
