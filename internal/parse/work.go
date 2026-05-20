// Package parse turns a WORK.md file (or its raw bytes) into a model.WorkDoc.
//
// The parser is line-oriented and preserves 1-indexed line numbers on every
// block and section so the renderer can mutate the on-disk file via splices.
package parse

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/prestontallen/day2day/internal/model"
)

var (
	sectionHeading = regexp.MustCompile(`^## +(.+?)\s*$`)
	blockStart     = regexp.MustCompile(`^- \[([ ~x])\]\s*(.*)$`)
	metaField      = regexp.MustCompile(`^  - \*\*(.+?)\*\*:\s*(.*)$`)
	// `**ID** — Title` or `**ID** -- Title` form for the bullet line body.
	titleIDRe = regexp.MustCompile(`^\*\*(.+?)\*\*\s*(?:—|--)\s*(.*)$`)
)

// File reads and parses the WORK.md at path.
func File(path string) (*model.WorkDoc, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", model.ErrWorkMDMissing, path)
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return Bytes(path, data)
}

// Bytes parses data as a WORK.md whose origin is path (used only for error
// messages and for round-tripping back to disk).
func Bytes(path string, data []byte) (*model.WorkDoc, error) {
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	var lines []string
	if s != "" {
		lines = strings.Split(s, "\n")
	}

	doc := &model.WorkDoc{Path: path, Lines: lines}

	var sec *model.Section
	var blk *model.Block
	var blkSection model.SectionName

	closeBlock := func(endLine int) {
		if blk == nil || sec == nil {
			blk = nil
			return
		}
		blk.EndLine = endLine
		sec.Blocks = append(sec.Blocks, *blk)
		blk = nil
	}

	closeSection := func(endLine int) {
		closeBlock(endLine)
		if sec != nil {
			sec.EndLine = endLine
		}
		sec = nil
	}

	for idx, line := range lines {
		lineNo := idx + 1

		if m := sectionHeading.FindStringSubmatch(line); m != nil {
			closeSection(lineNo - 1)
			name := strings.TrimSpace(m[1])
			doc.Sections = append(doc.Sections, model.Section{
				Name:     model.SectionName(name),
				HeadLine: lineNo,
			})
			sec = &doc.Sections[len(doc.Sections)-1]
			blkSection = sec.Name
			continue
		}

		if sec == nil {
			// Lines before any `## ` heading (document title, intro prose).
			continue
		}

		if m := blockStart.FindStringSubmatch(line); m != nil {
			closeBlock(lineNo - 1)
			blk = &model.Block{
				StartLine: lineNo,
				Section:   blkSection,
				State:     model.State(m[1]),
				Type:      model.TypeTicket, // overridable by **Type** metadata
			}
			body := strings.TrimSpace(m[2])
			if t := titleIDRe.FindStringSubmatch(body); t != nil {
				blk.ID = strings.ToLower(t[1])
				blk.Title = strings.TrimSpace(t[2])
			} else {
				blk.Title = body
			}
			continue
		}

		if blk != nil {
			if m := metaField.FindStringSubmatch(line); m != nil {
				field := strings.ToLower(strings.TrimSpace(m[1]))
				value := strings.TrimSpace(m[2])
				applyMeta(blk, field, value)
				continue
			}
			// Blank lines stay inside the block (preserved by line range);
			// anything else at column 0 terminates accumulation.
			if strings.TrimSpace(line) == "" {
				continue
			}
			closeBlock(lineNo - 1)
		}
	}

	if len(lines) > 0 {
		closeSection(len(lines))
	}

	// Build the ID index AFTER all slices are stable, so pointers are valid.
	byID := make(map[string]*model.Block)
	for i := range doc.Sections {
		for j := range doc.Sections[i].Blocks {
			b := &doc.Sections[i].Blocks[j]
			if b.ID != "" {
				byID[strings.ToLower(b.ID)] = b
			}
		}
	}
	doc.SetByID(byID)

	return doc, nil
}

func applyMeta(b *model.Block, field, value string) {
	switch field {
	case "id":
		b.ID = strings.ToLower(value)
	case "type":
		b.Type = model.BlockType(strings.ToLower(value))
	case "parent":
		b.Parent = strings.ToLower(value)
	case "repo":
		b.Repo = value
	case "tags":
		b.Tags = splitCSV(value)
	case "started":
		b.Started = value
	case "pr":
		b.PR = value
	case "waiting since":
		b.WaitingSince = value
	case "files":
		b.Files = splitCSV(value)
	case "acceptance":
		b.Acceptance = value
	case "notes":
		b.NotesRef = value
	case "status":
		b.Status = value
	case "active children":
		if value == "" || strings.EqualFold(value, "<none>") {
			b.ActiveChildren = nil
		} else {
			b.ActiveChildren = splitCSV(strings.ToLower(value))
		}
	}
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
