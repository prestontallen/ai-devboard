-- Migration 1: the identity-first worklog schema.
-- Ratified constraints (contract adb-schema-design, 2026-09-02):
-- ULID PKs everywhere; slug UNIQUE COLLATE NOCASE across all history;
-- rank column for order, never position-as-identity; typed links with a
-- unique pr relation; phase_transitions + journal; extras JSON on every
-- entity and sub-item; absent != empty for pr (NULL vs '').

CREATE TABLE tickets (
    id             TEXT PRIMARY KEY,
    slug           TEXT UNIQUE COLLATE NOCASE, -- NULL for title-only quick-capture entities
    title          TEXT NOT NULL DEFAULT '',
    type           TEXT NOT NULL CHECK (type IN ('ticket','epic','spike','chore')),
    state          TEXT NOT NULL CHECK (state IN ('pending','active','done')),
    section        TEXT NOT NULL DEFAULT ''
                   CHECK (section IN ('','now','waiting','next','someday','blocked')),
    parent_id      TEXT REFERENCES tickets(id),
    repo           TEXT NOT NULL DEFAULT '',
    tags           TEXT NOT NULL DEFAULT '[]',
    started        TEXT NOT NULL DEFAULT '',
    waiting_since  TEXT NOT NULL DEFAULT '',
    pr             TEXT,
    source         TEXT NOT NULL DEFAULT '',
    files          TEXT NOT NULL DEFAULT '[]',
    acceptance     TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL DEFAULT '',
    plan_text      TEXT NOT NULL DEFAULT '',
    archived       INTEGER NOT NULL DEFAULT 0,
    completed      TEXT NOT NULL DEFAULT '',
    summary        TEXT NOT NULL DEFAULT '',
    time_spent     TEXT NOT NULL DEFAULT '',
    archive_feedback TEXT NOT NULL DEFAULT '[]',
    archive_month  TEXT NOT NULL DEFAULT '',
    board_tracked  INTEGER NOT NULL DEFAULT 0,
    board_archived INTEGER NOT NULL DEFAULT 0,
    tier           INTEGER NOT NULL DEFAULT 0,
    complexity     TEXT NOT NULL DEFAULT '' CHECK (complexity IN ('','low','medium','high')),
    phase          TEXT NOT NULL DEFAULT '',
    branch         TEXT NOT NULL DEFAULT '',
    session        TEXT NOT NULL DEFAULT '',
    repo_path      TEXT NOT NULL DEFAULT '',
    scout_mode     TEXT NOT NULL DEFAULT '' CHECK (scout_mode IN ('','ran','inline','skipped')),
    scout_why      TEXT NOT NULL DEFAULT '',
    scout_when     TEXT NOT NULL DEFAULT '',
    notes_preamble TEXT NOT NULL DEFAULT '',
    extra          TEXT NOT NULL DEFAULT '{}',
    extra_fields   TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX ix_tickets_parent  ON tickets(parent_id) WHERE parent_id IS NOT NULL;
CREATE INDEX ix_tickets_section ON tickets(section) WHERE section != '';

-- Every slug ever worn, forever: renames keep the old alias resolving,
-- and no ticket may claim an alias another ticket ever held.
CREATE TABLE slug_aliases (
    slug      TEXT PRIMARY KEY COLLATE NOCASE,
    ticket_id TEXT NOT NULL REFERENCES tickets(id)
);

CREATE TABLE plan_steps (
    id        TEXT PRIMARY KEY,
    ticket_id TEXT NOT NULL REFERENCES tickets(id),
    rank      INTEGER NOT NULL,
    text      TEXT NOT NULL DEFAULT '',
    state     TEXT NOT NULL DEFAULT ''
              CHECK (state IN ('','pending','in_progress','done','blocked')),
    extra     TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX ix_plan_ticket ON plan_steps(ticket_id, rank);

CREATE TABLE score_items (
    id        TEXT PRIMARY KEY,
    ticket_id TEXT NOT NULL REFERENCES tickets(id),
    rank      INTEGER NOT NULL,
    text      TEXT NOT NULL DEFAULT '',
    verify    TEXT NOT NULL DEFAULT '',
    status    TEXT NOT NULL DEFAULT '' CHECK (status IN ('','pending','pass','fail')),
    extra     TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX ix_score_ticket ON score_items(ticket_id, rank);

CREATE TABLE decisions (
    id         TEXT PRIMARY KEY,
    ticket_id  TEXT NOT NULL REFERENCES tickets(id),
    rank       INTEGER NOT NULL,
    what       TEXT NOT NULL,
    why        TEXT NOT NULL DEFAULT '',
    made_on    TEXT NOT NULL DEFAULT '',
    complexity TEXT NOT NULL DEFAULT '',
    extra      TEXT NOT NULL DEFAULT '{}',
    UNIQUE (ticket_id, what, why)
);

CREATE TABLE code_refs (
    id        TEXT PRIMARY KEY,
    ticket_id TEXT NOT NULL REFERENCES tickets(id),
    rank      INTEGER NOT NULL,
    file      TEXT NOT NULL DEFAULT '',
    lines     TEXT NOT NULL DEFAULT '',
    lang      TEXT NOT NULL DEFAULT '',
    note      TEXT NOT NULL DEFAULT '',
    snippet   TEXT NOT NULL DEFAULT '',
    extra     TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE needs_you (
    id        TEXT PRIMARY KEY,
    ticket_id TEXT NOT NULL REFERENCES tickets(id),
    rank      INTEGER NOT NULL,
    kind      TEXT NOT NULL CHECK (kind IN ('question','checkpoint')),
    text      TEXT NOT NULL DEFAULT '',
    detail    TEXT NOT NULL DEFAULT '',
    extra     TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE waiting_on (
    id        TEXT PRIMARY KEY,
    ticket_id TEXT NOT NULL REFERENCES tickets(id),
    rank      INTEGER NOT NULL,
    text      TEXT NOT NULL DEFAULT '',
    who       TEXT NOT NULL DEFAULT '',
    asked     TEXT NOT NULL DEFAULT '',
    link      TEXT NOT NULL DEFAULT '',
    detail    TEXT NOT NULL DEFAULT '',
    extra     TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE links (
    id        TEXT PRIMARY KEY,
    ticket_id TEXT NOT NULL REFERENCES tickets(id),
    rank      INTEGER NOT NULL,
    kind      TEXT NOT NULL CHECK (kind IN ('pr','ref')),
    label     TEXT NOT NULL DEFAULT '',
    url       TEXT NOT NULL DEFAULT '',
    extra     TEXT NOT NULL DEFAULT '{}'
);
-- The PR relation is unique by construction: the collision the old label
-- namespace allowed cannot be typed.
CREATE UNIQUE INDEX ux_links_pr ON links(ticket_id) WHERE kind = 'pr';

CREATE TABLE phase_transitions (
    id        TEXT PRIMARY KEY,
    ticket_id TEXT NOT NULL REFERENCES tickets(id),
    rank      INTEGER NOT NULL,
    from_phase TEXT NOT NULL DEFAULT '',
    to_phase   TEXT NOT NULL DEFAULT '',
    at        TEXT NOT NULL DEFAULT '',
    actor     TEXT NOT NULL DEFAULT '',
    note      TEXT NOT NULL DEFAULT ''
);
CREATE INDEX ix_transitions_ticket ON phase_transitions(ticket_id, rank);

CREATE TABLE note_entries (
    id        TEXT PRIMARY KEY,
    ticket_id TEXT NOT NULL REFERENCES tickets(id),
    rank      INTEGER NOT NULL,
    stamp     TEXT NOT NULL DEFAULT '',
    body      TEXT NOT NULL DEFAULT ''
);
CREATE INDEX ix_notes_ticket ON note_entries(ticket_id, rank);

CREATE TABLE feedback (
    id       TEXT PRIMARY KEY,
    seconds  INTEGER NOT NULL,
    signal   TEXT NOT NULL,
    trigger  TEXT NOT NULL,
    excerpt  TEXT NOT NULL DEFAULT '',
    context  TEXT NOT NULL DEFAULT '',
    resolved INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX ix_feedback_seconds ON feedback(seconds);

CREATE TABLE journal (
    id     TEXT PRIMARY KEY,
    entity TEXT NOT NULL,
    field  TEXT NOT NULL,
    old    TEXT NOT NULL DEFAULT '',
    new    TEXT NOT NULL DEFAULT '',
    at     TEXT NOT NULL DEFAULT '',
    actor  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX ix_journal_entity ON journal(entity, at);
