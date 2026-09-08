import { render } from '@testing-library/preact'
import { h } from 'preact'
import { readFileSync, readdirSync } from 'node:fs'
import { expect, test } from 'vitest'
import { NOW, EPIC, EPIC_EMPTY } from './fixture.js'
import {
  childTask, childStats, childrenOf, ChildGrid,
} from '../worklog/internal/serve/static/assets/src/epic.js'
import { detailHref } from '../worklog/internal/serve/static/assets/src/card.js'
import { childState } from '../worklog/internal/serve/static/assets/src/counts.js'
import { DetailView } from '../worklog/internal/serve/static/assets/src/detail.js'
import { appCss } from './styles.js'

const child = (id) => childrenOf(EPIC).find((c) => c.id === id)
const adapt = (id) => childTask(EPIC, child(id))

// ---------- the adapter ----------
// If a children[] entry cannot be fed to the components a standalone task
// already uses, the whole premise of this ticket is wrong. These run first.

test('a child adapts to the shape every existing component consumes', () => {
  const t = adapt('detail')
  expect(t.repo).toBe('ai-devboard')
  expect(t.id).toBe('detail')
  expect(t.parent).toBe('lens-epic')
  // The body arrives intact: this is what lets Ledger, Record and Stepper read
  // a child without knowing it is one.
  expect(t.task.plan).toHaveLength(2)
  expect(t.task.scorecard).toHaveLength(2)
  expect(t.task.decisions).toHaveLength(1)
  expect(t.task.needs_you).toHaveLength(1)
  expect(t.task.scout.mode).toBe('inline')
  expect(t.task.session).toBe('sess-child')
})

test('a child carries no mtime, because no per-child mtime exists', () => {
  // Borrowing the epic file's would date every child identically and mark them
  // all stale in the same instant.
  expect(adapt('detail').mtime).toBeUndefined()
  expect(adapt('ledger').mtime).toBeUndefined()
})

test('state done outranks a phase left mid-flight', () => {
  // The live shape: closing a child does not advance its phase
  // (adb-child-done-phase-sync), so the file says done and verify at once.
  expect(child('ledger').phase).toBe('verify')
  expect(adapt('ledger').task.phase).toBe('done')
  // An open child keeps the phase it actually has.
  expect(adapt('detail').task.phase).toBe('implementing')
})

test('a missing state reads as pending, and never as a fourth state', () => {
  expect(childState(child('stateless'))).toBe('pending')
  expect(adapt('stateless').task.state).toBe('pending')
  expect(childState({ state: 'nonsense' })).toBe('pending')
  expect(childState(undefined)).toBe('pending')
})

test('a child is addressed through its epic, never on its own', () => {
  expect(detailHref(adapt('detail'))).toBe('#/task/ai-devboard/lens-epic/detail')
})

test('the adapter tolerates a junk entry rather than throwing', () => {
  expect(() => childTask(EPIC, undefined)).not.toThrow()
  expect(childTask(EPIC, {}).id).toBe('')
})

// ---------- the aggregate ----------

test('the aggregate counts every child, with no state landing in pending', () => {
  const s = childStats(childrenOf(EPIC))
  expect(s).toEqual({ total: 4, done: 1, active: 1, pending: 2 })
  expect(s.done + s.active + s.pending).toBe(s.total)
})

test('an epic with no children aggregates to zero rather than throwing', () => {
  expect(childStats(childrenOf(EPIC_EMPTY))).toEqual({ total: 0, done: 0, active: 0, pending: 0 })
  expect(childStats(undefined).total).toBe(0)
})

// ---------- the grid ----------

const grid = (epic) =>
  render(h(ChildGrid, { epic, now: NOW, isDesktop: true })).container

test('children order active, then pending, then done', () => {
  const titles = [...grid(EPIC).querySelectorAll('.card .ctitle')].map((e) => e.textContent)
  expect(titles).toEqual([
    'Epic and child detail', // active
    'Lens cutover', // pending
    'No state recorded', // pending, from an absent state
    'Contract ledger', // done
  ])
})

test('a child card shows no age and no freshness dot', () => {
  const card = grid(EPIC).querySelector('.card')
  expect(card.querySelector('.age')).toBeNull()
  expect(card.querySelector('.dot')).toBeNull()
})

test('a pending child reads as not started, not as a missing field', () => {
  const cards = [...grid(EPIC).querySelectorAll('.card')]
  const pending = cards.find((c) => c.querySelector('.ctitle').textContent === 'Lens cutover')
  const lbl = pending.querySelector('.phase-lbl')
  expect(lbl.textContent.trim()).toBe('not started')
  expect(lbl.className).not.toContain('unknown')
})

test('a done child renders on the done phase, not the one it was closed at', () => {
  const cards = [...grid(EPIC).querySelectorAll('.card')]
  const done = cards.find((c) => c.querySelector('.ctitle').textContent === 'Contract ledger')
  expect(done.querySelector('.phase-lbl').textContent).toContain('done')
  expect(done.querySelector('.phase-lbl').textContent).not.toContain('verify')
})

test('an epic with no children says so instead of drawing an empty grid', () => {
  const c = grid(EPIC_EMPTY)
  expect(c.querySelector('.grid')).toBeNull()
  expect(c.querySelector('.calmline').textContent).toContain('no children started yet')
})

test('each child card links to its own route', () => {
  const hrefs = [...grid(EPIC).querySelectorAll('.card')].map((c) => c.getAttribute('href'))
  expect(hrefs).toContain('#/task/ai-devboard/lens-epic/detail')
  expect(hrefs.every((h) => h.startsWith('#/task/ai-devboard/lens-epic/'))).toBe(true)
})

// ---------- the two routes ----------

const db = (...tasks) => ({ repos: [{ repo: 'ai-devboard', tasks }], feedback: [] })
const mount = (props) =>
  render(h(DetailView, { db: db(EPIC, EPIC_EMPTY), now: NOW, repo: 'ai-devboard', ...props })).container

const epicPage = (props) => mount({ id: 'lens-epic', ...props })
const childPage = (child, props) => mount({ id: 'lens-epic', child, ...props })

test('the epic route renders the hero, the aggregate and the roster', () => {
  const c = epicPage()
  expect(c.querySelector('.hero h1').textContent).toBe('Devboard Lens Board')
  expect(c.querySelector('.badge.epic')).toBeTruthy()
  expect(c.querySelector('[data-testid=epic-agg]').textContent.replace(/\s+/g, ' '))
    .toContain('4 children · 1 done · 1 active · 2 pending')
  expect(c.querySelectorAll('.card')).toHaveLength(4)
  expect(c.querySelector('[data-testid=notes]').textContent).toContain('The epic notes body.')
})

test('the epic page draws neither a ledger nor a stepper', () => {
  const c = epicPage()
  expect(c.querySelector('[data-testid=ledger]')).toBeNull()
  expect(c.querySelector('[data-testid=stepper]')).toBeNull()
})

// The epic file carries a session, and it is the wrong one to hand you: each
// child runs its own agent, so an epic's top-level fields are unused.
test('the epic hero offers no resume, and a child hero resumes the child', () => {
  expect(epicPage().querySelector('[data-testid=detail-resume]')).toBeNull()
  const btn = childPage('detail').querySelector('[data-testid=detail-resume]')
  expect(btn.getAttribute('title')).toContain('sess-child')
  expect(btn.getAttribute('title')).not.toContain('sess-epic')
})

test('the child route renders the ledger, the record and the child notes', () => {
  const c = childPage('detail')
  expect(c.querySelector('.hero h1').textContent).toBe('Epic and child detail')
  expect(c.querySelector('[data-testid=ledger]')).toBeTruthy()
  expect(c.querySelector('[data-testid=ratio-plan]').textContent).toContain('1/2')
  expect(c.querySelector('[data-testid=ratio-checks]').textContent).toContain('1/2')
  expect(c.querySelector('[data-testid=stepper]')).toBeTruthy()
  expect(c.querySelector('[data-testid=scout]')).toBeTruthy()
  expect(c.querySelector('[data-testid=decision]')).toBeTruthy()
  expect(c.querySelector('[data-testid=detail-need]')).toBeTruthy()
  expect(c.querySelector('[data-testid=notes]').textContent).toContain('The child notes body.')
})

test('back from a child goes to its epic, not to the board', () => {
  const back = childPage('detail').querySelector('.crumb')
  expect(back.getAttribute('href')).toBe('#/task/ai-devboard/lens-epic')
  expect(back.textContent).toContain('Devboard Lens Board')
})

test("a child's own identity fields are not reported as unknown schema keys", () => {
  const rows = [...childPage('detail').querySelectorAll('[data-testid=other-row] td:first-child')]
    .map((td) => td.textContent)
  expect(rows).toContain('mystery')
  for (const own of ['id', 'title', 'state', 'notes']) expect(rows).not.toContain(own)
})

// ---------- sad paths ----------

test('an unknown child offers a way back to its epic', () => {
  const c = childPage('ghost')
  expect(c.querySelector('[data-testid=detail-missing]').textContent).toContain('No child ghost')
  expect(c.querySelector('.crumb').getAttribute('href')).toBe('#/task/ai-devboard/lens-epic')
})

test('a child hash pointing at a plain task is refused, not silently reparented', () => {
  const plain = { repo: 'ai-devboard', id: 'solo', mtime: NOW / 1000, task: { title: 'Solo', phase: 'plan' } }
  const c = render(h(DetailView, {
    db: db(plain), now: NOW, repo: 'ai-devboard', id: 'solo', child: 'kid',
  })).container
  expect(c.querySelector('[data-testid=detail-missing]')).toBeTruthy()
  expect(c.querySelector('[data-testid=ledger]')).toBeNull()
})

test('an unknown epic and a parse-error epic each render a way onward', () => {
  expect(mount({ id: 'nope' }).querySelector('[data-testid=detail-missing]')).toBeTruthy()
  const broken = { repo: 'ai-devboard', id: 'bad', file: 'ai-devboard/bad.yaml', error: 'yaml: unclosed' }
  const c = render(h(DetailView, { db: db(broken), now: NOW, repo: 'ai-devboard', id: 'bad' })).container
  expect(c.querySelector('[data-testid=detail-error]').textContent).toContain('yaml: unclosed')
})

test('an epic with no children still renders its hero above the empty roster', () => {
  const c = mount({ id: 'barren' })
  expect(c.querySelector('.hero h1').textContent).toBe('An epic with nothing under it')
  expect(c.querySelector('[data-testid=epic-agg]')).toBeNull()
  expect(c.querySelector('.calmline').textContent).toContain('no children started yet')
})

// ---------- phone ----------

test('a child page keeps the ledger phone treatment', () => {
  const desktop = childPage('detail', { isDesktop: true })
  expect(desktop.querySelector('[data-testid=collapsed]')).toBeNull()
  const phone = childPage('detail', { isDesktop: false })
  // The one passing check collapses so the outstanding one fills the screen.
  expect(phone.querySelector('[data-testid=collapsed]').textContent).toContain('1 more passing')
  expect(phone.querySelectorAll('[data-testid=check]')).toHaveLength(1)
})

test('the child grid stacks to one column on a phone', () => {
  const css = appCss().replace(/\s+/g, ' ')
  // ChildGrid renders through .grid, so the board's own phone rule carries it.
  expect(css).toMatch(/@media[^{]*max-width: 640px[^}]*\{[^@]*\.grid \{ grid-template-columns: 1fr;/)
})

// ---------- the two invariants this epic keeps having to defend ----------

const SRC = 'worklog/internal/serve/static/assets/src'
// Comments are stripped first: these are claims about what the code does, and
// a doc comment explaining a rule must not read as breaking it.
const source = (f) => readFileSync(`${SRC}/${f}`, 'utf8')
  .replace(/\/\*[\s\S]*?\*\//g, '')
  .replace(/(^|[^:])\/\/.*$/gm, '$1')
const modules = () => readdirSync(SRC).filter((f) => f.endsWith('.js'))

test('there is still exactly one card renderer', () => {
  // adb-epic-per-child-cards died of parallel render modes. The epic page
  // reuses the card by adapting its input, so nothing new draws one.
  expect(modules().filter((f) => /export\s+function\s+\w*Card\b/.test(source(f)))).toEqual(['card.js'])
  // ...and it reaches the card through the shared grid rather than past it.
  expect(source('epic.js')).toMatch(/import\s*\{[^}]*TaskGrid[^}]*\}\s*from\s*'\.\/grid\.js'/)
})

test('no module in the app links back to the outgoing board', () => {
  // `/#<repo>/<id>` is the legacy grammar. With epic and child detail built,
  // every link this app emits stays inside it.
  expect(modules().filter((f) => /['"`]\/#/.test(source(f)))).toEqual([])
})
