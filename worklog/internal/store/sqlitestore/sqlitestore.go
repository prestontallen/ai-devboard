// Package sqlitestore is the durable Store implementation: one SQLite
// file, WAL mode, busy_timeout, numbered user_version migrations, pure-Go
// driver (modernc — verified CGO_ENABLED=0 across all release targets).
// Behavioral semantics (validation, minting, dedupe, journaling) come
// from the store package's shared helpers so this implementation cannot
// drift from memstore.
package sqlitestore

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

//go:embed schema.sql
var migration1 string

//go:embed schema2.sql
var migration2 string

// migrations are applied in order inside one transaction each; index+1 is
// the resulting PRAGMA user_version.
var migrations = []string{migration1, migration2}

type SQLite struct {
	db *sql.DB
}

// Open opens (creating if needed) the store at path and migrates it to
// the current user_version. The concurrency contract: WAL so readers
// never block on the writer, busy_timeout so a second writer waits
// instead of erroring, foreign keys enforced, _txlock=immediate so a
// writer acquires the write lock at BEGIN rather than deferring it to
// the first write statement — deferred transactions race each other on
// the upgrade and surface as an immediate SQLITE_BUSY instead of
// blocking for busy_timeout (confirmed by adb-cutover's M1 load test:
// without this, 7 of 8 concurrent writers failed within 10ms).
func Open(path string) (*SQLite, error) {
	dsn := "file:" + path + "?_txlock=immediate&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// database/sql pooling would hand out connections that race DDL; the
	// single-writer discipline is part of the design.
	db.SetMaxOpenConns(1)
	s := &SQLite{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *SQLite) migrate() error {
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	for i := version; i < len(migrations); i++ {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLite) Close() error { return s.db.Close() }

func jstr(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

func junstr[T any](raw string, into *T) {
	if raw != "" {
		json.Unmarshal([]byte(raw), into)
	}
}

func (s *SQLite) PutTicket(t *store.Ticket) error {
	if err := store.ValidateTicket(t); err != nil {
		return err
	}
	if t.ID == "" {
		t.ID = store.NewID()
	}
	slug := store.NormalizeSlug(t.Slug)

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Alias reservation: a slug ever worn by another ticket is refused.
	if slug != "" {
		var owner string
		err = tx.QueryRow("SELECT ticket_id FROM slug_aliases WHERE slug = ?", slug).Scan(&owner)
		switch {
		case err == sql.ErrNoRows:
		case err != nil:
			return err
		case store.ID(owner) != t.ID:
			return fmt.Errorf("slug %q already used by another ticket (slugs are reserved across history)", slug)
		}
	}

	prev, err := s.ticketTx(tx, t.ID)
	if err != nil && !store.IsNotFound(err) {
		return err
	}
	if store.IsNotFound(err) {
		prev = nil
	}

	// Dedupe before minting so ranks stay contiguous on the surviving set.
	t.Decisions = store.DedupeDecisions(t.Decisions)
	store.MintSubItemIDs(t)

	scoutMode, scoutWhy, scoutWhen := "", "", ""
	if t.Scout != nil {
		scoutMode, scoutWhy, scoutWhen = t.Scout.Mode, t.Scout.Why, t.Scout.When
	}
	var parent any
	if t.ParentID != "" {
		parent = string(t.ParentID)
	}
	var slugCol any
	if slug != "" {
		slugCol = slug
	}
	_, err = tx.Exec(`
INSERT INTO tickets (id, slug, title, type, state, rank, roster_rank, section, parent_id, repo,
  tags, started, waiting_since, pr, source, files, acceptance, status,
  plan_text, archived, completed, summary, time_spent, archive_feedback,
  archive_month, board_tracked, board_archived, tier, complexity, phase,
  branch, session, repo_path, scout_mode, scout_why, scout_when,
  notes_preamble, extra, extra_fields)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET
  slug=excluded.slug, title=excluded.title, type=excluded.type,
  state=excluded.state, rank=excluded.rank, roster_rank=excluded.roster_rank,
  section=excluded.section, parent_id=excluded.parent_id,
  repo=excluded.repo, tags=excluded.tags, started=excluded.started,
  waiting_since=excluded.waiting_since, pr=excluded.pr, source=excluded.source,
  files=excluded.files, acceptance=excluded.acceptance, status=excluded.status,
  plan_text=excluded.plan_text, archived=excluded.archived,
  completed=excluded.completed, summary=excluded.summary,
  time_spent=excluded.time_spent, archive_feedback=excluded.archive_feedback,
  archive_month=excluded.archive_month, board_tracked=excluded.board_tracked,
  board_archived=excluded.board_archived, tier=excluded.tier,
  complexity=excluded.complexity, phase=excluded.phase, branch=excluded.branch,
  session=excluded.session, repo_path=excluded.repo_path,
  scout_mode=excluded.scout_mode, scout_why=excluded.scout_why,
  scout_when=excluded.scout_when, notes_preamble=excluded.notes_preamble,
  extra=excluded.extra, extra_fields=excluded.extra_fields`,
		string(t.ID), slugCol, t.Title, t.Type, t.State, t.Rank, t.RosterRank, t.Section, parent, t.Repo,
		jstr(t.Tags), t.Started, t.WaitingSince, t.PR, t.Source, jstr(t.Files),
		t.Acceptance, t.Status, t.PlanText, t.Archived, t.Completed, t.Summary,
		t.TimeSpent, jstr(t.ArchiveFeedback), t.ArchiveMonth, t.BoardTracked,
		t.BoardArchived, t.Tier, t.Complexity, t.Phase, t.Branch, t.Session,
		t.RepoPath, scoutMode, scoutWhy, scoutWhen, t.NotesPreamble,
		jstr(t.Extra), jstr(t.ExtraFields))
	if err != nil {
		return err
	}
	if slug != "" {
		if _, err := tx.Exec("INSERT OR IGNORE INTO slug_aliases (slug, ticket_id) VALUES (?, ?)",
			slug, string(t.ID)); err != nil {
			return err
		}
	}

	if err := s.replaceSubItems(tx, t); err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	for _, ch := range store.DiffScalars(prev, t) {
		if _, err := tx.Exec(
			"INSERT INTO journal (id, entity, field, old, new, at, actor) VALUES (?,?,?,?,?,?,?)",
			string(store.NewID()), string(t.ID), ch.Field, ch.Old, ch.New, now, ch.Actor); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLite) replaceSubItems(tx *sql.Tx, t *store.Ticket) error {
	id := string(t.ID)
	for _, table := range []string{"plan_steps", "score_items", "decisions",
		"code_refs", "needs_you", "waiting_on", "links", "phase_transitions", "note_entries"} {
		if _, err := tx.Exec("DELETE FROM "+table+" WHERE ticket_id = ?", id); err != nil {
			return err
		}
	}
	ins := func(q string, args ...any) error {
		_, err := tx.Exec(q, args...)
		return err
	}
	for _, p := range t.PlanSteps {
		if err := ins("INSERT INTO plan_steps (id, ticket_id, rank, text, state, extra) VALUES (?,?,?,?,?,?)",
			string(p.ID), id, p.Rank, p.Text, p.State, jstr(p.Extra)); err != nil {
			return err
		}
	}
	for _, c := range t.Scorecard {
		if err := ins("INSERT INTO score_items (id, ticket_id, rank, text, verify, status, extra) VALUES (?,?,?,?,?,?,?)",
			string(c.ID), id, c.Rank, c.Text, c.Verify, c.Status, jstr(c.Extra)); err != nil {
			return err
		}
	}
	for _, d := range t.Decisions {
		if err := ins("INSERT INTO decisions (id, ticket_id, rank, what, why, made_on, complexity, extra) VALUES (?,?,?,?,?,?,?,?)",
			string(d.ID), id, d.Rank, d.What, d.Why, d.When, d.Complexity, jstr(d.Extra)); err != nil {
			return err
		}
	}
	for _, c := range t.CodeRefs {
		if err := ins("INSERT INTO code_refs (id, ticket_id, rank, file, lines, lang, note, snippet, extra) VALUES (?,?,?,?,?,?,?,?,?)",
			string(c.ID), id, c.Rank, c.File, c.Lines, c.Lang, c.Note, c.Snippet, jstr(c.Extra)); err != nil {
			return err
		}
	}
	for _, n := range t.NeedsYou {
		if err := ins("INSERT INTO needs_you (id, ticket_id, rank, kind, text, detail, extra) VALUES (?,?,?,?,?,?,?)",
			string(n.ID), id, n.Rank, n.Type, n.Text, n.Detail, jstr(n.Extra)); err != nil {
			return err
		}
	}
	for _, w := range t.WaitingOn {
		if err := ins("INSERT INTO waiting_on (id, ticket_id, rank, text, who, asked, link, detail, extra) VALUES (?,?,?,?,?,?,?,?,?)",
			string(w.ID), id, w.Rank, w.Text, w.Who, w.Asked, w.Link, w.Detail, jstr(w.Extra)); err != nil {
			return err
		}
	}
	for _, l := range t.Links {
		if err := ins("INSERT INTO links (id, ticket_id, rank, kind, label, url, extra) VALUES (?,?,?,?,?,?,?)",
			string(l.ID), id, l.Rank, l.Kind, l.Label, l.URL, jstr(l.Extra)); err != nil {
			return err
		}
	}
	for _, p := range t.Transitions {
		if err := ins("INSERT INTO phase_transitions (id, ticket_id, rank, from_phase, to_phase, at, actor, note) VALUES (?,?,?,?,?,?,?,?)",
			string(p.ID), id, p.Rank, p.From, p.To, p.At, p.Actor, p.Note); err != nil {
			return err
		}
	}
	for _, n := range t.NoteEntries {
		if err := ins("INSERT INTO note_entries (id, ticket_id, rank, stamp, body) VALUES (?,?,?,?,?)",
			string(n.ID), id, n.Rank, n.Stamp, n.Body); err != nil {
			return err
		}
	}
	return nil
}

type queryer interface {
	QueryRow(query string, args ...any) *sql.Row
	Query(query string, args ...any) (*sql.Rows, error)
}

func (s *SQLite) Ticket(id store.ID) (*store.Ticket, error) {
	return s.ticketTx(s.db, id)
}

func (s *SQLite) TicketBySlug(slug string) (*store.Ticket, error) {
	var id string
	err := s.db.QueryRow("SELECT ticket_id FROM slug_aliases WHERE slug = ?",
		store.NormalizeSlug(slug)).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, store.NotFound("slug " + slug)
	}
	if err != nil {
		return nil, err
	}
	return s.ticketTx(s.db, store.ID(id))
}

func (s *SQLite) Tickets() ([]*store.Ticket, error) {
	// rank first, slug as the tiebreak: rank carries the human's document
	// order, and slug keeps the result total and deterministic for rows
	// that share one (quick-capture entities, anything never ranked).
	rows, err := s.db.Query("SELECT id FROM tickets ORDER BY rank, slug")
	if err != nil {
		return nil, err
	}
	ids, err := scanIDs(rows)
	if err != nil {
		return nil, err
	}
	return s.aggregates(ids)
}

func (s *SQLite) Children(parent store.ID) ([]*store.Ticket, error) {
	rows, err := s.db.Query("SELECT id FROM tickets WHERE parent_id = ? ORDER BY roster_rank, slug", string(parent))
	if err != nil {
		return nil, err
	}
	ids, err := scanIDs(rows)
	if err != nil {
		return nil, err
	}
	return s.aggregates(ids)
}

func scanIDs(rows *sql.Rows) ([]store.ID, error) {
	defer rows.Close()
	var ids []store.ID
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, store.ID(id))
	}
	return ids, rows.Err()
}

func (s *SQLite) aggregates(ids []store.ID) ([]*store.Ticket, error) {
	out := make([]*store.Ticket, 0, len(ids))
	for _, id := range ids {
		t, err := s.ticketTx(s.db, id)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

func (s *SQLite) ticketTx(q queryer, id store.ID) (*store.Ticket, error) {
	t := &store.Ticket{}
	var (
		idS, parent, slugCol      sql.NullString
		pr                        sql.NullString
		tags, files, afb          string
		extra, extraFields        string
		scoutMode, scoutWhy, when string
	)
	err := q.QueryRow(`
SELECT id, slug, title, type, state, rank, roster_rank, section, parent_id, repo, tags,
  started, waiting_since, pr, source, files, acceptance, status, plan_text,
  archived, completed, summary, time_spent, archive_feedback, archive_month,
  board_tracked, board_archived, tier, complexity, phase, branch, session,
  repo_path, scout_mode, scout_why, scout_when, notes_preamble, extra,
  extra_fields
FROM tickets WHERE id = ?`, string(id)).Scan(
		&idS, &slugCol, &t.Title, &t.Type, &t.State, &t.Rank, &t.RosterRank, &t.Section, &parent,
		&t.Repo, &tags, &t.Started, &t.WaitingSince, &pr, &t.Source, &files,
		&t.Acceptance, &t.Status, &t.PlanText, &t.Archived, &t.Completed,
		&t.Summary, &t.TimeSpent, &afb, &t.ArchiveMonth, &t.BoardTracked,
		&t.BoardArchived, &t.Tier, &t.Complexity, &t.Phase, &t.Branch,
		&t.Session, &t.RepoPath, &scoutMode, &scoutWhy, &when,
		&t.NotesPreamble, &extra, &extraFields)
	if err == sql.ErrNoRows {
		return nil, store.NotFound("ticket " + string(id))
	}
	if err != nil {
		return nil, err
	}
	t.ID = store.ID(idS.String)
	t.Slug = slugCol.String
	if parent.Valid {
		t.ParentID = store.ID(parent.String)
	}
	if pr.Valid {
		v := pr.String
		t.PR = &v
	}
	junstr(tags, &t.Tags)
	junstr(files, &t.Files)
	junstr(afb, &t.ArchiveFeedback)
	junstr(extra, &t.Extra)
	junstr(extraFields, &t.ExtraFields)
	if len(t.Extra) == 0 {
		t.Extra = nil
	}
	if len(t.ExtraFields) == 0 {
		t.ExtraFields = nil
	}
	if scoutMode != "" {
		t.Scout = &store.Scout{Mode: scoutMode, Why: scoutWhy, When: when}
	}
	return t, s.loadSubItems(q, t)
}

func (s *SQLite) loadSubItems(q queryer, t *store.Ticket) error {
	id := string(t.ID)
	each := func(query string, scan func(*sql.Rows) error) error {
		rows, err := q.Query(query, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if err := scan(rows); err != nil {
				return err
			}
		}
		return rows.Err()
	}

	if err := each("SELECT id, rank, text, state, extra FROM plan_steps WHERE ticket_id = ? ORDER BY rank", func(r *sql.Rows) error {
		var p store.PlanStep
		var pid, extra string
		if err := r.Scan(&pid, &p.Rank, &p.Text, &p.State, &extra); err != nil {
			return err
		}
		p.ID = store.ID(pid)
		junstr(extra, &p.Extra)
		if len(p.Extra) == 0 {
			p.Extra = nil
		}
		t.PlanSteps = append(t.PlanSteps, p)
		return nil
	}); err != nil {
		return err
	}
	if err := each("SELECT id, rank, text, verify, status, extra FROM score_items WHERE ticket_id = ? ORDER BY rank", func(r *sql.Rows) error {
		var c store.ScoreItem
		var cid, extra string
		if err := r.Scan(&cid, &c.Rank, &c.Text, &c.Verify, &c.Status, &extra); err != nil {
			return err
		}
		c.ID = store.ID(cid)
		junstr(extra, &c.Extra)
		if len(c.Extra) == 0 {
			c.Extra = nil
		}
		t.Scorecard = append(t.Scorecard, c)
		return nil
	}); err != nil {
		return err
	}
	if err := each("SELECT id, rank, what, why, made_on, complexity, extra FROM decisions WHERE ticket_id = ? ORDER BY rank", func(r *sql.Rows) error {
		var d store.Decision
		var did, extra string
		if err := r.Scan(&did, &d.Rank, &d.What, &d.Why, &d.When, &d.Complexity, &extra); err != nil {
			return err
		}
		d.ID = store.ID(did)
		junstr(extra, &d.Extra)
		if len(d.Extra) == 0 {
			d.Extra = nil
		}
		t.Decisions = append(t.Decisions, d)
		return nil
	}); err != nil {
		return err
	}
	if err := each("SELECT id, rank, file, lines, lang, note, snippet, extra FROM code_refs WHERE ticket_id = ? ORDER BY rank", func(r *sql.Rows) error {
		var c store.CodeRef
		var cid, extra string
		if err := r.Scan(&cid, &c.Rank, &c.File, &c.Lines, &c.Lang, &c.Note, &c.Snippet, &extra); err != nil {
			return err
		}
		c.ID = store.ID(cid)
		junstr(extra, &c.Extra)
		if len(c.Extra) == 0 {
			c.Extra = nil
		}
		t.CodeRefs = append(t.CodeRefs, c)
		return nil
	}); err != nil {
		return err
	}
	if err := each("SELECT id, rank, kind, text, detail, extra FROM needs_you WHERE ticket_id = ? ORDER BY rank", func(r *sql.Rows) error {
		var n store.NeedsItem
		var nid, extra string
		if err := r.Scan(&nid, &n.Rank, &n.Type, &n.Text, &n.Detail, &extra); err != nil {
			return err
		}
		n.ID = store.ID(nid)
		junstr(extra, &n.Extra)
		if len(n.Extra) == 0 {
			n.Extra = nil
		}
		t.NeedsYou = append(t.NeedsYou, n)
		return nil
	}); err != nil {
		return err
	}
	if err := each("SELECT id, rank, text, who, asked, link, detail, extra FROM waiting_on WHERE ticket_id = ? ORDER BY rank", func(r *sql.Rows) error {
		var w store.WaitingItem
		var wid, extra string
		if err := r.Scan(&wid, &w.Rank, &w.Text, &w.Who, &w.Asked, &w.Link, &w.Detail, &extra); err != nil {
			return err
		}
		w.ID = store.ID(wid)
		junstr(extra, &w.Extra)
		if len(w.Extra) == 0 {
			w.Extra = nil
		}
		t.WaitingOn = append(t.WaitingOn, w)
		return nil
	}); err != nil {
		return err
	}
	if err := each("SELECT id, rank, kind, label, url, extra FROM links WHERE ticket_id = ? ORDER BY rank", func(r *sql.Rows) error {
		var l store.Link
		var lid, extra string
		if err := r.Scan(&lid, &l.Rank, &l.Kind, &l.Label, &l.URL, &extra); err != nil {
			return err
		}
		l.ID = store.ID(lid)
		junstr(extra, &l.Extra)
		if len(l.Extra) == 0 {
			l.Extra = nil
		}
		t.Links = append(t.Links, l)
		return nil
	}); err != nil {
		return err
	}
	if err := each("SELECT id, rank, from_phase, to_phase, at, actor, note FROM phase_transitions WHERE ticket_id = ? ORDER BY rank", func(r *sql.Rows) error {
		var p store.PhaseTransition
		var pid string
		if err := r.Scan(&pid, &p.Rank, &p.From, &p.To, &p.At, &p.Actor, &p.Note); err != nil {
			return err
		}
		p.ID = store.ID(pid)
		t.Transitions = append(t.Transitions, p)
		return nil
	}); err != nil {
		return err
	}
	return each("SELECT id, rank, stamp, body FROM note_entries WHERE ticket_id = ? ORDER BY rank", func(r *sql.Rows) error {
		var n store.NoteEntry
		var nid string
		if err := r.Scan(&nid, &n.Rank, &n.Stamp, &n.Body); err != nil {
			return err
		}
		n.ID = store.ID(nid)
		t.NoteEntries = append(t.NoteEntries, n)
		return nil
	})
}

func (s *SQLite) PutFeedback(e *store.FeedbackEntry) error {
	if e.Signal == "" || e.Trigger == "" {
		return fmt.Errorf("feedback: signal and trigger are required")
	}
	if e.ID == "" {
		e.ID = store.NewID()
	}
	_, err := s.db.Exec(`
INSERT INTO feedback (id, seconds, signal, trigger, excerpt, context, resolved)
VALUES (?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET seconds=excluded.seconds, signal=excluded.signal,
  trigger=excluded.trigger, excerpt=excluded.excerpt, context=excluded.context,
  resolved=excluded.resolved`,
		string(e.ID), e.Seconds, e.Signal, e.Trigger, e.Excerpt, e.Context, e.Resolved)
	return err
}

func (s *SQLite) Feedback() ([]*store.FeedbackEntry, error) {
	rows, err := s.db.Query("SELECT id, seconds, signal, trigger, excerpt, context, resolved FROM feedback ORDER BY seconds, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*store.FeedbackEntry, 0)
	for rows.Next() {
		e := &store.FeedbackEntry{}
		var id string
		if err := rows.Scan(&id, &e.Seconds, &e.Signal, &e.Trigger, &e.Excerpt, &e.Context, &e.Resolved); err != nil {
			return nil, err
		}
		e.ID = store.ID(id)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *SQLite) Journal(entity store.ID) ([]store.FieldChange, error) {
	rows, err := s.db.Query("SELECT id, entity, field, old, new, at, actor FROM journal WHERE entity = ? ORDER BY at, id", string(entity))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.FieldChange
	for rows.Next() {
		var ch store.FieldChange
		var id, ent string
		if err := rows.Scan(&id, &ent, &ch.Field, &ch.Old, &ch.New, &ch.At, &ch.Actor); err != nil {
			return nil, err
		}
		ch.ID, ch.Entity = store.ID(id), store.ID(ent)
		out = append(out, ch)
	}
	return out, rows.Err()
}

var _ store.Store = (*SQLite)(nil)
