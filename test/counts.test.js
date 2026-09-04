import { expect, test } from 'vitest'
import { DB, EMPTY, NOW } from './fixture.js'
import {
  lensCounts, needsYou, waitingOn, isStale, isDone, isArchived, isError, flatten, inFlight,
} from '../worklog/internal/serve/static/assets/src/counts.js'

const byId = (id) => flatten(DB).find((t) => t.id === id)

test('counts are tasks-in-lens, not queue items', () => {
  // router and lens-board each have one needs-you task; the epic contributes
  // one task, not the two entries its children hold between them. board is 4,
  // not 3: the malformed entry stays in flight so it cannot silently vanish.
  expect(lensCounts(DB)).toEqual({
    board: 4,
    'needs-you': 2,
    waiting: 2,
    friction: 1,
    done: 1,
    archived: 1,
  })
})

test('an epic flattens its children rather than reading absent top-level fields', () => {
  const epic = byId('lens-board')
  expect(epic.task.needs_you).toBeUndefined()
  expect(needsYou(epic)).toHaveLength(1)
  expect(waitingOn(epic)).toHaveLength(1)
})

// An epic has no top-level phase, so deriving done-ness from children would
// close it behind the human's back; it stays in flight until archived.
test('an epic is in flight until it is archived, never done by inference', () => {
  const epic = byId('lens-board')
  expect(isDone(epic)).toBe(false)
  expect(inFlight(flatten(DB)).map((t) => t.id)).toContain('lens-board')
})

test('a malformed entry has no task or mtime and no helper throws on it', () => {
  const broken = byId('broken')
  expect(isError(broken)).toBe(true)
  expect(broken.task).toBeUndefined()
  expect(broken.mtime).toBeUndefined()
  for (const fn of [needsYou, waitingOn, isDone, isArchived]) {
    expect(() => fn(broken)).not.toThrow()
  }
  expect(() => isStale(broken, NOW)).not.toThrow()
  expect(isStale(broken, NOW)).toBe(false)
})

test('a malformed entry stays on the board rather than vanishing', () => {
  expect(inFlight(flatten(DB)).map((t) => t.id)).toContain('broken')
})

test('staleness comes from the injected clock, not from wall time', () => {
  expect(isStale(byId('router'), NOW)).toBe(true)   // mtime 5h old
  expect(isStale(byId('ledger'), NOW)).toBe(false)  // mtime 1m old
  expect(isStale(byId('router'), NOW - 5 * 3600 * 1000)).toBe(false)
})

test('archived tasks leave the board and count only as archived', () => {
  expect(inFlight(flatten(DB)).map((t) => t.id)).not.toContain('old-thing')
  expect(lensCounts(DB).archived).toBe(1)
  expect(lensCounts(DB).done).toBe(1) // the archived done task is not double-counted
})

test('an empty payload yields all zeroes and throws nothing', () => {
  expect(() => lensCounts(EMPTY)).not.toThrow()
  expect(Object.values(lensCounts(EMPTY)).every((n) => n === 0)).toBe(true)
  expect(lensCounts({})).toMatchObject({ board: 0, friction: 0 })
})
