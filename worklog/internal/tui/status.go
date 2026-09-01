// Package tui hosts the Bubble Tea views for the worklog CLI.
package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"github.com/prestontallen/day2day/internal/model"
	"github.com/prestontallen/day2day/internal/note"
	"github.com/prestontallen/day2day/internal/parse"
	"github.com/prestontallen/day2day/internal/pr"
	"github.com/prestontallen/day2day/internal/style"
	"github.com/prestontallen/day2day/internal/wait"
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
	Next             key.Binding
	Prev             key.Binding
	Quit             key.Binding
	EditPR           key.Binding
	EditNotes        key.Binding
	MoveWait         key.Binding
	OpenSearch       key.Binding
	OpenSearchCorpus key.Binding
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
		EditPR: key.NewBinding(
			key.WithKeys("p"),
			key.WithHelp("p", "edit PR"),
		),
		EditNotes: key.NewBinding(
			key.WithKeys("n"),
			key.WithHelp("n", "add note"),
		),
		MoveWait: key.NewBinding(
			key.WithKeys("w"),
			key.WithHelp("w", "park to waiting"),
		),
		OpenSearch: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "search"),
		),
		OpenSearchCorpus: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "search all"),
		),
	}
}

func (k keymap) ShortHelp() []key.Binding {
	return []key.Binding{k.Next, k.Prev, k.EditPR, k.EditNotes, k.MoveWait, k.OpenSearch, k.OpenSearchCorpus, k.Quit}
}

func (k keymap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Next, k.Prev, k.EditPR, k.EditNotes, k.MoveWait},
		{k.OpenSearch, k.OpenSearchCorpus, k.Quit},
	}
}

// viewMode tracks the active overlay / mode.
type viewMode int

const (
	modeList viewMode = iota
	modeEditPR
	modeAddNote
	modeSearch
)

// prWriter is the function the TUI uses to persist a new PR value. Tests can
// swap it in to avoid touching the filesystem.
type prWriter func(id, value string) (pr.Result, error)

// noteAppender is the function the TUI uses to append a note entry. Tests can
// swap it in to avoid touching the filesystem.
type noteAppender func(id, body string) error

// waitMover is the function the TUI uses to park a Now ticket into Waiting.
// Tests can swap it in to avoid touching the filesystem.
type waitMover func(id string) error

// Status is the top-level Bubble Tea model.
type Status struct {
	wd       model.Workdir
	doc      *model.WorkDoc
	sections []sectionView
	active   int
	keys     keymap
	help     help.Model
	width    int
	height   int

	mode      viewMode
	prForm    *huh.Form
	prTarget  string // block ID currently being edited
	prValue   string // bound value backing the huh input
	prStatus  string // last-write status line shown below the list

	noteForm   *huh.Form
	noteTarget string // block ID receiving the new note
	noteValue  string // bound value backing the huh text input
	noteStatus string // last-write status line shown below the list

	waitStatus string // last park-to-waiting status line

	searchOverlay *searchOverlay
	searchStatus  string // last search-jump or corpus-hit message

	writePR    prWriter
	appendNote noteAppender
	moveToWait waitMover
}

type sectionView struct {
	name model.SectionName
	list list.Model
}

// NewStatus builds the model from a parsed doc. The writer is the function
// called on PR-edit submit; default is the real on-disk writer.
func NewStatus(wd model.Workdir, doc *model.WorkDoc) *Status {
	return newStatusWithWriter(wd, doc, func(id, value string) (pr.Result, error) {
		return pr.SetPR(wd, id, value)
	})
}

// startNoteAdd transitions into modeAddNote with a fresh Huh multi-line form.
func (s *Status) startNoteAdd(b *model.Block) tea.Cmd {
	s.mode = modeAddNote
	s.noteTarget = b.ID
	s.noteValue = ""
	s.noteForm = huh.NewForm(
		huh.NewGroup(
			huh.NewText().
				Title("New note for "+strings.ToUpper(b.ID)).
				Description("Markdown body. Submit (Ctrl+D) appends; Esc cancels.").
				Value(&s.noteValue),
		),
	)
	return s.noteForm.Init()
}

// submitNote persists the note via appendNote and re-parses WORK.md.
func (s *Status) submitNote() {
	if strings.TrimSpace(s.noteValue) == "" {
		s.noteStatus = "note discarded (empty body)"
		return
	}
	if err := s.appendNote(s.noteTarget, s.noteValue); err != nil {
		s.noteStatus = "error: " + err.Error()
		return
	}
	s.noteStatus = fmt.Sprintf("note appended to %s", strings.ToUpper(s.noteTarget))
	if doc, err := parse.File(s.wd.WorkMD()); err == nil {
		s.doc = doc
		s.reloadSections()
	}
}

func newStatusWithWriter(wd model.Workdir, doc *model.WorkDoc, w prWriter) *Status {
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

	s := &Status{
		wd:  wd,
		doc: doc,
		sections: []sectionView{
			mkSection(model.SectionNow),
			mkSection(model.SectionWaiting),
			mkSection(model.SectionNext),
			mkSection(model.SectionSomeday),
		},
		keys:    keys,
		help:    help.New(),
		writePR: w,
	}
	s.appendNote = func(id, body string) error {
		_, err := note.Append(wd, id, body, time.Now())
		return err
	}
	s.moveToWait = func(id string) error {
		today := time.Now().Format("2006-01-02")
		_, err := wait.Wait(wd, id, today)
		return err
	}
	return s
}

func (s *Status) Init() tea.Cmd { return nil }

// allLiveBlocks returns all blocks from all sections in display order.
func (s *Status) allLiveBlocks() []model.Block {
	var blocks []model.Block
	for _, secName := range []model.SectionName{
		model.SectionNow, model.SectionWaiting, model.SectionNext, model.SectionSomeday,
	} {
		if sec := s.doc.Section(secName); sec != nil {
			blocks = append(blocks, sec.Blocks...)
		}
	}
	return blocks
}

// jumpToResult navigates to the block identified by res. For corpus hits not
// in the live view, it stores the snippet in searchStatus for the detail pane.
func (s *Status) jumpToResult(res *searchResult) {
	if res == nil {
		return
	}
	if res.BlockRef == nil {
		s.searchStatus = fmt.Sprintf("archive: %s in %s", res.Anchor, res.File)
		return
	}
	for i, sec := range s.sections {
		for j, item := range sec.list.Items() {
			bi, ok := item.(blockItem)
			if ok && bi.block.ID == res.BlockRef.ID {
				s.active = i
				s.sections[i].list.Select(j)
				return
			}
		}
	}
}

// selectedBlock returns a pointer to the currently-selected block, or nil if
// no item is selected (empty section).
func (s *Status) selectedBlock() *model.Block {
	sec := s.sections[s.active]
	item, ok := sec.list.SelectedItem().(blockItem)
	if !ok {
		return nil
	}
	return &item.block
}

// startPREdit transitions into modeEditPR with a fresh Huh form pre-populated
// with the block's current PR value.
func (s *Status) startPREdit(b *model.Block) tea.Cmd {
	s.mode = modeEditPR
	s.prTarget = b.ID
	s.prValue = b.PR
	s.prForm = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("PR URL").
				Description("for " + strings.ToUpper(b.ID)).
				Value(&s.prValue),
		),
	)
	return s.prForm.Init()
}

// submitPR persists the edited value via writePR and re-parses WORK.md so the
// list reflects the new value.
func (s *Status) submitPR() {
	res, err := s.writePR(s.prTarget, s.prValue)
	if err != nil {
		s.prStatus = "error: " + err.Error()
		return
	}
	s.prStatus = fmt.Sprintf("PR for %s set to %q (was %q)",
		strings.ToUpper(res.ID), res.PR, res.Previous)
	// Reload the doc so the detail pane reflects the new value.
	if doc, err := parse.File(s.wd.WorkMD()); err == nil {
		s.doc = doc
		s.reloadSections()
	}
}

func (s *Status) reloadSections() {
	for i := range s.sections {
		sec := s.doc.Section(s.sections[i].name)
		var items []list.Item
		if sec != nil {
			for _, b := range sec.Blocks {
				items = append(items, blockItem{block: b})
			}
		}
		s.sections[i].list.SetItems(items)
	}
}

func (s *Status) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = msg.Width, msg.Height
		// Reserve lines for header (1), spacer (1), detail pane (3), footer help (1-2).
		listHeight := msg.Height - 9
		if listHeight < 5 {
			listHeight = 5
		}
		for i := range s.sections {
			s.sections[i].list.SetSize(msg.Width-2, listHeight)
		}
	case tea.KeyMsg:
		if s.mode == modeEditPR {
			// Esc cancels; submit (form returns Completed) writes via writePR.
			if msg.String() == "esc" {
				s.mode = modeList
				s.prForm = nil
				return s, nil
			}
			form, cmd := s.prForm.Update(msg)
			if f, ok := form.(*huh.Form); ok {
				s.prForm = f
			}
			if s.prForm.State == huh.StateCompleted {
				s.submitPR()
				s.mode = modeList
				s.prForm = nil
			}
			return s, cmd
		}

		if s.mode == modeAddNote {
			// Esc cancels; submit (form returns Completed) appends via appendNote.
			if msg.String() == "esc" {
				s.mode = modeList
				s.noteForm = nil
				return s, nil
			}
			form, cmd := s.noteForm.Update(msg)
			if f, ok := form.(*huh.Form); ok {
				s.noteForm = f
			}
			if s.noteForm.State == huh.StateCompleted {
				s.submitNote()
				s.mode = modeList
				s.noteForm = nil
			}
			return s, cmd
		}

		if s.mode == modeSearch {
			if msg.String() == "esc" {
				s.searchOverlay = nil
				s.mode = modeList
				return s, nil
			}
			cmd, done := s.searchOverlay.update(msg, s.allLiveBlocks(), s.wd)
			if done {
				s.jumpToResult(s.searchOverlay.currentSelection())
				s.searchOverlay = nil
				s.mode = modeList
				return s, nil
			}
			return s, cmd
		}

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
		case key.Matches(msg, s.keys.EditPR):
			if b := s.selectedBlock(); b != nil && b.ID != "" {
				return s, s.startPREdit(b)
			}
			return s, nil
		case key.Matches(msg, s.keys.EditNotes):
			if b := s.selectedBlock(); b != nil && b.ID != "" {
				return s, s.startNoteAdd(b)
			}
			return s, nil
		case key.Matches(msg, s.keys.MoveWait):
			if b := s.selectedBlock(); b != nil && b.ID != "" {
				if err := s.moveToWait(b.ID); err != nil {
					s.waitStatus = "error: " + err.Error()
				} else {
					s.waitStatus = fmt.Sprintf("%s parked to ## Waiting", strings.ToUpper(b.ID))
					if doc, err := parse.File(s.wd.WorkMD()); err == nil {
						s.doc = doc
						s.reloadSections()
					}
				}
			}
			return s, nil
		case key.Matches(msg, s.keys.OpenSearch):
			s.searchOverlay = newSearchOverlay(scopeInMemory)
			s.searchOverlay.width = s.width
			s.searchOverlay.height = s.height
			s.mode = modeSearch
			return s, textinput.Blink
		case key.Matches(msg, s.keys.OpenSearchCorpus):
			s.searchOverlay = newSearchOverlay(scopeCorpus)
			s.searchOverlay.width = s.width
			s.searchOverlay.height = s.height
			s.mode = modeSearch
			return s, textinput.Blink
		}
	}

	if s.mode == modeEditPR && s.prForm != nil {
		form, cmd := s.prForm.Update(msg)
		if f, ok := form.(*huh.Form); ok {
			s.prForm = f
		}
		return s, cmd
	}

	if s.mode == modeAddNote && s.noteForm != nil {
		form, cmd := s.noteForm.Update(msg)
		if f, ok := form.(*huh.Form); ok {
			s.noteForm = f
		}
		return s, cmd
	}

	var cmd tea.Cmd
	s.sections[s.active].list, cmd = s.sections[s.active].list.Update(msg)
	return s, cmd
}

// detailPane renders a small block of metadata for the currently-selected
// item, including the PR field (always shown — em-dash when empty).
func (s *Status) detailPane() string {
	b := s.selectedBlock()
	if b == nil {
		return style.Dim.Render("(no item selected)")
	}
	prText := b.PR
	if prText == "" {
		prText = "—"
	}
	lines := []string{
		style.SubHeading.Render(strings.ToUpper(b.ID) + " — " + b.Title),
		"PR: " + prText,
	}
	if s.prStatus != "" {
		lines = append(lines, style.Dim.Render(s.prStatus))
	}
	if s.noteStatus != "" {
		lines = append(lines, style.Dim.Render(s.noteStatus))
	}
	if s.waitStatus != "" {
		lines = append(lines, style.Dim.Render(s.waitStatus))
	}
	if s.searchStatus != "" {
		lines = append(lines, style.Dim.Render(s.searchStatus))
	}
	return strings.Join(lines, "\n")
}

func (s *Status) View() string {
	if s.width == 0 {
		return "" // wait for the initial WindowSizeMsg
	}

	if s.mode == modeEditPR && s.prForm != nil {
		return s.prForm.View()
	}
	if s.mode == modeAddNote && s.noteForm != nil {
		return s.noteForm.View()
	}
	if s.mode == modeSearch && s.searchOverlay != nil {
		return lipgloss.Place(s.width, s.height, lipgloss.Center, lipgloss.Center,
			s.searchOverlay.view())
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
	detail := s.detailPane()
	footer := s.help.View(s.keys)

	return lipgloss.JoinVertical(lipgloss.Left, header, body, detail, footer)
}

// Run launches the Bubble Tea program in alt-screen mode.
func Run(wd model.Workdir, doc *model.WorkDoc) error {
	p := tea.NewProgram(NewStatus(wd, doc), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
