import { expect, test } from 'vitest'
import { DB, EMPTY, NOW, daysAgo } from './fixture.js'
import { needsRows, waitRows, askedDays, byAsked, byRecency } from '../worklog/internal/serve/static/assets/src/rows.js'

const rows = () => needsRows(DB)

test('one row per queue entry, not per task', () => {
  // `router` alone holds two entries; a task count would say 2 rows total.
  expect(rows()).toHaveLength(3)
  expect(rows().filter((r) => r.id === 'router')).toHaveLength(2)
})

test('a row carries the task it belongs to', () => {
  const r = rows().find((x) => x.id === 'router')
  expect(r.repo).toBe('ai-devboard')
  expect(r.taskTitle).toBe('Lens router')
  expect(r.entry.text).toBe('approve')
  expect(r.entry.type).toBe('checkpoint')
  expect(r.entry.detail).toContain('wire it up')
})

test("an epic's rows carry the child, and the CHILD's session", () => {
  const r = rows().find((x) => x.id === 'lens-board')
  expect(r.childId).toBe('a')
  expect(r.childTitle).toBe('child a')
  // The epic's own session is 'sess-epic'. Resuming that resumes the wrong
  // agent: every child of an epic runs its own.
  expect(r.session).toBe('sess-child-a')
})

test('a plain task takes its own session and has no child', () => {
  const r = rows().find((x) => x.id === 'router')
  expect(r.session).toBe('sess-abc123')
  expect(r.childId).toBe('')
  expect(r.childTitle).toBe('')
})

test('the malformed entry produces no rows and does not throw', () => {
  expect(() => needsRows(DB)).not.toThrow()
  expect(rows().some((r) => r.id === 'broken')).toBe(false)
})

test('waiting rows come from the same flattening', () => {
  const w = waitRows(DB)
  expect(w.map((r) => r.id).sort()).toEqual(['ledger', 'lens-board'])
  expect(w.find((r) => r.id === 'lens-board').childTitle).toBe('child b')
})

test('an empty payload yields no rows', () => {
  expect(needsRows(EMPTY)).toEqual([])
  expect(waitRows(EMPTY)).toEqual([])
})

test('a scalar queue entry renders as text rather than [object Object]', () => {
  const db = {
    feedback: [],
    repos: [{ repo: 'r', tasks: [{ id: 't', task: { title: 'T', needs_you: ['ask Preston'] } }] }],
  }
  expect(needsRows(db)[0].entry.text).toBe('ask Preston')
})

test('askedDays reads an ISO date, and rejects epoch seconds', () => {
  expect(askedDays(daysAgo(2), NOW)).toBe(2)
  expect(askedDays(undefined, NOW)).toBe(null)
  expect(askedDays('not a date', NOW)).toBe(null)
})

test('waiting sorts longest-waiting first, undated last', () => {
  const sorted = waitRows(DB).concat({ entry: {} }).sort(byAsked(NOW))
  expect(sorted.map((r) => askedDays(r.entry.asked, NOW))).toEqual([6, 2, null])
})

test('cards sort newest first, and a card with no mtime sorts last', () => {
  const sorted = [{ mtime: 10 }, { id: 'no-mtime' }, { mtime: 99 }].sort(byRecency)
  expect(sorted.map((c) => c.mtime)).toEqual([99, 10, undefined])
})
