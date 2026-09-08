import { render, screen } from '@testing-library/preact'
import { h } from 'preact'
import { expect, test, vi } from 'vitest'
import { NOW } from './fixture.js'
import { DetailView } from '../worklog/internal/serve/static/assets/src/detail.js'

const FULL = {
  repo: 'ai-devboard',
  id: 'ledger',
  file: 'ai-devboard/ledger.yaml',
  mtime: NOW / 1000 - 120,
  notes: '# Notes\n- a bullet\n',
  task: {
    title: 'The contract ledger', phase: 'verify', branch: 'master', tier: 2,
    complexity: 'medium', worklog: 'adb-devboard-contract-ledger', session: 'sess-xyz',
    plan: [{ text: 'one', state: 'done' }],
    scorecard: [{ text: 'a', verify: 'npm test', status: 'pass' }],
    needs_you: [{ type: 'checkpoint', text: 'approve the commit', detail: 'two files' }],
    waiting_on: [{ who: 'platform', text: 'flock?', asked: '2026-09-01', link: 'javascript:alert(1)' }],
    scout: { mode: 'inline', why: 'subagents disabled', when: '2026-09-06' },
    decisions: [{ what: 'ledger over sections', why: 'state is not evidence', when: '2026-09-06' }],
    code: [{ file: 'src/ledger.js', lines: '1-40', lang: 'js', note: 'the region', snippet: 'const a = 1' }],
    links: [
      { label: 'contract', url: 'https://example.test/c' },
      { label: 'sneaky', url: 'javascript:alert(1)' },
    ],
    surprise_key: 'from a newer schema',
  },
}

const db = (...tasks) => ({ feedback: [], repos: [{ repo: 'ai-devboard', tasks }] })
const mount = (over = {}) => render(h(DetailView, {
  db: db(FULL), repo: 'ai-devboard', id: 'ledger', now: NOW, isDesktop: true, ...over,
}))
const sections = () => screen.queryAllByTestId('section').map((s) => s.dataset.sec)

test('the hero carries the task and its grading', () => {
  mount()
  expect(document.querySelector('.hero h1').textContent).toBe('The contract ledger')
  const meta = document.querySelector('.metarow').textContent
  for (const bit of ['ai-devboard', '⎇ master', 'tier 2 · cx medium', '☰ adb-devboard-contract-ledger']) {
    expect(meta).toContain(bit)
  }
  expect(screen.getByTestId('detail-resume')).toBeTruthy()
})

test('the stepper labels the phases and marks the current one', () => {
  mount()
  const steps = [...screen.getByTestId('stepper').querySelectorAll('.sstep')]
  expect(steps.map((s) => s.textContent)).toContain('verify')
  expect(steps.filter((s) => s.className.includes('now'))).toHaveLength(1)
})

test('the ledger sits directly under the hero, before the record', () => {
  const { container } = mount()
  const order = [...container.querySelectorAll('.hero, .ledger, .record')]
    .map((e) => e.className.split(' ')[0])
  expect(order).toEqual(['hero', 'ledger', 'record'])
})

test('the record renders in the designed order', () => {
  mount()
  expect(sections()).toEqual([
    'Needs you', 'Waiting on', 'Risk scout', 'Decisions', 'Code to know', 'Links', 'Other',
  ])
})

test('a section with nothing to say is absent, not empty', () => {
  mount({ db: db({ ...FULL, task: { title: 'Bare', phase: 'intake' } }) })
  expect(sections()).toEqual([])
  // The ledger still frames the task; only the record thins out.
  expect(screen.getByTestId('ledger')).toBeTruthy()
})

test('unknown top-level keys surface in Other rather than vanishing', () => {
  mount()
  const row = screen.getByTestId('other-row').textContent
  expect(row).toContain('surprise_key')
  expect(row).toContain('from a newer schema')
})

test('the scout section reports what was attested', () => {
  mount()
  expect(screen.getByTestId('scout').textContent).toContain('inline')
  expect(screen.getByTestId('scout').textContent).toContain('subagents disabled')
})

// The absence IS the finding, and nothing else on the page reports it.
test('a medium-complexity task with no scout reports not attested', () => {
  mount({ db: db({ ...FULL, task: { title: 'T', complexity: 'medium' } }) })
  expect(screen.getByTestId('scout-missing').textContent).toContain('not attested')
})

test('low complexity owes no scout, so the section stays away', () => {
  mount({ db: db({ ...FULL, task: { title: 'T', complexity: 'low' } }) })
  expect(screen.queryByTestId('scout-missing')).toBe(null)
  expect(sections()).not.toContain('Risk scout')
})

test('a refused link scheme renders as text in both the record and the queue', () => {
  mount()
  expect(document.querySelectorAll('a[href^="javascript:"]')).toHaveLength(0)
  expect(screen.getByTestId('detail-wait').querySelector('.qlink').tagName).toBe('SPAN')
  const links = screen.getAllByTestId('link')
  expect(links[0].querySelector('a').getAttribute('href')).toBe('https://example.test/c')
  expect(links[1].querySelector('a')).toBe(null)
})

test('code snippets stay folded, and notes are their own fold', () => {
  mount()
  expect(screen.getByTestId('code').querySelector('details summary').textContent).toBe('snippet')
  expect(screen.getByTestId('notes').tagName).toBe('DETAILS')
  expect(screen.getByTestId('notes').querySelector('li').textContent).toBe('a bullet')
})

test('an unknown task offers a way back rather than a blank page', () => {
  mount({ id: 'nope' })
  expect(screen.getByTestId('detail-missing').textContent).toContain('No task ai-devboard/nope')
  expect(document.querySelector('.crumb')).toBeTruthy()
})

test('a parse-error task shows its error instead of throwing', () => {
  const broken = { repo: 'ai-devboard', id: 'broken', file: 'ai-devboard/broken.yaml', error: 'yaml: unclosed' }
  expect(() => mount({ db: db(broken), id: 'broken' })).not.toThrow()
  expect(screen.getByTestId('detail-error').textContent).toContain('yaml: unclosed')
})

// An epic's top-level plan/scorecard are unused, so it gets the roster rather
// than a ledger: rendering one would draw an empty agreement for every epic.
// This was a refusal linking out to `/` until adb-lens-epic-detail built the
// view; the no-ledger half of the claim is unchanged and still the point.
test('an epic renders its roster, and never a ledger or a stepper', () => {
  const epic = { repo: 'ai-devboard', id: 'e', task: { title: 'An epic', type: 'epic', children: [] } }
  mount({ db: db(epic), id: 'e' })
  expect(screen.getByTestId('detail-epic').textContent).toContain('no children started yet')
  expect(screen.queryByTestId('ledger')).toBe(null)
  expect(screen.queryByTestId('stepper')).toBe(null)
})

test('the archive control appears only when the lens passes one', () => {
  mount()
  expect(screen.queryByTestId('move')).toBe(null)
  document.body.innerHTML = ''
  mount({ onMove: vi.fn() })
  expect(screen.getByTestId('move')).toBeTruthy()
})
