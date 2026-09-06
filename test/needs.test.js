import { fireEvent, render, screen } from '@testing-library/preact'
import { h } from 'preact'
import { expect, test, vi } from 'vitest'
import { DB, EMPTY, NOW } from './fixture.js'
import { NeedsYouLens } from '../worklog/internal/serve/static/assets/src/needs.js'
import { lensCounts } from '../worklog/internal/serve/static/assets/src/counts.js'

const mount = (over = {}) =>
  render(h(NeedsYouLens, { db: DB, now: NOW, isDesktop: true, ...over }))

const panels = () => screen.queryAllByTestId('qpanel')

test('one panel per item, and the chip count is that number', () => {
  mount()
  expect(panels()).toHaveLength(3)
  expect(lensCounts(DB)['needs-you']).toBe(panels().length)
})

test('a panel leads with the item, not the task', () => {
  mount()
  const p = panels()[0]
  expect(p.querySelector('.qtext').textContent).toBe('approve')
  expect(p.querySelector('.qsub').textContent).toContain('Lens router')
})

test('the type badge is the entry type, defaulting to question', () => {
  expect(mount().container.querySelectorAll('.qpanel.checkpoint')).toHaveLength(1)
  expect(panels().map((p) => p.dataset.kind)).toEqual(['checkpoint', 'question', 'question'])
})

test("an epic child's panel names the child and links to the child route", () => {
  mount()
  const p = panels().find((x) => x.textContent.includes('child a'))
  expect(p.querySelector('.qchild').textContent).toBe('child a')
  expect(p.querySelector('.qsub a').getAttribute('href')).toBe('/#ai-devboard/lens-board/a')
})

test('detail renders inline as a pre, and is absent when the entry has none', () => {
  mount()
  const withDetail = panels()[0]
  expect(withDetail.querySelector('.qdetail').textContent).toContain('wire it up')
  expect(panels()[1].querySelector('.qdetail')).toBe(null)
})

test('approve is checkpoint-only, answer is always there, and both are inert', () => {
  mount()
  const [checkpoint, question] = panels()
  expect(checkpoint.querySelectorAll('button.cta[disabled]')).toHaveLength(2)
  expect(checkpoint.textContent).toContain('✓ approve')
  expect(question.textContent).not.toContain('approve')
  const inert = [...document.querySelectorAll('.qcta button')]
    .filter((b) => b.dataset.testid !== 'qresume')
  expect(inert).toHaveLength(4) // one approve + three answers
  for (const b of inert) {
    expect(b.disabled).toBe(true)
    expect(b.title).toContain('adb-checkpoint-answer-endpoint')
  }
})

test('resume copies the CHILD session for a child row, never the epic one', async () => {
  const writeText = vi.fn(() => Promise.resolve())
  vi.stubGlobal('navigator', { clipboard: { writeText } })
  try {
    mount()
    const p = panels().find((x) => x.textContent.includes('child a'))
    fireEvent.click(p.querySelector('[data-testid="qresume"]'))
    expect(writeText).toHaveBeenCalledWith('claude --resume sess-child-a')
  } finally {
    vi.unstubAllGlobals()
  }
})

test('phone gets no resume button at all', () => {
  mount({ isDesktop: false })
  expect(screen.queryAllByTestId('qresume')).toHaveLength(0)
  // The inert pair still renders — placement is the point of drawing them.
  expect(screen.getAllByText('↳ answer').length).toBe(3)
})

test('the age slot stays empty until an entry carries a timestamp', () => {
  mount()
  expect(document.querySelector('.qage')).toBe(null)
  const since = NOW / 1000 - 7200
  const db = {
    feedback: [],
    repos: [{ repo: 'r', tasks: [{ id: 't', mtime: since, task: { title: 'T', needs_you: [{ text: 'x', since }] } }] }],
  }
  mount({ db })
  expect(document.querySelector('.qage').textContent).toBe('2h ago')
})

test('an empty queue says so rather than rendering an empty list', () => {
  mount({ db: EMPTY })
  expect(panels()).toHaveLength(0)
  expect(document.querySelector('.calmline').textContent).toContain('no questions or checkpoints')
})

test('a malformed entry contributes no panel and does not throw', () => {
  expect(() => mount()).not.toThrow()
  expect(panels().some((p) => p.textContent.includes('broken'))).toBe(false)
})
