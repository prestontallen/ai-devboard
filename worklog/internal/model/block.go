package model

import "strings"

// State of a top-level worklog item.
type State string

const (
	StatePending State = " "
	StateActive  State = "~"
	StateDone    State = "x"
)

// Label returns the human/JSON-friendly representation of a State:
// "pending" | "active" | "done". Unknown values map to "pending".
func (s State) Label() string {
	switch s {
	case StateActive:
		return "active"
	case StateDone:
		return "done"
	default:
		return "pending"
	}
}

// BlockType distinguishes epic containers from ticket-like blocks.
type BlockType string

const (
	TypeTicket  BlockType = "ticket"
	TypeEpic    BlockType = "epic"
	TypeSpike   BlockType = "spike"
	TypeChore   BlockType = "chore"
	TypeUnknown BlockType = ""
)

// SectionName is one of the canonical WORK.md sections.
type SectionName string

const (
	SectionNow     SectionName = "Now"
	SectionNext    SectionName = "Next"
	SectionSomeday SectionName = "Someday"
	SectionBlocked SectionName = "Blocked"
	SectionWaiting SectionName = "Waiting"
)

// Block is one top-level `- [ ] ...` item plus its indented metadata.
// Line numbers are 1-indexed and inclusive of the block's metadata range.
//
// JSON tags are present on metadata fields; line numbers, section back-
// pointer, and raw state are omitted from JSON because CLI wire types
// translate them into more friendly shapes (e.g. State -> "pending"/"active"/"done").
type Block struct {
	StartLine int `json:"-"`
	EndLine   int `json:"-"`

	Section SectionName `json:"-"`
	State   State       `json:"-"`
	Title   string      `json:"title"`

	ID             string      `json:"id"`
	Type           BlockType   `json:"type"`
	Parent         string      `json:"parent"`
	Repo           string      `json:"repo"`
	Tags           []string    `json:"tags"`
	Started        string      `json:"started"`
	PR             string      `json:"pr"`
	Source         string      `json:"source"`
	WaitingSince   string      `json:"waitingSince"`
	Files          []string    `json:"files"`
	Acceptance     string      `json:"acceptance"`
	NotesRef       string      `json:"notesRef"`
	Status         string      `json:"status"`
	Plan           string      `json:"plan"`
	ActiveChildren []string    `json:"activeChildren"`
	Links          []LinkEntry `json:"links"`
}

// LinkEntry is one `**Link**: <name> — <url>` metadata line. Unlike every
// other field on Block, Link is repeatable — a block may carry any number.
type LinkEntry struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// IsEpic reports whether the block is an epic container.
func (b Block) IsEpic() bool { return b.Type == TypeEpic }

// IsActive reports whether the block is in-progress (`[~]`).
func (b Block) IsActive() bool { return b.State == StateActive }

// IsDone reports whether the block is complete (`[x]`).
func (b Block) IsDone() bool { return b.State == StateDone }

// LinkByName returns the block's link with the given name (case-
// insensitive) and whether one was found.
func (b Block) LinkByName(name string) (LinkEntry, bool) {
	for _, l := range b.Links {
		if strings.EqualFold(l.Name, name) {
			return l, true
		}
	}
	return LinkEntry{}, false
}
