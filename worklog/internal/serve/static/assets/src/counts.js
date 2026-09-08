/**
 * Task classification and lens counts.
 *
 * Counting grammar: a chip is a route, so "3" has to mean "you will see 3
 * things here". What that unit is follows the lens. Four lenses render cards
 * and count TASKS. Needs-you renders one panel per queue ITEM, so it counts
 * items — the same rule applied, not an exception to it. Friction counts
 * unresolved FEEDBACK.md entries, because it is a global log rather than a
 * set of tasks.
 *
 * The outgoing board mixed units without meaning to (in-flight counted tasks
 * while needs-you summed items), which is what made the phone default-route
 * rule ambiguous. The fix was never "always count tasks" — it was "count what
 * the lens draws".
 *
 * Every helper guards `error` before touching `task`: a malformed file yields
 * an entry with `error` and no `task`/`mtime` at all (devboard/API.md), so an
 * unguarded field access throws on the whole board.
 */

export const STALE_SECONDS = 2 * 3600

export const isError = (t) => !!(t && t.error)
export const isArchived = (t) => !!(t && t.archived)
export const isEpic = (t) => !!(t && t.task && t.task.type === 'epic')
export const phaseOf = (t) => (t && t.task && t.task.phase) || ''

/** Epics have no top-level phase, so they never read as done. That is
 *  deliberate: an epic closes when the human archives it (`worklog done`),
 *  not when its last child happens to finish. */
export const isDone = (t) => !isError(t) && phaseOf(t) === 'done'

export const isStale = (t, now = Date.now()) =>
  !isError(t) && !!t.mtime && now / 1000 - t.mtime > STALE_SECONDS

/**
 * `children[].state` is pending | active | done, and a child that has never
 * been started carries none at all — the roster is written from the worklog
 * relation before any agent touches the entry. Absent reads as pending, never
 * as a fourth state, and an unrecognised value reads as pending too rather
 * than sorting off the end of the list.
 *
 * This lives here, with the rest of the classification, because both the
 * roster chips on an epic card and the child grid on epic detail order by it —
 * two copies of the order is how they drift apart.
 */
export const childState = (c) => {
  const s = (c && c.state) || ''
  return s === 'active' || s === 'done' ? s : 'pending'
}

const CHILD_ORDER = { active: 0, pending: 1, done: 2 }
export const byChildState = (a, b) => CHILD_ORDER[childState(a)] - CHILD_ORDER[childState(b)]

/** An epic's own needs_you/waiting_on are unused — the real queues live per
 *  child (devboard/schema.md, "Epic files"), so both flatten across children. */
function queue(t, field) {
  if (isError(t) || !t.task) return []
  if (isEpic(t)) return (t.task.children || []).flatMap((c) => c[field] || [])
  return Array.isArray(t.task[field]) ? t.task[field] : []
}

export const needsYou = (t) => queue(t, 'needs_you')
export const waitingOn = (t) => queue(t, 'waiting_on')

export const flatten = (db) =>
  ((db && db.repos) || []).flatMap((r) => (r.tasks || []).map((t) => ({ repo: r.repo, ...t })))

/** In-flight: not done, not archived. Error cards stay in — a malformed file
 *  must remain visible (devboard/README.md), not vanish from the board. */
export const inFlight = (tasks) => tasks.filter((t) => !isArchived(t) && !isDone(t))

/** WORK.md's not-yet-started sections, as the payload carries them. Absent on
 *  a payload from a server older than adb-lens-backlog, which is why every
 *  reader goes through here rather than touching `db.backlog` directly. */
export const backlogSections = (db) => ((db && db.backlog) || []).filter((s) => s && s.name)

export const backlogItems = (db) => backlogSections(db).flatMap((s) => s.items || [])

export function lensCounts(db) {
  const all = flatten(db)
  const live = all.filter((t) => !isArchived(t))
  const open = inFlight(all)
  const feedback = ((db && db.feedback) || []).filter((e) => !e.resolved)
  return {
    board: open.length,
    // Items across both sections: the lens draws a row per ticket, and the
    // chip counts what the lens draws.
    backlog: backlogItems(db).length,
    // Items, not tasks: the needs-you lens draws a panel per entry, and one
    // task can hold several. See the counting grammar above.
    'needs-you': open.reduce((n, t) => n + needsYou(t).length, 0),
    waiting: open.filter((t) => waitingOn(t).length > 0).length,
    friction: feedback.length,
    done: live.filter(isDone).length,
    archived: all.filter(isArchived).length,
  }
}

export const TONES = {
  'needs-you': 'attn',
  waiting: 'wait',
}
