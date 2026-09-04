// Package hazard detects corpus constructs the parsers drop WITHOUT
// refusing.
//
// This is the half of the adoption oracle a round trip structurally cannot
// provide. store.Canonical proves the store survives a lap through its own
// file representation, but a construct dropped at parse time is dropped
// identically on both sides of that lap: the renderer never emits it, the
// re-parse never sees it, and the two stores match perfectly. The corpus
// changed and every check reported clean.
//
// So these detectors read raw bytes and never go through convert. Each one
// is a construct verified in the code to be silently discarded or silently
// rewritten. Everything here BLOCKS: the point is to convert a silent loss
// into a refusal naming a file and a line.
package hazard

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Finding is one construct that would not survive a conversion intact.
type Finding struct {
	File      string // relative to the corpus root, devboard/ prefixed
	Line      int    // 1-based; 0 when the finding is about the file itself
	Construct string // stable id, e.g. "workmd-preamble"
	Detail    string
}

func (f Finding) String() string {
	if f.Line > 0 {
		return fmt.Sprintf("%s:%d: [%s] %s", f.File, f.Line, f.Construct, f.Detail)
	}
	return fmt.Sprintf("%s: [%s] %s", f.File, f.Construct, f.Detail)
}

var (
	fieldRe       = regexp.MustCompile(`^\s*-\s+\*\*([^*]+)\*\*:(.*)$`)
	dayRe         = regexp.MustCompile(`^##\s+(\d{4}-\d{2}-\d{2})\s*$`)
	entryRe       = regexp.MustCompile(`^###\s+(\S+)`)
	ticketRe      = regexp.MustCompile(`^-\s+\[.\]\s+(?:\*\*([A-Za-z0-9._-]+)\*\*\s+[—-]+\s*)?(.*)$`)
	archiveHeadRe = regexp.MustCompile(`^###\s+(\S+)\s+[—-]+\s*(.*)$`)
)

// feedbackFields are the only labels internal/feedback reads. Anything else
// hits its "Unknown field: skip the line, keep the entry" branch, a surface
// with no refusal path at all.
var feedbackFields = map[string]bool{
	"Trigger": true, "Excerpt": true, "Context": true, "Resolved": true,
}

// Scan runs every detector over a corpus. devboardDir may be empty.
func Scan(worklogDir, devboardDir string) ([]Finding, error) {
	var out []Finding

	work := filepath.Join(worklogDir, "WORK.md")
	if lines, err := readLines(work); err == nil {
		out = append(out, scanWorkMD(lines)...)
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	if lines, err := readLines(filepath.Join(worklogDir, "FEEDBACK.md")); err == nil {
		out = append(out, scanFeedback(lines)...)
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	months, err := os.ReadDir(filepath.Join(worklogDir, "archive"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, m := range months {
		if !strings.HasSuffix(m.Name(), ".md") {
			continue
		}
		lines, err := readLines(filepath.Join(worklogDir, "archive", m.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, scanArchive("archive/"+m.Name(), lines)...)
	}

	if devboardDir != "" {
		db, err := scanDevboard(worklogDir, devboardDir)
		if err != nil {
			return nil, err
		}
		out = append(out, db...)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out, nil
}

// scanWorkMD covers three constructs.
func scanWorkMD(lines []string) []Finding {
	var out []Finding

	// 1. Preamble. convert.WorkMD skips every line before the first
	// "## " heading and projection.WorkMD writes a fixed banner and a
	// hardcoded "# Worklog — active" back, so anything else here is gone.
	for i, line := range lines {
		if strings.HasPrefix(line, "## ") {
			break
		}
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "<!--") || strings.HasPrefix(t, "# ") {
			continue
		}
		out = append(out, Finding{"WORK.md", i + 1, "workmd-preamble",
			fmt.Sprintf("content before the first section is discarded on read: %q", t)})
	}

	// 2 and 3. Per-block field bullets.
	seen := map[string]int{}
	blockStart := 0
	flushBlock := func() { seen = map[string]int{} }
	for i, line := range lines {
		if strings.HasPrefix(line, "## ") || ticketRe.MatchString(line) {
			flushBlock()
			blockStart = i + 1
			continue
		}
		m := fieldRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		label, value := strings.TrimSpace(m[1]), strings.TrimSpace(m[2])

		// 2. An empty value is dropped by the renderer for every label
		// except PR, which it deliberately emits empty.
		if value == "" && label != "PR" {
			out = append(out, Finding{"WORK.md", i + 1, "empty-field-value",
				fmt.Sprintf("%q has no value and is not re-emitted", label)})
		}
		// 3. ExtraFields is a map[string]string, so a repeated label in one
		// block silently keeps only the last.
		if prev, dup := seen[label]; dup {
			out = append(out, Finding{"WORK.md", i + 1, "duplicate-field-label",
				fmt.Sprintf("%q also appears at line %d; only the last survives", label, prev)})
		}
		seen[label] = i + 1
		_ = blockStart
	}
	return out
}

// scanArchive covers the day-heading pair. convert.ArchiveMonth consumes
// the "## YYYY-MM-DD" heading and throws it away; projection re-derives the
// day by grouping on each entry's Completed. An entry with no Completed
// therefore renders under a bare "## " that the NEXT parse refuses, and an
// entry whose Completed disagrees with its heading silently moves day.
func scanArchive(file string, lines []string) []Finding {
	var out []Finding
	day := ""
	entry, entryLine := "", 0
	completed := ""

	flush := func() {
		if entry == "" {
			return
		}
		switch {
		case completed == "":
			out = append(out, Finding{file, entryLine, "archive-entry-missing-completed",
				fmt.Sprintf("%q has no Completed date; the day heading is re-derived from it, so this renders under a bare heading the next parse refuses", entry)})
		case day != "" && completed != day:
			out = append(out, Finding{file, entryLine, "archive-day-mismatch",
				fmt.Sprintf("%q is under %s but Completed is %s; it silently moves day on render", entry, day, completed)})
		}
		entry, entryLine, completed = "", 0, ""
	}

	for i, line := range lines {
		if m := dayRe.FindStringSubmatch(line); m != nil {
			flush()
			day = m[1]
			continue
		}
		if m := entryRe.FindStringSubmatch(line); m != nil {
			flush()
			entry, entryLine = m[1], i+1
			continue
		}
		if m := fieldRe.FindStringSubmatch(line); m != nil && entry != "" {
			label, value := strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
			switch label {
			case "Completed":
				completed = value
			case "Started → Completed":
				if parts := strings.Split(value, "→"); len(parts) == 2 {
					completed = strings.TrimSpace(parts[1])
				}
			}
		}
	}
	flush()
	return out
}

// scanFeedback covers internal/feedback's unknown-label branch, which
// discards the line and keeps the entry, with no refusal path anywhere.
func scanFeedback(lines []string) []Finding {
	var out []Finding
	for i, line := range lines {
		if !strings.HasPrefix(line, "**") {
			continue
		}
		label := line
		if j := strings.Index(line[2:], "**"); j >= 0 {
			label = line[2 : 2+j]
		}
		if !feedbackFields[label] {
			out = append(out, Finding{"FEEDBACK.md", i + 1, "feedback-unknown-field",
				fmt.Sprintf("%q is not a field the reader knows; the line is dropped and the entry kept", label)})
		}
	}
	return out
}

// scanDevboard covers the YAML constructs plus the two cross-file ones.
//
// It decodes with yaml.v3's Node API rather than scanning text: comments,
// anchors and quoting are exactly what these detectors are about, and a
// regex over raw lines gets them wrong. A first cut did, reporting three
// findings on the live corpus that were all artefacts of guessing at
// quoting rather than real hazards.
func scanDevboard(worklogDir, devboardDir string) ([]Finding, error) {
	titles, err := corpusTitles(worklogDir)
	if err != nil {
		return nil, err
	}

	var out []Finding
	joins := map[string][]string{}

	err = filepath.Walk(devboardDir, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return err
		}
		if !strings.HasSuffix(p, ".yaml") && !strings.HasSuffix(p, ".yml") {
			return nil
		}
		rel, _ := filepath.Rel(devboardDir, p)
		rel = "devboard/" + filepath.ToSlash(rel)

		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		var doc yaml.Node
		if err := yaml.Unmarshal(data, &doc); err != nil {
			out = append(out, Finding{rel, 0, "yaml-unparseable", err.Error()})
			return nil
		}
		out = append(out, walkYAML(rel, &doc)...)

		root := docRoot(&doc)
		if root == nil {
			return nil
		}
		if v, line := mapValue(root, "worklog"); v != "" {
			joins[v] = append(joins[v], rel)
			_ = line
		}
		// boardFragment never reads a top-level title; the renderer
		// re-derives it from the ticket, so a disagreeing value here is
		// silently replaced on the next write.
		if got, line := mapValue(root, "title"); got != "" {
			slug := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
			if want, ok := titles[slug]; ok && want != "" && got != want {
				out = append(out, Finding{rel, line, "devboard-title-mismatch",
					fmt.Sprintf("title %q is never read; the ticket's %q replaces it on render", got, want)})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// convert merges board files onto a ticket with no duplicate check, so
	// two files claiming one slug silently collapse.
	for slug, files := range joins {
		if len(files) > 1 {
			sort.Strings(files)
			out = append(out, Finding{files[0], 0, "devboard-duplicate-join",
				fmt.Sprintf("slug %q is claimed by %s; they merge with no duplicate check", slug, strings.Join(files, ", "))})
		}
	}
	return out, nil
}

// walkYAML reports comments, anchors, aliases and duplicate mapping keys.
// yamlx decodes to plain Go values, so none of these have anywhere to live
// in the store and all vanish on the next render.
func walkYAML(file string, n *yaml.Node) []Finding {
	var out []Finding
	var walk func(*yaml.Node)
	walk = func(n *yaml.Node) {
		if n == nil {
			return
		}
		for _, c := range []string{n.HeadComment, n.LineComment, n.FootComment} {
			if strings.TrimSpace(c) != "" {
				out = append(out, Finding{file, n.Line, "yaml-comment",
					fmt.Sprintf("comment %q is not represented in the store and vanishes on render", strings.TrimSpace(c))})
				break
			}
		}
		if n.Anchor != "" {
			out = append(out, Finding{file, n.Line, "yaml-anchor",
				fmt.Sprintf("anchor &%s is expanded on read and never written back", n.Anchor)})
		}
		if n.Kind == yaml.AliasNode {
			out = append(out, Finding{file, n.Line, "yaml-anchor",
				"an alias is expanded on read and never written back"})
		}
		if n.Kind == yaml.MappingNode {
			seen := map[string]int{}
			for i := 0; i+1 < len(n.Content); i += 2 {
				k := n.Content[i]
				if prev, dup := seen[k.Value]; dup {
					out = append(out, Finding{file, k.Line, "yaml-duplicate-key",
						fmt.Sprintf("key %q also appears at line %d; only one survives", k.Value, prev)})
				}
				seen[k.Value] = k.Line
			}
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(n)
	return out
}

func docRoot(doc *yaml.Node) *yaml.Node {
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		return doc.Content[0]
	}
	return doc
}

// mapValue returns a top-level scalar value and the line it sits on.
func mapValue(root *yaml.Node, key string) (string, int) {
	if root.Kind != yaml.MappingNode {
		return "", 0
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key && root.Content[i+1].Kind == yaml.ScalarNode {
			return root.Content[i+1].Value, root.Content[i+1].Line
		}
	}
	return "", 0
}

// corpusTitles maps slug to the title WORK.md or the archive gives it.
func corpusTitles(worklogDir string) (map[string]string, error) {
	out := map[string]string{}
	if lines, err := readLines(filepath.Join(worklogDir, "WORK.md")); err == nil {
		for _, line := range lines {
			if m := ticketRe.FindStringSubmatch(line); m != nil && m[1] != "" {
				out[strings.ToLower(m[1])] = strings.TrimSpace(m[2])
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	months, err := os.ReadDir(filepath.Join(worklogDir, "archive"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, m := range months {
		if !strings.HasSuffix(m.Name(), ".md") {
			continue
		}
		lines, err := readLines(filepath.Join(worklogDir, "archive", m.Name()))
		if err != nil {
			return nil, err
		}
		for _, line := range lines {
			if mm := archiveHeadRe.FindStringSubmatch(line); mm != nil {
				out[strings.ToLower(mm[1])] = strings.TrimSpace(mm[2])
			}
		}
	}
	return out, nil
}

func readLines(p string) ([]string, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n"), nil
}
