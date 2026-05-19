// Package tui hosts the Bubble Tea views for the worklog CLI.
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/prestontallen/day2day/internal/model"
	"github.com/prestontallen/day2day/internal/style"
)

// blockItem implements list.Item.
type blockItem struct{ block model.Block }

func (b blockItem) Title() string {
	if b.block.ID != "" {
		return strings.ToUpper(b.block.ID) + " — " + b.block.Title
	}
	return b.block.Title
}

func (b blockItem) Description() string {
	parts := []string{}
	switch b.block.State {
	case model.StateActive:
		parts = append(parts, "[~] active")
	case model.StateDone:
		parts = append(parts, "[x] done")
	default:
		parts = append(parts, "[ ] pending")
	}
	if b.block.Parent != "" {
		parts = append(parts, "parent: "+b.block.Parent)
	}
	if b.block.IsEpic() && len(b.block.ActiveChildren) > 0 {
		parts = append(parts, "active children: "+strings.Join(b.block.ActiveChildren, ", "))
	}
	if len(b.block.Tags) > 0 {
		parts = append(parts, "tags: "+strings.Join(b.block.Tags, ", "))
	}
	return strings.Join(parts, " • ")
}

func (b blockItem) FilterValue() string { return b.block.ID + " " + b.block.Title }

type keymap struct {
	Next key.Binding
	Prev key.Binding
	Quit key.Binding
	Help key.Binding
}

func newKeyMap() keymap {
	return keymap{
		Next: key.NewBinding(
			key.WithKeys("tab", "right", "l"),
			key.WithHelp("tab/→", "next section"),
		),
		Prev: key.NewBinding(
			key.WithKeys("shift+tab", "left", "h"),
			key.WithHelp("shift+tab/←", "prev section"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c", "esc"),
			key.WithHelp("q", "quit"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "toggle help"),
		),
	}
}

func (k keymap) ShortHelp() []key.Binding {
	return []key.Binding{k.Next, k.Prev, k.Quit, k.Help}
}

func (k keymap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Next, k.Prev}, {k.Quit, k.Help}}
}

// Status is the top-level Bubble Tea model.
type Status struct {
	doc      *model.WorkDoc
	sections []sectionView
	active   int
	keys     keymap
	help     help.Model
	width    int
	height   int
}

type sectionView struct {
	name model.SectionName
	list list.Model
}

// NewStatus builds the model from a parsed doc.
func NewStatus(doc *model.WorkDoc) *Status {
	keys := newKeyMap()

	mkSection := func(name model.SectionName) sectionView {
		var items []list.Item
		if s := doc.Section(name); s != nil {
			for _, b := range s.Blocks {
				items = append(items, blockItem{block: b})
			}
		}
		l := list.New(items, list.NewDefaultDelegate(), 0, 0)
		l.Title = "## " + string(name)
		l.SetShowHelp(false)
		l.SetShowStatusBar(true)
		l.SetFilteringEnabled(true)
		return sectionView{name: name, list: l}
	}

	return &Status{
		doc: doc,
		sections: []sectionView{
			mkSection(model.SectionNow),
			mkSection(model.SectionNext),
			mkSection(model.SectionSomeday),
		},
		keys: keys,
		help: help.New(),
	}
}

func (s *Status) Init() tea.Cmd { return nil }

func (s *Status) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = msg.Width, msg.Height
		// Reserve 4 lines: tab header (1), spacer (1), footer help (1-2).
		listHeight := msg.Height - 5
		if listHeight < 5 {
			listHeight = 5
		}
		for i := range s.sections {
			s.sections[i].list.SetSize(msg.Width-2, listHeight)
		}
	case tea.KeyMsg:
		// Don't intercept when the list is filtering — let the user type the query.
		if s.sections[s.active].list.FilterState() == list.Filtering {
			break
		}
		switch {
		case key.Matches(msg, s.keys.Quit):
			return s, tea.Quit
		case key.Matches(msg, s.keys.Next):
			s.active = (s.active + 1) % len(s.sections)
			return s, nil
		case key.Matches(msg, s.keys.Prev):
			s.active = (s.active - 1 + len(s.sections)) % len(s.sections)
			return s, nil
		case key.Matches(msg, s.keys.Help):
			s.help.ShowAll = !s.help.ShowAll
			return s, nil
		}
	}

	var cmd tea.Cmd
	s.sections[s.active].list, cmd = s.sections[s.active].list.Update(msg)
	return s, cmd
}

func (s *Status) View() string {
	if s.width == 0 {
		return "" // wait for the initial WindowSizeMsg
	}

	// Tab header
	var tabs []string
	for i, sec := range s.sections {
		count := 0
		if doc := s.doc.Section(sec.name); doc != nil {
			count = len(doc.Blocks)
		}
		label := fmt.Sprintf(" %s (%d) ", sec.name, count)
		if i == s.active {
			tabs = append(tabs, style.Heading.Render(label))
		} else {
			tabs = append(tabs, style.Dim.Render(label))
		}
	}
	header := lipgloss.JoinHorizontal(lipgloss.Top, tabs...)

	body := s.sections[s.active].list.View()
	footer := s.help.View(s.keys)

	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

// Run launches the Bubble Tea program in alt-screen mode.
func Run(doc *model.WorkDoc) error {
	p := tea.NewProgram(NewStatus(doc), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
