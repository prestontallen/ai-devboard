package convert

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

var (
	dayRe      = regexp.MustCompile(`^## (\d{4}-\d{2}-\d{2})\s*$`)
	entryRe    = regexp.MustCompile(`^### (\S+) — (.*)$`)
	archMetaRe = regexp.MustCompile(`^- \*\*(.+?)\*\*:(?:\s(.*))?$`)
	fbBulletRe = regexp.MustCompile(`^  - (.*)$`)
	datePairRe = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}) → (\d{4}-\d{2}-\d{2})$`)
)

// ArchiveMonth parses one archive/YYYY-MM.md into archived ticket
// fragments. Full fidelity: every field FormatArchiveEntry emits is
// captured (the CLI's own archive parser keeps 8 of ~14 — that lossiness
// is exactly what this parser exists to not have). Unmodeled lines refuse.
func ArchiveMonth(month string, data []byte) ([]*store.Ticket, error) {
	lines := splitNorm(data)
	var out []*store.Ticket
	var cur *store.Ticket
	inFeedback := false

	for i, line := range lines {
		lineNo := i + 1
		switch {
		case i == 0 && strings.HasPrefix(line, "# "):
			continue // month title
		case dayRe.MatchString(line):
			cur, inFeedback = nil, false
			continue
		}
		if m := entryRe.FindStringSubmatch(line); m != nil {
			cur = &store.Ticket{
				Slug:  store.NormalizeSlug(m[1]),
				Title: strings.TrimSpace(m[2]),
				Type:  store.TypeTicket,
				State: store.StateDone,
				// Archived entries carry no section; identity of the file
				// they render into is the month.
				Archived:     true,
				ArchiveMonth: month,
			}
			inFeedback = false
			out = append(out, cur)
			continue
		}
		if strings.TrimSpace(line) == "" {
			inFeedback = false
			continue
		}
		if cur == nil {
			return nil, fmt.Errorf("archive/%s.md:%d: content before first entry: %q", month, lineNo, line)
		}
		if inFeedback {
			if m := fbBulletRe.FindStringSubmatch(line); m != nil {
				cur.ArchiveFeedback = append(cur.ArchiveFeedback, m[1])
				continue
			}
			inFeedback = false
		}
		m := archMetaRe.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("archive/%s.md:%d: unmodeled content %q — refusing to convert", month, lineNo, line)
		}
		label, value := strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
		switch strings.ToLower(label) {
		case "repo":
			cur.Repo = value
		case "tags":
			cur.Tags = splitCSV(value)
		case "pr":
			v := value
			cur.PR = &v
		case "files":
			cur.Files = splitCSV(value)
		case "parent":
			cur.ExtraFields = addField(cur.ExtraFields, "__parent_slug", store.NormalizeSlug(value))
		case "type":
			cur.Type = strings.ToLower(value)
		case "started → completed":
			dp := datePairRe.FindStringSubmatch(value)
			if dp == nil {
				return nil, fmt.Errorf("archive/%s.md:%d: malformed date pair %q", month, lineNo, value)
			}
			cur.Started, cur.Completed = dp[1], dp[2]
		case "completed":
			cur.Completed = value
		case "notes":
			cur.ExtraFields = addField(cur.ExtraFields, "__notes_ref", value)
		case "plan":
			cur.PlanText = value
		case "children":
			cur.ExtraFields = addField(cur.ExtraFields, "__archived_children", strings.ToLower(value))
		case "summary":
			cur.Summary = value
		case "feedback / notes":
			inFeedback = true
		case "time":
			cur.TimeSpent = value
		default:
			cur.ExtraFields = addField(cur.ExtraFields, label, value)
		}
	}
	return out, nil
}
