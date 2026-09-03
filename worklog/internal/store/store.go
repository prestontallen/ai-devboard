// Package store defines the identity-first data model for the worklog
// rewrite (epic adb-worklog-rewrite) and the Store interface every
// consumer programs against. Implementations are swappable by design:
// sqlitestore is the durable one, memstore proves the boundary holds.
//
// Modeling rules (ratified 2026-09-02, contract adb-schema-design):
//   - Every entity and sub-item carries a ULID. Position is never identity;
//     ordering is the Rank field.
//   - The slug is a mutable alias, unique case-insensitively across all
//     history (archived slugs stay reserved).
//   - Unknown data survives: YAML unknown keys land in Extra, unknown
//     WORK.md field bullets in ExtraFields, both round-tripped verbatim.
//   - Absent and empty differ where today's files distinguish them; PR is
//     the load-bearing case (a "**PR**: " line with no value is not the
//     same as no line).
//   - Bare devboard files (no worklog join) are NOT canon: they remain
//     producer-owned sources on disk that renderers never touch.
package store

// ID is a ULID in its 26-char Crockford base32 form.
type ID string

// Enumerations. Stored as strings; constraints live in the
// implementations (CHECK constraints in SQLite, validation in memstore).
const (
	TypeTicket = "ticket"
	TypeEpic   = "epic"
	TypeSpike  = "spike"
	TypeChore  = "chore"

	// State: pending ([ ]) and active ([~]) are both OPEN — epic
	// completability counts only done ([x]). (done.go:222 behavior.)
	StatePending = "pending"
	StateActive  = "active"
	StateDone    = "done"

	SectionNow     = "now"
	SectionWaiting = "waiting"
	SectionNext    = "next"
	SectionSomeday = "someday"
	SectionBlocked = "blocked"

	// Link kinds: the typed relation that makes the PR/label collision
	// unconstructable. One pr-kind link per ticket (enforced).
	LinkPR  = "pr"
	LinkRef = "ref"
)

// Phases is the single canonical phase vocabulary (the implement→
// implementing alias is retired at cutover; converters normalize).
var Phases = []string{
	"intake", "clarify", "research", "contract", "plan",
	"implementing", "verify", "present", "ship", "done",
}

// Ticket is the aggregate root. One worklog entity — live or archived,
// standalone or epic child — with every sub-item it owns. Get/Put move
// the whole aggregate; PutTicket is atomic, which is what makes the
// waiting-on→decision lifecycle rule (resolution and its decision record
// land together or not at all) expressible as a single call.
type Ticket struct {
	ID    ID
	Slug  string // lowercase; unique NOCASE across history
	Title string
	Type  string // ticket|epic|spike|chore
	State string // pending|active|done

	// Rank is the ticket's position in whichever document renders it —
	// WORK.md while live, the archive month file once Archived (a ticket is
	// in exactly one of the two). This is the modeling rule at the top of
	// this file applied to the ticket itself: the human's ordering is real
	// data (## Next is a hand-ordered priority queue), so it is a field,
	// not the position of a line in a file. Without it the only available
	// sort is by slug, which silently alphabetizes the backlog.
	Rank int

	// RosterRank is this ticket's position in its PARENT's child roster —
	// the order children were added to the epic, held by the epic's
	// `## Children` list while live and by its archived `Children:` CSV
	// after. Distinct from Rank: a child's Rank is where it sits in
	// WORK.md or an archive month, which says nothing about its place in
	// the roster. Meaningless when ParentID is "".
	RosterRank int

	// WORK.md-side fields.
	Section      string // now|waiting|next|someday|blocked; "" once archived
	ParentID     ID     // epic parent ("" = none)
	Repo         string // canonical attribution: the ticket field wins (D5)
	Tags         []string
	Started      string  // YYYY-MM-DD, "" = never started
	WaitingSince string  // YYYY-MM-DD
	PR           *string // nil = no PR line; "" = present-but-empty line
	Source       string
	Files        []string
	Acceptance   string
	Status       string // free text
	PlanText     string // WORK.md's **Plan** field — a STRING, distinct from PlanSteps

	// Archive-record fields (populated once Archived).
	Archived        bool
	Completed       string // YYYY-MM-DD
	Summary         string
	TimeSpent       string // free text, e.g. "~2h"
	ArchiveFeedback []string
	ArchiveMonth    string // YYYY-MM: which archive file renders this entry

	// Devboard in-flight fields (sole-source today, canon now).
	BoardTracked  bool // renders a devboard projection file
	BoardArchived bool // the board's _archive/ state — entity state, not file location
	Tier          int  // 0 = unset
	Complexity    string
	Phase         string // current phase; Transitions holds history
	Branch        string
	Session       string
	RepoPath      string
	Scout         *Scout

	// Notes file: preamble (title line, scaffold comment, Background
	// prose) verbatim; entries in NoteEntries.
	NotesPreamble string

	// Unknown-data passthrough.
	Extra       map[string]any    // devboard YAML unknown keys
	ExtraFields map[string]string // WORK.md unknown `- **Label**: value` bullets

	// Sub-items, each ULID-identified, rank-ordered.
	PlanSteps   []PlanStep
	Scorecard   []ScoreItem
	Decisions   []Decision
	CodeRefs    []CodeRef
	NeedsYou    []NeedsItem
	WaitingOn   []WaitingItem
	Links       []Link
	Transitions []PhaseTransition
	NoteEntries []NoteEntry
}

// Open reports whether the ticket counts as open for epic completability.
func (t *Ticket) Open() bool { return t.State != StateDone }

type PlanStep struct {
	ID    ID
	Rank  int
	Text  string
	State string // pending|in_progress|done|blocked
	Extra map[string]any
}

type ScoreItem struct {
	ID     ID
	Rank   int
	Text   string
	Verify string
	Status string // pending|pass|fail
	Extra  map[string]any
}

// Decision entries form a set keyed on (What, Why): PutTicket dedupes
// exact duplicates (adb-decision-dedupe absorbed here).
type Decision struct {
	ID         ID
	Rank       int
	What       string
	Why        string
	When       string // YYYY-MM-DD
	Complexity string // free text today ("medium → high", "medium (first rating)")
	Extra      map[string]any
}

type CodeRef struct {
	ID      ID
	Rank    int
	File    string
	Lines   string
	Lang    string
	Note    string
	Snippet string
	Extra   map[string]any
}

type NeedsItem struct {
	ID     ID
	Rank   int
	Type   string // question|checkpoint
	Text   string
	Detail string
	Extra  map[string]any
}

type WaitingItem struct {
	ID     ID
	Rank   int
	Text   string
	Who    string
	Asked  string // YYYY-MM-DD
	Link   string
	Detail string
	Extra  map[string]any
}

type Link struct {
	ID    ID
	Rank  int
	Kind  string // pr|ref
	Label string
	URL   string
	Extra map[string]any
}

// Scout is the risk-scout attestation.
type Scout struct {
	Mode string // ran|inline|skipped
	Why  string
	When string
}

// PhaseTransition is one row of phase history. Ticket.Phase stays the
// authoritative current value (history starts accruing at cutover; the
// converter does not fabricate a past).
type PhaseTransition struct {
	ID    ID
	Rank  int
	From  string
	To    string
	At    string // RFC3339
	Actor string
	Note  string
}

// NoteEntry is one timestamped journal segment, split per the ratified
// rule: only `## YYYY-MM-DD HH:MM` lines are boundaries; duplicate
// stamps are legal; Body is verbatim, owned by the human.
type NoteEntry struct {
	ID    ID
	Rank  int
	Stamp string // "YYYY-MM-DD HH:MM" as written
	Body  string // verbatim, includes any content headings
}

// FeedbackEntry is one friction-log record (canon per ratified OQ4).
// Seconds stays the stable alias: it is the `feedback resolve` handle and
// the frozen JSON timestamp; same-second entries are distinct by ID.
type FeedbackEntry struct {
	ID       ID
	Seconds  int64
	Signal   string
	Trigger  string
	Excerpt  string
	Context  string
	Resolved int64 // unix seconds, 0 = open
}

// FieldChange is one journal row, written by PutTicket in the same
// transaction as the change it records.
type FieldChange struct {
	ID     ID
	Entity ID
	Field  string
	Old    string
	New    string
	At     string // RFC3339
	Actor  string
}

// Store is the storage boundary. Every consumer — converter, renderers,
// oracle tests, and eventually the CLI verbs and server — programs
// against this interface only; nothing outside an implementation package
// may import the implementation. Implementations must be swappable
// without touching callers.
type Store interface {
	// PutTicket upserts the whole aggregate atomically, journaling
	// scalar-field diffs against the prior version in the same
	// transaction and deduping Decisions on (What, Why). New sub-items
	// without an ID get one minted; existing IDs are preserved (item
	// removal never renumbers survivors).
	PutTicket(t *Ticket) error

	// Ticket fetches an aggregate by ULID.
	Ticket(id ID) (*Ticket, error)

	// TicketBySlug resolves the alias, case-insensitively. Slugs stay
	// the primary CLI resolution surface forever; ULIDs are additional.
	TicketBySlug(slug string) (*Ticket, error)

	// Tickets returns every aggregate, ordered by slug.
	Tickets() ([]*Ticket, error)

	// Children returns an epic's children ordered by rank of insertion
	// (stable), the single source the four legacy places render from.
	Children(parent ID) ([]*Ticket, error)

	// PutFeedback upserts one friction entry.
	PutFeedback(e *FeedbackEntry) error

	// Feedback returns all entries oldest-first.
	Feedback() ([]*FeedbackEntry, error)

	// Journal returns an entity's field-change history oldest-first.
	Journal(entity ID) ([]FieldChange, error)

	Close() error
}

// ErrNotFound is returned by lookups that miss; sentinel shared by all
// implementations so callers never type-switch on implementation errors.
type notFound struct{ what string }

func (e *notFound) Error() string { return e.what + " not found" }

// NotFound constructs the shared miss error.
func NotFound(what string) error { return &notFound{what: what} }

// IsNotFound reports whether err is a lookup miss.
func IsNotFound(err error) bool {
	_, ok := err.(*notFound)
	return ok
}
