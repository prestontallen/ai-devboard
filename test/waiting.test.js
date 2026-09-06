import { render, screen } from '@testing-library/preact'
import { h } from 'preact'
import { expect, test } from 'vitest'
import { DB, EMPTY, NOW } from './fixture.js'
import { WaitingLens } from '../worklog/internal/serve/static/assets/src/waiting.js'

const mount = (db = DB) => render(h(WaitingLens, { db, now: NOW }))
const rows = () => screen.queryAllByTestId('waitrow')

test('a row leads with who is being waited on', () => {
  mount()
  expect(rows()).toHaveLength(2)
  expect(rows()[0].querySelector('.qkind').textContent).toBe('platform')
})

test('longest-waiting first', () => {
  mount()
  expect(rows().map((r) => r.querySelector('.qage').textContent)).toEqual(['6d', '2d'])
})

test('an http link is clickable and opens away from the board', () => {
  mount()
  const a = rows()[0].querySelector('a.qlink')
  expect(a.getAttribute('href')).toBe('https://example.test/thread/1')
  expect(a.getAttribute('rel')).toBe('noopener')
})

test('a javascript: link renders as inert text, never as an href', () => {
  mount()
  const dead = rows()[1].querySelector('.qlink')
  expect(dead.tagName).toBe('SPAN')
  expect(dead.getAttribute('href')).toBe(null)
  expect(document.querySelectorAll('a[href^="javascript:"]')).toHaveLength(0)
})

test("an epic's row names the child it belongs to", () => {
  mount()
  expect(rows()[1].querySelector('.qchild').textContent).toBe('child b')
})

test('an empty queue says so rather than rendering an empty list', () => {
  mount(EMPTY)
  expect(rows()).toHaveLength(0)
  expect(document.querySelector('.calmline').textContent).toContain('waiting on no one')
})

test('an entry with no asked date renders without an age and sorts last', () => {
  const db = {
    feedback: [],
    repos: [{ repo: 'r', tasks: [
      { id: 'a', task: { title: 'A', waiting_on: [{ who: 'x', text: 'undated' }] } },
      { id: 'b', task: { title: 'B', waiting_on: [{ who: 'y', text: 'dated', asked: '2020-01-01' }] } },
    ] }],
  }
  mount(db)
  expect(rows().map((r) => r.querySelector('.qtext').textContent)).toEqual(['dated', 'undated'])
  expect(rows()[1].querySelector('.qage')).toBe(null)
})

test('an asked-today entry reads today, not 0d', () => {
  const asked = new Date(NOW).toISOString().slice(0, 10)
  const db = {
    feedback: [],
    repos: [{ repo: 'r', tasks: [{ id: 'a', task: { title: 'A', waiting_on: [{ who: 'x', asked }] } }] }],
  }
  mount(db)
  expect(rows()[0].querySelector('.qage').textContent).toBe('today')
})
