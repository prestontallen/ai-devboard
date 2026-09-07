import { expect, test } from 'vitest'
import {
  LENSES, routeFromHash, hashForLens, defaultRoute, taskFromHash, hashForTask,
} from '../worklog/internal/serve/static/assets/src/routes.js'

test('the six lenses ship in bar order, and backlog is not among them', () => {
  expect(LENSES).toEqual(['board', 'needs-you', 'waiting', 'friction', 'done', 'archived'])
  expect(LENSES).not.toContain('backlog')
})

test('a lens hash resolves to its lens', () => {
  for (const lens of LENSES) expect(routeFromHash(hashForLens(lens))).toBe(lens)
})

// The outgoing board's deep links have no leading slash, so they must never be
// mistaken for a lens — that is what lets both grammars share one origin.
test('unknown, legacy and empty hashes resolve to nothing rather than throwing', () => {
  for (const hash of [
    '', '#', '#/', '#/nope', '#/BOARD',
    '#ai-devboard/adb-lens-router',
    '#ai-devboard/adb-devboard-lens-board/adb-lens-router',
    '#/board/extra',
  ]) {
    expect(() => routeFromHash(hash)).not.toThrow()
    expect(routeFromHash(hash)).toBe(null)
  }
  expect(routeFromHash(undefined)).toBe(null)
})

test('an explicit lens hash is never overridden by the default rule', () => {
  expect(defaultRoute({ hash: '#/archived', phone: true, needsYou: 5 })).toBe(null)
})

test('the phone opens on needs-you only when something is actually waiting', () => {
  expect(defaultRoute({ hash: '', phone: true, needsYou: 2 })).toBe('needs-you')
  expect(defaultRoute({ hash: '', phone: true, needsYou: 0 })).toBe('board')
  expect(defaultRoute({ hash: '', phone: false, needsYou: 2 })).toBe('board')
})

// A legacy hash is not an explicit lens choice, so the rule still applies.
test('a legacy deep link does not suppress the default rule', () => {
  expect(defaultRoute({ hash: '#ai-devboard/router', phone: true, needsYou: 3 })).toBe('needs-you')
})

// ---- task detail route (adb-devboard-contract-ledger) ----

test('a task hash round-trips, including ids that need escaping', () => {
  expect(taskFromHash('#/task/ai-devboard/adb-lens-card')).toEqual({ repo: 'ai-devboard', id: 'adb-lens-card' })
  expect(taskFromHash(hashForTask('my repo', 'id/with slash'))).toEqual({ repo: 'my repo', id: 'id/with slash' })
})

test('anything that is not a task hash is not a task', () => {
  for (const hash of ['#/board', '#ai-devboard/router', '#/task/only-one', '#/task/a/b/c', '#/task/', '', undefined]) {
    expect(() => taskFromHash(hash)).not.toThrow()
    expect(taskFromHash(hash)).toBe(null)
  }
  // A malformed escape is not a route either, and must not throw.
  expect(taskFromHash('#/task/a/%E0%A4%A')).toBe(null)
})

// The blocker this ticket's scout found: defaultRoute asked only about lenses,
// so a pasted task link on a phone would have bounced to needs-you.
test('a task deep link counts as an explicit route, on a phone too', () => {
  expect(defaultRoute({ hash: '#/task/ai-devboard/router', phone: true, needsYou: 5 })).toBe(null)
  expect(defaultRoute({ hash: '#/task/ai-devboard/router', phone: false, needsYou: 0 })).toBe(null)
})

test('the phone rule still fires when the hash names nothing this app owns', () => {
  expect(defaultRoute({ hash: '', phone: true, needsYou: 2 })).toBe('needs-you')
  expect(defaultRoute({ hash: '#ai-devboard/router', phone: true, needsYou: 3 })).toBe('needs-you')
})
