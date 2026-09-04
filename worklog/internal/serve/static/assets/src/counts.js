/**
 * Task classification and lens counts.
 *
 * Counting grammar: five lenses count TASKS — a chip is a route, so "3" has to
 * mean "you will see 3 things here". The outgoing board mixes units (in-flight
 * counts tasks while needs-you sums queue items across tasks), which is exactly
 * what made the phone default-route rule ambiguous. Friction is the one
 * exception and counts unresolved FEEDBACK.md entries, because it is a global
 * log rather than a set of tasks.
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

export function lensCounts(db) {
  const all = flatten(db)
  const live = all.filter((t) => !isArchived(t))
  const open = inFlight(all)
  const feedback = ((db && db.feedback) || []).filter((e) => !e.resolved)
  return {
    board: open.length,
    'needs-you': open.filter((t) => needsYou(t).length > 0).length,
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
