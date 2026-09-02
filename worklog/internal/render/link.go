package render

import (
	"fmt"
	"strings"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
)

// linkLine formats one **Link**: <name> — <url> metadata line.
func linkLine(name, url string) string {
	return metaLine("Link", name+" — "+url)
}

// ListBlockLinks returns every **Link**: entry on blockID's block, in
// document order.
func ListBlockLinks(doc *model.WorkDoc, blockID string) ([]model.LinkEntry, error) {
	b := doc.BlockByID(blockID)
	if b == nil {
		return nil, fmt.Errorf("%w: %q", ErrBlockNotFound, blockID)
	}
	return b.Links, nil
}

// SetBlockLink sets, updates, or removes (value == "") the named link on
// blockID's block. Link is the one repeatable field in this schema, so
// unlike SetBlockField this matches on name as well as field name: an
// existing **Link**: line for name (case-insensitive) is rewritten or
// deleted in place; a brand new name is inserted right after the block's
// last existing **Link**: line, or — if it has none yet — at Link's
// canonical fieldOrder position.
func SetBlockLink(doc *model.WorkDoc, blockID, name, value string) ([]string, error) {
	b := doc.BlockByID(blockID)
	if b == nil {
		return nil, fmt.Errorf("%w: %q", ErrBlockNotFound, blockID)
	}

	out := make([]string, len(doc.Lines))
	copy(out, doc.Lines)
	start, end := metaBounds(out, b)

	// b.Links is populated in the same top-to-bottom scan order the parser
	// walks this metadata range in, so the k-th **Link**: line encountered
	// here is b.Links[k] — no need to re-split the line's raw text.
	lastLinkLine, seen := -1, 0
	for i := start; i < end; i++ {
		m := metaFieldRe.FindStringSubmatch(out[i])
		if m == nil || !strings.EqualFold(strings.TrimSpace(m[1]), "Link") {
			continue
		}
		lastLinkLine = i
		entry := b.Links[seen]
		seen++
		if !strings.EqualFold(entry.Name, name) {
			continue
		}
		if value == "" {
			return append(out[:i:i], out[i+1:]...), nil
		}
		out[i] = linkLine(name, value)
		return out, nil
	}

	if value == "" {
		return out, nil
	}

	insertAt := end
	if lastLinkLine >= 0 {
		insertAt = lastLinkLine + 1
	} else {
		rank := fieldRank("Link")
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
	}

	res := make([]string, 0, len(out)+1)
	res = append(res, out[:insertAt]...)
	res = append(res, linkLine(name, value))
	res = append(res, out[insertAt:]...)
	return res, nil
}
