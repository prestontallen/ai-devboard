import { act, render } from '@testing-library/preact'
import { h } from 'preact'
import { expect, test, vi } from 'vitest'
import { NOW, task, SPIKE, SPIKE_OFF_TRACK, NO_PHASE, OFF_ENUM, CHORE, LONG } from './fixture.js'
import { Card, detailHref } from '../worklog/internal/serve/static/assets/src/card.js'
import { PHASES, SPIKE_PHASES, itemText, planStats, checkStatuses, ago } from '../worklog/internal/serve/static/assets/src/phases.js'
import { safeHref, fallbackCopy } from '../worklog/internal/serve/static/assets/src/clipboard.js'
import { withAppStyles } from './styles.js'
import { BoardLens } from '../worklog/internal/serve/static/assets/src/board.js'

const draw = (props) => render(h(Card, { now: NOW, ...props })).container
const one = (props) => draw(props).querySelector('.card')

// ---------- face ----------

test('a card renders title, repo, track, phase label, meter and pips', () => {
  const c = one({ task: task({ task: { plan: [{ state: 'done' }, {}], scorecard: [{ status: 'pass' }] } }) })
  expect(c.querySelector('.ctitle').textContent).toContain('A sample task')
  expect(c.querySelector('.badge.repo').textContent).toBe('ai-devboard')
  expect(c.querySelectorAll('.track i')).toHaveLength(PHASES.length)
  expect(c.querySelector('.phase-lbl').textContent).toContain('implementing')
  expect(c.querySelector('.meter b').textContent).toBe('1/2')
  expect(c.querySelectorAll('.pip')).toHaveLength(1)
  expect(c.querySelector('.foot .age')).toBeTruthy()
})

test('a card links into the existing detail view on /', () => {
  const t = task({ repo: 'ai-devboard', id: 'sample' })
  expect(detailHref(t)).toBe('/#ai-devboard/sample')
  expect(one({ task: t }).getAttribute('href')).toBe('/#ai-devboard/sample')
})

// ---------- scalar tolerance: a shipped guarantee the port nearly lost ----------

test('scalar plan and scorecard entries render real values, not 0/N and pending', () => {
  const t = task({ task: { plan: ['a', 'b'], scorecard: ['only'] } })
  expect(itemText('a')).toEqual({ text: 'a' })
  expect(planStats(['a', 'b'])).toEqual({ total: 2, done: 0 })
  expect(checkStatuses(['only'])).toEqual(['pending'])
  const c = one({ task: t })
  expect(c.querySelector('.meter b').textContent).toBe('0/2')
  expect(c.querySelectorAll('.pip')).toHaveLength(1)
  expect(() => one({ task: t })).not.toThrow()
})

// ---------- track ----------

test('the track is right for standard, spike, off-enum and no-phase', () => {
  expect(one({ task: task({}) }).querySelectorAll('.track i')).toHaveLength(PHASES.length)
  expect(one({ task: SPIKE }).querySelectorAll('.track i')).toHaveLength(SPIKE_PHASES.length)
  expect(one({ task: SPIKE }).querySelector('.phase-lbl').textContent).toContain('2/4')

  const off = one({ task: OFF_ENUM }).querySelector('.phase-lbl')
  expect(off.className).toContain('unknown')
  expect(off.textContent).toContain('unknown phase')

  const none = one({ task: NO_PHASE }).querySelector('.phase-lbl')
  expect(none.textContent).toContain('no phase')
})

test('a spike parked off its short track renders on the full track', () => {
  const c = one({ task: SPIKE_OFF_TRACK })
  expect(c.querySelectorAll('.track i')).toHaveLength(PHASES.length)
  expect(c.querySelector('.phase-lbl').className).not.toContain('unknown')
})

// ---------- badges ----------

test('the type badge renders only for epic and spike', () => {
  expect(one({ task: LONG }).querySelector('.badge.epic')).toBeTruthy()
  expect(one({ task: SPIKE }).querySelector('.badge.spike')).toBeTruthy()
  expect(one({ task: CHORE }).querySelector('.badge.epic, .badge.spike')).toBe(null)
  expect(one({ task: task({}) }).querySelector('.badge.epic, .badge.spike')).toBe(null)
})

test('needs-you is a filled flag and waiting is an outlined badge', () => {
  const c = one({ task: task({ task: { needs_you: [{}, {}], waiting_on: [{}] } }) })
  const flag = c.querySelector('.flag')
  expect(flag.textContent).toContain('needs you · 2') // items on THIS task
  expect(c.querySelector('.badge.wait').textContent).toContain('1')
  expect(c.querySelector('.flag.badge')).toBe(null) // filled, not outlined
  expect(c.className).toContain('attn')
})

// The title-overflow bug that helped kill the per-child-cards attempt: the fix
// is an overflow invariant on the badge, not on the title.
test('a long badge truncates itself rather than squeezing the title', () => {
  const drop = withAppStyles()
  const c = one({ task: LONG })
  const badge = c.querySelector('.badge.repo')
  const title = c.querySelector('.ctitle')
  const style = (el) => getComputedStyle(el)
  expect(style(badge).textOverflow).toBe('ellipsis')
  expect(style(badge).overflow).toBe('hidden')
  expect(style(badge).maxWidth).not.toBe('none')
  expect(style(badge).flex).toBe('0 0 auto') // flex: none — the badge never grows
  expect(style(title).flexGrow).toBe('1')
  expect(style(title).minWidth).toBe('0')
  drop()
})

// ---------- clock ----------

test('age and the stale dot come from the injected clock', () => {
  const old = task({ mtime: NOW / 1000 - 5 * 3600 })
  const c = one({ task: old })
  expect(c.className).toContain('stale')
  expect(c.querySelector('.dot.old')).toBeTruthy()
  expect(c.querySelector('.age').textContent).toContain('— stale')
  // Same task, a clock from before its mtime: not stale.
  const early = one({ task: old, now: NOW - 5 * 3600 * 1000 })
  expect(early.className).not.toContain('stale')
})

// app.html mounts App without a `now` prop, so a required one would render NaN
// in the live page while every test stayed green.
test('a card with no now prop still shows a real age', () => {
  const c = one({ task: task({ mtime: Date.now() / 1000 - 120 }), now: undefined })
  const age = c.querySelector('.age').textContent
  expect(age).not.toContain('NaN')
  expect(age.trim()).not.toBe('')
  expect(ago(Date.now() / 1000 - 120)).toBe('2m ago')
})

// ---------- resume ----------

test('no session means no resume affordance and no empty slot', () => {
  expect(one({ task: task({}) }).querySelector('[data-testid="resume"]')).toBe(null)
})

test('the resume affordance is desktop-only', () => {
  const withSession = task({ task: { session: 'abc' } })
  expect(one({ task: withSession, isDesktop: true }).querySelector('[data-testid="resume"]')).toBeTruthy()
  expect(one({ task: withSession, isDesktop: false }).querySelector('[data-testid="resume"]')).toBe(null)
})

test('copying writes the resume command without navigating the card', async () => {
  const writeText = vi.fn(() => Promise.resolve())
  vi.stubGlobal('navigator', { clipboard: { writeText } })
  const c = one({ task: task({ task: { session: 'abc123' } }) })
  const btn = c.querySelector('[data-testid="resume"]')
  const ev = new MouseEvent('click', { bubbles: true, cancelable: true })
  await act(async () => { btn.dispatchEvent(ev) })
  expect(writeText).toHaveBeenCalledWith('claude --resume abc123')
  expect(ev.defaultPrevented).toBe(true) // the card did not navigate
  vi.unstubAllGlobals()
})

// The path that actually works over LAN cannot be exercised — happy-dom has no
// execCommand — so this asserts it degrades rather than throwing.
test('the non-secure fallback degrades instead of throwing', () => {
  expect(typeof document.execCommand).not.toBe('function')
  expect(() => fallbackCopy('x')).not.toThrow()
  expect(fallbackCopy('x')).toBe(false)
})

// ---------- epic roster ----------

test('an epic renders its roster, and a roster click does not navigate the card', async () => {
  const onChild = vi.fn()
  const c = one({ task: LONG, onChild })
  const chip = c.querySelector('.childchip')
  expect(chip.textContent).toContain('An extremely long child title')
  const ev = new MouseEvent('click', { bubbles: true, cancelable: true })
  await act(async () => { chip.dispatchEvent(ev) })
  expect(onChild).toHaveBeenCalledWith('ai-devboard', 'long', 'kid')
  expect(ev.defaultPrevented).toBe(true)
})

test('a child with no state renders as pending and emits no undefined id', () => {
  const t = task({ task: { type: 'epic', children: [{ id: 'k', title: 'k' }] } })
  const chip = one({ task: t }).querySelector('.childchip')
  expect(chip.dataset.state).toBe('pending')
  expect(chip.dataset.child).toBe('k')
  expect(one({ task: t }).innerHTML).not.toContain('undefined')
})

// ---------- error variant ----------

test('the error card shows file and the raw error and touches no task fields', () => {
  const c = one({ task: { repo: 'r', id: 'broken', file: 'r/broken.yaml', error: 'yaml: line 3: unclosed' } })
  expect(c.className).toContain('err')
  expect(c.textContent).toContain('r/broken.yaml')
  expect(c.textContent).toContain('yaml: line 3: unclosed')
  expect(c.querySelector('.track')).toBe(null)
})

// ---------- misc guarantees ----------

test('the time-in-phase slot collapses with no empty separator', () => {
  const lbl = one({ task: task({}) }).querySelector('.phase-lbl').textContent
  expect(lbl).toContain('implementing')
  expect(lbl).not.toMatch(/·\s*·/)
  expect(lbl.trim()).not.toMatch(/·$/)
})

test('a task-supplied url never reaches an href unsanitised', () => {
  expect(safeHref('javascript:alert(1)')).toBe(null)
  expect(safeHref('data:text/html,x')).toBe(null)
  expect(safeHref('https://example.com')).toBe('https://example.com')
  expect(safeHref('/#repo/id')).toBe('/#repo/id')
  expect(safeHref('')).toBe(null)
})

// id is only the filename stem, so two repos can hold the same one; every link
// on / is repo-namespaced for exactly this reason.
test('two repos holding the same task id render as independent cards', () => {
  const db = {
    feedback: [],
    repos: [
      { repo: 'alpha', tasks: [{ id: 'task', file: 'alpha/task.yaml', mtime: NOW / 1000, task: { title: 'Alpha task' } }] },
      { repo: 'beta', tasks: [{ id: 'task', file: 'beta/task.yaml', mtime: NOW / 1000, task: { title: 'Beta task' } }] },
    ],
  }
  const { container } = render(h(BoardLens, { db, now: NOW }))
  const cards = [...container.querySelectorAll('.card')]
  expect(cards).toHaveLength(2)
  expect(cards.map((c) => c.getAttribute('href'))).toEqual(['/#alpha/task', '/#beta/task'])
  expect(cards.map((c) => c.querySelector('.ctitle').textContent)).toEqual(['Alpha task', 'Beta task'])
})
