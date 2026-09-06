import { fireEvent, render, screen } from '@testing-library/preact'
import { h } from 'preact'
import { expect, test, vi } from 'vitest'
import { DB, EMPTY, NOW } from './fixture.js'
import { FrictionLens } from '../worklog/internal/serve/static/assets/src/friction.js'
import { appCss } from './styles.js'

const mount = (db = DB) => render(h(FrictionLens, { db, now: NOW }))
const rows = () => screen.queryAllByTestId('frrow')

test('an entry renders its signal, trigger and age', () => {
  mount()
  const open = rows()[0]
  expect(open.querySelector('.frsig').textContent).toBe('tui-error')
  expect(open.querySelector('.qtext').textContent).toBe('tui error on resize')
  expect(open.querySelector('.qsub').textContent).toContain('ago')
})

test('excerpt and context both render', () => {
  mount()
  expect(rows()[0].querySelector('.qdetail').textContent).toContain('index out of range')
  expect(rows()[0].querySelector('.fnote').textContent).toContain('below 40 cols')
})

test('resolved entries sit in their own section, below the open ones', () => {
  mount()
  expect(rows()).toHaveLength(2)
  const done = document.querySelector('.frdone')
  expect(done.textContent).toContain('resolved · 1')
  expect(done.querySelectorAll('[data-testid="frrow"]')).toHaveLength(1)
  expect(done.querySelector('.qtext').textContent).toBe('already handled')
})

test('resolve is a copyable command, never a button that writes', () => {
  const writeText = vi.fn(() => Promise.resolve())
  vi.stubGlobal('navigator', { clipboard: { writeText } })
  try {
    mount()
    fireEvent.click(screen.getAllByTestId('frcopy')[0])
    expect(writeText).toHaveBeenCalledWith('worklog feedback resolve 1799990000')
  } finally {
    vi.unstubAllGlobals()
  }
})

test('an already-resolved entry offers no resolve control', () => {
  mount()
  expect(screen.getAllByTestId('frcopy')).toHaveLength(1)
  expect(document.querySelector('.frdone [data-testid="frcopy"]')).toBe(null)
})

test('the lens stays uncoloured — no attention or waiting tone in its markup', () => {
  const { container } = mount()
  expect(container.querySelectorAll('.attn, .qkind.wait, .flag')).toHaveLength(0)
})

// Asserted against the real stylesheet, not the markup: colouring friction
// amber later would be a one-line CSS change that no DOM test would notice.
test('no friction rule reaches for an attention colour', () => {
  const rules = appCss().split('}')
    .map((r) => r.split('{'))
    .filter(([sel]) => /(^|,|\s)\.(fr[a-z]*|sigcount)/.test(sel))
  expect(rules.length).toBeGreaterThan(4) // the rules exist to be checked
  for (const [sel, body] of rules) {
    expect(body || '', `${sel.trim()} must stay uncoloured`).not.toMatch(/--hold|--signal|--go\b/)
  }
})

test('signal counts summarise only the open entries', () => {
  mount()
  const counts = document.querySelector('.sigcounts').textContent
  expect(counts).toContain('tui-error')
  expect(counts).not.toContain('missing-feature') // resolved
})

test('no feedback at all is a calm line', () => {
  mount(EMPTY)
  expect(rows()).toHaveLength(0)
  expect(document.querySelector('.calmline').textContent).toContain('no friction captured')
})

test('all-reviewed reads as reviewed, not as empty', () => {
  mount({ repos: [], feedback: [{ timestamp: 1, signal: 's', trigger: 't', resolved: 2 }] })
  expect(document.querySelector('.calmline').textContent).toContain('all captured friction reviewed')
  expect(document.querySelector('.frdone')).toBeTruthy()
})

test('resolved is an int, so 0 means open — never a boolean', () => {
  mount({ repos: [], feedback: [{ timestamp: 1, signal: 's', trigger: 'still open', resolved: 0 }] })
  expect(document.querySelector('.frdone')).toBe(null)
  expect(screen.getAllByTestId('frcopy')).toHaveLength(1)
})
