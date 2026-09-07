import { render, screen } from '@testing-library/preact'
import { h } from 'preact'
import { expect, test } from 'vitest'
import { Ledger, orderedChecks } from '../worklog/internal/serve/static/assets/src/ledger.js'

const task = (over = {}) => ({ repo: 'r', id: 't', task: { title: 'T', phase: 'verify', ...over } })
const mount = (over = {}, isDesktop = true) => render(h(Ledger, { task: task(over), isDesktop }))

const steps = () => screen.queryAllByTestId('step')
const checks = () => screen.queryAllByTestId('check')
const ratio = (which) => screen.queryByTestId(`ratio-${which}`)

const PLAN = [
  { text: 'one', state: 'done' },
  { text: 'two', state: 'blocked', why: 'required checks live outside the repo' },
  { text: 'three', state: 'in_progress' },
  { text: 'four' },
]
const CARD = [
  { text: 'a', verify: 'go test', status: 'pass' },
  { text: 'b', verify: 'npm test' },
  { text: 'c', verify: 'by hand', status: 'fail' },
]

test('one header carries the phase and both ratios', () => {
  mount({ plan: PLAN, scorecard: CARD })
  expect(document.querySelector('.ledgerhead .phase').textContent).toBe('verify')
  expect(ratio('plan').textContent.replace(/\s+/g, ' ').trim()).toBe('1/4 plan')
  expect(ratio('checks').textContent.replace(/\s+/g, ' ').trim()).toBe('1/3 checks')
})

test('the time-in-phase slot stays empty until something fills it', () => {
  mount({ plan: PLAN })
  expect(document.querySelector('.inphase')).toBe(null)
})

test('plan is a numbered rail whose states are distinguishable', () => {
  mount({ plan: PLAN })
  expect(steps().map((s) => s.dataset.state)).toEqual(['done', 'blocked', 'doing', 'pending'])
  expect(steps()[0].textContent).toContain('1')
  // The rail keeps running past a blocked step: four steps render, not two.
  expect(steps()).toHaveLength(4)
})

test('a blocked step carries its reason inline when the file has one', () => {
  mount({ plan: PLAN })
  expect(steps()[1].querySelector('.why').textContent).toContain('outside the repo')
  // No reason recorded is the common case today; the step still reads blocked.
  mount({ plan: [{ text: 'x', state: 'blocked' }] })
  expect(screen.getAllByTestId('step').pop().querySelector('.why')).toBe(null)
})

test('outstanding and failed checks sort above passing, keeping their numbers', () => {
  mount({ scorecard: CARD })
  expect(checks().map((c) => c.dataset.state)).toEqual(['fail', 'open', 'pass'])
  // #03 failed, so it leads — but it is still #03.
  expect(checks()[0].querySelector('.n').textContent).toBe('03')
  expect(checks()[2].querySelector('.n').textContent).toBe('01')
})

test('every check shows its verify line, passing ones included', () => {
  mount({ scorecard: CARD })
  for (const c of checks()) expect(c.querySelector('.vf')).toBeTruthy()
  expect(checks()[2].querySelector('.vf').textContent).toContain('go test')
})

test('a failed check turns the header ratio too, not just its own row', () => {
  mount({ scorecard: CARD })
  expect(checks()[0].className).toContain('fail')
  expect(ratio('checks').className).toContain('bad')
})

test('ratios read green when finished and amber while outstanding', () => {
  mount({ plan: [{ text: 'a', state: 'done' }], scorecard: [{ text: 'x', status: 'pass' }] })
  expect(ratio('plan').className).toContain('done')
  expect(ratio('checks').className).toContain('done')
  mount({ plan: [{ text: 'a' }] })
  expect(screen.getAllByTestId('ratio-plan').pop().className).toContain('open')
})

test('an empty scorecard warns from tier 2 up, and never below it', () => {
  mount({ tier: 2 })
  expect(screen.getByTestId('no-contract').textContent).toContain('tier 2')
  // Renders accumulate inside one test: the harness cleans up between tests,
  // not between mounts.
  document.body.innerHTML = ''
  for (const tier of [0, 1, undefined, 'not a number']) {
    render(h(Ledger, { task: task({ tier }), isDesktop: true }))
    expect(screen.queryAllByTestId('no-contract')).toHaveLength(0)
    expect(document.body.textContent).toContain('no scorecard recorded')
    document.body.innerHTML = ''
  }
})

test('phone collapses passing checks behind a count; desktop shows them all', () => {
  mount({ scorecard: CARD }, false)
  expect(checks().map((c) => c.dataset.state)).toEqual(['fail', 'open'])
  expect(screen.getByTestId('collapsed').textContent).toContain('1 more passing')
  document.body.innerHTML = ''
  mount({ scorecard: CARD }, true)
  expect(checks()).toHaveLength(3)
  expect(screen.queryByTestId('collapsed')).toBe(null)
})

test('a bare task renders the frame with both tracks saying so', () => {
  mount({})
  expect(screen.getByTestId('ledger')).toBeTruthy()
  expect(document.body.textContent).toContain('no plan recorded')
  expect(document.body.textContent).toContain('no scorecard recorded')
  expect(ratio('plan')).toBe(null)
})

test('scalar plan and scorecard entries render as text', () => {
  mount({ plan: ['bare step'], scorecard: ['bare criterion'] })
  expect(steps()[0].textContent).toContain('bare step')
  expect(checks()[0].textContent).toContain('bare criterion')
  expect(checks()[0].dataset.state).toBe('open')
})

test('orderedChecks is stable inside a group', () => {
  const rows = orderedChecks([{ text: 'p1', status: 'pass' }, { text: 'o1' }, { text: 'o2' }])
  expect(rows.map((r) => r.item.text)).toEqual(['o1', 'o2', 'p1'])
})
