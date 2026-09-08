import { flatten, inFlight, isError, isEpic } from './counts.js'
import { itemText } from './phases.js'
import { hashForTask, hashForChild } from './routes.js'

/**
 * Queue rows: one row per entry in a needs_you / waiting_on queue.
 *
 * counts.js flattens the same queues, but only ever counts them, so it throws
 * the provenance away. A row has to survive being rendered AND acted on, which
 * takes three things the flat entry does not carry:
 *
 *   - the task, for the subtitle and the deep link;
 *   - the CHILD, when the queue belongs to one — an epic's own queues are
 *     unused and every entry really comes from a child (schema.md, "Epic
 *     files"), so a row that forgets which child it came from cannot link to
 *     it or name it;
 *   - that child's own `session`. Each child of an epic runs its own agent, so
 *     resuming from the epic's session resumes the wrong one.
 *
 * Entries are passed through `itemText` for the same scalar tolerance the plan
 * and scorecard get: a hand-written `needs_you: ["ask Preston"]` renders as a
 * question rather than as `[object Object]`.
 */
function rowsFrom(tasks, field) {
  const out = []
  for (const t of tasks) {
    if (isError(t) || !t.task) continue
    const of = (entry, child) => ({
      repo: t.repo,
      id: t.id,
      taskTitle: t.task.title || t.id,
      childId: child ? child.id || '' : '',
      childTitle: child ? child.title || child.id || '' : '',
      session: child ? child.session : t.task.session,
      mtime: t.mtime,
      entry: itemText(entry),
    })
    if (isEpic(t)) {
      for (const child of t.task.children || []) {
        for (const entry of Array.isArray(child[field]) ? child[field] : []) {
          out.push(of(entry, child))
        }
      }
    } else {
      for (const entry of Array.isArray(t.task[field]) ? t.task[field] : []) {
        out.push(of(entry, null))
      }
    }
  }
  return out
}

/** Both queues read from in-flight work only — a done or archived task's
 *  leftover queue is history, not something that still wants you. */
export const needsRows = (db) => rowsFrom(inFlight(flatten(db)), 'needs_you')
export const waitRows = (db) => rowsFrom(inFlight(flatten(db)), 'waiting_on')

/**
 * Where a row's subtitle points.
 *
 * It lives beside the row builder, not in each lens, because both lenses draw
 * the same row and there is only one right answer for it. The two copies this
 * replaces are how `waiting.js` kept a legacy `/#` link through the whole seam
 * flip while `needs.js` was updated — the grep found it, a reader did not.
 */
export const rowHref = (r) =>
  r.childId ? hashForChild(r.repo, r.id, r.childId) : hashForTask(r.repo, r.id)

/** `asked` is an ISO date string, NOT epoch seconds — `ago()` would read it as
 *  1970. Longest-waiting first; an entry with no date sorts last rather than
 *  jumping the queue. */
export function askedDays(asked, now = Date.now()) {
  const t = Date.parse(asked || '')
  return Number.isNaN(t) ? null : Math.max(0, Math.floor((now - t) / 86400000))
}

export const byAsked = (now) => (a, b) =>
  (askedDays(b.entry.asked, now) ?? -1) - (askedDays(a.entry.asked, now) ?? -1)

/**
 * The one card comparator: newest first.
 *
 * The outgoing board hoisted needs-you cards to the top because the board was
 * the only place attention could surface. The Lens Board gives attention its
 * own route AND the line above the grid, so the hoist is redundant and the
 * grid is free to answer the question a grid is actually good at: what moved
 * most recently. An entry with no mtime — a parse error — sorts last.
 */
export const byRecency = (a, b) => (b.mtime || 0) - (a.mtime || 0)
