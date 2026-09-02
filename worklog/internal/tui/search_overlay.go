package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/search"
	"github.com/prestontallen/ai-devboard/worklog/internal/style"
)

type searchScope int

const (
	scopeInMemory searchScope = iota
	scopeCorpus
)

// searchResult is one entry in the overlay result list.
type searchResult struct {
	// BlockRef is set for in-memory hits and for corpus hits that are live
	// WORK.md blocks. Nil for archive / notes hits.
	BlockRef *model.Block
	File     string
	Anchor   string
	Title    string
	Snippet  string // one-line preview for corpus hits
}

type searchOverlay struct {
	input    textinput.Model
	scope    searchScope
	results  []searchResult
	selected int
	width    int
	height   int
}

func newSearchOverlay(scope searchScope) *searchOverlay {
	ti := textinput.New()
	if scope == scopeInMemory {
		ti.Placeholder = "filter tickets…"
	} else {
		ti.Placeholder = "search all (Enter to run)…"
	}
	ti.Focus()
	return &searchOverlay{input: ti, scope: scope}
}

// update processes a message and returns (cmd, done). done=true signals that
// Enter was pressed; the caller reads currentSelection() to resolve the hit.
// Esc is handled by the caller (status.go), not here.
func (o *searchOverlay) update(msg tea.Msg, blocks []model.Block, wd model.Workdir) (tea.Cmd, bool) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "up":
			if o.selected > 0 {
				o.selected--
			}
			return nil, false
		case "down":
			if o.selected < len(o.results)-1 {
				o.selected++
			}
			return nil, false
		case "enter":
			if o.scope == scopeCorpus && len(o.results) == 0 {
				// First Enter in corpus mode: run the search.
				o.refilterCorpus(wd)
				return nil, false
			}
			// Second Enter (results loaded) or in-memory Enter: done.
			return nil, true
		}
	}

	prevVal := o.input.Value()
	var cmd tea.Cmd
	o.input, cmd = o.input.Update(msg)
	newVal := o.input.Value()

	if newVal != prevVal && o.scope == scopeInMemory {
		o.refilterInMemory(blocks)
	}

	return cmd, false
}

func (o *searchOverlay) refilterInMemory(blocks []model.Block) {
	q := o.input.Value()
	if q == "" {
		o.results = nil
		o.selected = 0
		return
	}
	o.results = filterBlocks(blocks, q)
	if o.selected >= len(o.results) {
		o.selected = 0
	}
}

func (o *searchOverlay) refilterCorpus(wd model.Workdir) {
	q := strings.TrimSpace(o.input.Value())
	if q == "" {
		o.results = nil
		o.selected = 0
		return
	}
	sq := search.Query{Terms: []string{strings.ToLower(q)}, Mode: search.ModeSingle}
	out, err := search.Run(wd, search.Inputs{Query: sq, Limit: 20})
	if err != nil || len(out.Hits) == 0 {
		o.results = nil
		o.selected = 0
		return
	}
	var results []searchResult
	for _, h := range out.Hits {
		snip := h.Snippet
		if idx := strings.Index(snip, "\n"); idx >= 0 {
			snip = snip[:idx]
		}
		if len(snip) > 80 {
			snip = snip[:80] + "…"
		}
		results = append(results, searchResult{
			Anchor:  h.ID,
			File:    h.File,
			Title:   fmt.Sprintf("%s — %s (%s)", h.ID, h.Title, h.File),
			Snippet: snip,
		})
	}
	o.results = results
	o.selected = 0
}

// currentSelection returns the highlighted result, or nil if none.
func (o *searchOverlay) currentSelection() *searchResult {
	if len(o.results) == 0 || o.selected >= len(o.results) {
		return nil
	}
	return &o.results[o.selected]
}

// filterBlocks runs in-memory substring filtering across ID, Title, Tags, Repo.
func filterBlocks(blocks []model.Block, q string) []searchResult {
	q = strings.ToLower(q)
	var out []searchResult
	for i := range blocks {
		b := &blocks[i]
		hay := strings.ToLower(strings.Join([]string{
			b.ID, b.Title, strings.Join(b.Tags, ","), b.Repo,
		}, " "))
		if strings.Contains(hay, q) {
			out = append(out, searchResult{
				BlockRef: b,
				Title:    fmt.Sprintf("[%s] %s — %s", string(b.Section), strings.ToUpper(b.ID), b.Title),
			})
		}
	}
	return out
}

var (
	overlayBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(0, 1)

	selectedRow = lipgloss.NewStyle().
			Background(lipgloss.Color("62")).
			Foreground(lipgloss.Color("230"))
)

func (o *searchOverlay) view() string {
	var sb strings.Builder

	scopeLabel := "in-memory"
	if o.scope == scopeCorpus {
		scopeLabel = "corpus (Enter to search)"
	}
	sb.WriteString(style.SubHeading.Render("Search [" + scopeLabel + "]"))
	sb.WriteString("\n")
	sb.WriteString(o.input.View())
	sb.WriteString("\n")

	if len(o.results) == 0 {
		if o.input.Value() == "" {
			sb.WriteString(style.Dim.Render("type to filter…"))
		} else {
			sb.WriteString(style.Dim.Render("no matches"))
		}
	} else {
		for i, r := range o.results {
			line := r.Title
			if i == o.selected {
				line = selectedRow.Render(" ▶ " + line)
			} else {
				line = "   " + line
			}
			sb.WriteString(line)
			sb.WriteString("\n")
			if r.Snippet != "" {
				sb.WriteString(style.Dim.Render("     " + r.Snippet))
				sb.WriteString("\n")
			}
		}
	}

	w := o.width - 4
	if w < 40 {
		w = 40
	}
	return overlayBorder.Width(w).Render(sb.String())
}
