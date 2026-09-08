import { expect, test } from 'vitest'
import {
  LENSES, routeFromHash, hashForLens, defaultRoute, taskFromHash, hashForTask, hashForChild,
  legacyHash, adoptLegacyHash,
} from '../worklog/internal/serve/static/assets/src/routes.js'

// backlog joined with adb-lens-backlog, which added the server half it was
// waiting on; it sits between friction and done per the design brief.
test('the seven lenses ship in bar order', () => {
  expect(LENSES).toEqual(['board', 'needs-you', 'waiting', 'friction', 'backlog', 'done', 'archived'])
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

// Inverted at the cutover. A legacy hash used to be a link to the other board
// and so none of this app's business; now it resolves to a real view, and
// bouncing a phone off a pasted task link would be the bug the task route
// already fixed once.
test('a legacy deep link is an explicit route, so the phone rule leaves it alone', () => {
  expect(defaultRoute({ hash: '#ai-devboard/router', phone: true, needsYou: 3 })).toBe(null)
  expect(defaultRoute({ hash: '#ai-devboard/lens-board/kid', phone: true, needsYou: 3 })).toBe(null)
})

// ---- legacy hash translation (adb-lens-cutover) ----

test('the outgoing board\'s grammar translates to the new one', () => {
  expect(legacyHash('#ai-devboard/adb-lens-router')).toBe('#/task/ai-devboard/adb-lens-router')
  expect(legacyHash('#ai-devboard/lens-board/adb-lens-card'))
    .toBe('#/task/ai-devboard/lens-board/adb-lens-card')
  // Round-trips into something the app actually resolves.
  expect(taskFromHash(legacyHash('#r/e/c'))).toEqual({ repo: 'r', id: 'e', child: 'c' })
})

test('nothing already meaningful is translated', () => {
  for (const hash of [
    '#/board', '#/backlog', // lens hashes: the leading slash keeps them out
    '#/task/r/i', '#/task/r/e/c', // already the new grammar
    '#', '', undefined, '#one-segment', '#a/b/c/d', // no grammar at all
  ]) {
    expect(legacyHash(hash), `${hash} must not translate`).toBe(null)
  }
})

// The fragment is attacker-supplied in the sense that anyone can paste one.
// `[^/#]+` cannot span a slash, so no match can produce `//host`, and the
// rewrite only ever touches what follows the `#`.
test('translation cannot reach off-origin or past the fragment', () => {
  for (const hash of ['#//evil.test/x', '#https://evil.test/a', '#/../../etc/passwd']) {
    const out = legacyHash(hash)
    if (out !== null) {
      expect(out.startsWith('#/task/')).toBe(true)
      expect(out).not.toContain('//')
    }
  }
})

test('adopting a legacy hash replaces history rather than pushing it', () => {
  const calls = []
  const history = { replaceState: (...a) => calls.push(a) }
  const loc = { hash: '#r/i', pathname: '/', search: '' }

  expect(adoptLegacyHash(loc, history)).toBe(true)
  expect(calls).toHaveLength(1)
  // The rewritten URL keeps everything before the fragment exactly as it was.
  expect(calls[0][2]).toBe('/#/task/r/i')

  // A hash already in the new grammar is left alone, so a second pass is a
  // no-op and Back can never bounce into a re-translate loop.
  expect(adoptLegacyHash({ hash: '#/task/r/i', pathname: '/', search: '' }, history)).toBe(false)
  expect(calls).toHaveLength(1)
})

test('adopting preserves the path and query it was served from', () => {
  const calls = []
  adoptLegacyHash({ hash: '#r/i', pathname: '/next', search: '?x=1' },
    { replaceState: (...a) => calls.push(a) })
  expect(calls[0][2]).toBe('/next?x=1#/task/r/i')
})

// ---- task detail route (adb-devboard-contract-ledger) ----

test('a task hash round-trips, including ids that need escaping', () => {
  expect(taskFromHash('#/task/ai-devboard/adb-lens-card'))
    .toEqual({ repo: 'ai-devboard', id: 'adb-lens-card', child: null })
  expect(taskFromHash(hashForTask('my repo', 'id/with slash')))
    .toEqual({ repo: 'my repo', id: 'id/with slash', child: null })
})

test('anything that is not a task hash is not a task', () => {
  // `#/task/a/b/c` left this list with adb-lens-epic-detail: three segments is
  // now a child, and four is still nothing.
  for (const hash of ['#/board', '#ai-devboard/router', '#/task/only-one', '#/task/a/b/c/d', '#/task/', '', undefined]) {
    expect(() => taskFromHash(hash)).not.toThrow()
    expect(taskFromHash(hash)).toBe(null)
  }
  // A malformed escape is not a route either, and must not throw.
  expect(taskFromHash('#/task/a/%E0%A4%A')).toBe(null)
})

// ---- child detail route (adb-lens-epic-detail) ----

test('a child hash round-trips, and a plain task keeps a null child', () => {
  expect(taskFromHash('#/task/ai-devboard/lens-board/adb-lens-card'))
    .toEqual({ repo: 'ai-devboard', id: 'lens-board', child: 'adb-lens-card' })
  expect(taskFromHash(hashForChild('my repo', 'ep/ic', 'kid/1')))
    .toEqual({ repo: 'my repo', id: 'ep/ic', child: 'kid/1' })
  expect(taskFromHash(hashForTask('r', 'i')).child).toBe(null)
})

// Same blocker as the task route, one segment deeper: a child link opened on a
// phone must land on the child, not on needs-you.
test('a child deep link counts as an explicit route, on a phone too', () => {
  const hash = hashForChild('ai-devboard', 'lens-board', 'kid')
  expect(defaultRoute({ hash, phone: true, needsYou: 5 })).toBe(null)
  expect(defaultRoute({ hash, phone: false, needsYou: 0 })).toBe(null)
})

// The blocker this ticket's scout found: defaultRoute asked only about lenses,
// so a pasted task link on a phone would have bounced to needs-you.
test('a task deep link counts as an explicit route, on a phone too', () => {
  expect(defaultRoute({ hash: '#/task/ai-devboard/router', phone: true, needsYou: 5 })).toBe(null)
  expect(defaultRoute({ hash: '#/task/ai-devboard/router', phone: false, needsYou: 0 })).toBe(null)
})

test('the phone rule still fires when the hash names nothing this app owns', () => {
  expect(defaultRoute({ hash: '', phone: true, needsYou: 2 })).toBe('needs-you')
  // Junk that matches no grammar, rather than a legacy hash — those became
  // real routes at the cutover.
  expect(defaultRoute({ hash: '#', phone: true, needsYou: 3 })).toBe('needs-you')
  expect(defaultRoute({ hash: '#a/b/c/d', phone: true, needsYou: 3 })).toBe('needs-you')
})
