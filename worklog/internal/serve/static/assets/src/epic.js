import { h } from 'preact'
import htm from 'htm'
import { childState, byChildState } from './counts.js'
import { TaskGrid } from './grid.js'
import { NotesFold } from './markdown.js'

const html = htm.bind(h)

/**
 * Epic detail, and the adapter that makes it possible.
 *
 * An epic file's own plan/scorecard/phase/queues are unused: every child can
 * be independently active, so the in-flight detail lives per child under
 * `children[]`, in the exact shape a standalone task file uses (schema.md,
 * "Epic files"). That is what lets this page reuse the ONE card renderer
 * instead of growing a second one — `childTask` is an adapter, not a
 * component, and draws nothing.
 */

/**
 * A `children[]` entry as the `{repo, id, task}` shape every existing
 * component already consumes.
 *
 * Two fields are deliberate:
 *
 * - `mtime` is absent. There is no per-child mtime; the epic file's mtime
 *   describes the file, so borrowing it would date every child identically
 *   and mark them all stale in the same instant. The card guards on mtime and
 *   renders no dot and no age, which is the honest answer.
 * - `phase` is forced to `done` for a done child. Closing a child leaves its
 *   phase mid-flight (`adb-child-done-phase-sync`), so the live files carry
 *   `state: done` beside `phase: verify`; a done badge above a track parked at
 *   verify is a contradiction on screen. State is the authority, and the
 *   writer is fixed separately.
 *
 * `parent` is what `detailHref` reads to address the child through its epic —
 * a child has no task file of its own, so `<repo>/<epic-id>` is what resolves
 * the payload entry it was read out of.
 */
export function childTask(epic, child) {
  const c = child || {}
  const state = childState(c)
  return {
    repo: epic.repo,
    id: c.id || '',
    parent: epic.id,
    task: { ...c, state, phase: state === 'done' ? 'done' : c.phase },
  }
}

export const childrenOf = (epic) => ((epic.task || {}).children || [])

/** The aggregate line's numbers. A child with no `state` counts as pending —
 *  the same rule `childState` applies everywhere else, so the total always
 *  equals the sum of the three. */
export function childStats(children) {
  const list = Array.isArray(children) ? children : []
  const n = (want) => list.filter((c) => childState(c) === want).length
  return { total: list.length, done: n('done'), active: n('active'), pending: n('pending') }
}

/** The separators are text rather than elements between them: htm drops the
 *  whitespace around an element on its own line, so a markup separator reads
 *  as "4 children· 1 done" to anything consuming the text — a screen reader,
 *  or a copy-paste. */
function Aggregate({ stats }) {
  if (!stats.total) return null
  const parts = [
    [stats.total, stats.total === 1 ? 'child' : 'children'],
    [stats.done, 'done'],
    [stats.active, 'active'],
    [stats.pending, 'pending'],
  ]
  return html`
    <p class="agg" data-testid="epic-agg">
      ${parts.map(([n, label], i) => html`
        <span key=${label}>${i ? ' · ' : ''}<b>${n}</b>${` ${label}`}</span>`)}
    </p>`
}

/**
 * The child grid.
 *
 * Ordered by state rather than by recency: children share the epic file's
 * mtime, so the default comparator would be a constant across the whole grid —
 * sorting nothing while looking like it sorted something.
 */
export function ChildGrid({ epic, now, isDesktop }) {
  const children = childrenOf(epic)
  return html`
    <${TaskGrid}
      tasks=${children.map((c) => childTask(epic, c))}
      order=${(a, b) => byChildState(a.task, b.task)}
      now=${now}
      isDesktop=${isDesktop}
      empty="no children started yet" />`
}

/**
 * Epic detail.
 *
 * No stepper and no ledger: an epic has no phase and no contract of its own,
 * and drawing the frame anyway would show an empty agreement for every epic.
 * The roster IS the state here — the notes file below it is where the epic's
 * own reasoning lives.
 */
export function EpicView({ epic, now, isDesktop, hero }) {
  const k = epic.task || {}
  return html`
    <article data-testid="detail-epic" data-task=${`${epic.repo}/${epic.id}`}>
      ${hero}
      <${Aggregate} stats=${childStats(childrenOf(epic))} />
      <${ChildGrid} epic=${epic} now=${now} isDesktop=${isDesktop} />
      <${NotesFold} notes=${epic.notes} worklog=${k.worklog} />
    </article>`
}
