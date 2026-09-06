import { render, screen } from '@testing-library/preact'
import { h } from 'preact'
import { expect, test, vi } from 'vitest'
import { DB, EMPTY, NOW } from './fixture.js'
import { DoneLens, ArchivedLens } from '../worklog/internal/serve/static/assets/src/done.js'
import { TaskGrid } from '../worklog/internal/serve/static/assets/src/grid.js'

const onMove = () => Promise.resolve()
const mount = (Lens, db = DB) => render(h(Lens, { db, now: NOW, isDesktop: true, onMove }))
const titles = () => [...document.querySelectorAll('.ctitle')].map((e) => e.textContent)

test('done shows finished work and offers to archive it', () => {
  mount(DoneLens)
  expect(titles()).toEqual(['Stack scaffold'])
  expect(screen.getByTestId('move').dataset.kind).toBe('archive')
})

test('archived shows archived work and offers to restore it', () => {
  mount(ArchivedLens)
  expect(titles()).toEqual(['Archived thing'])
  expect(screen.getByTestId('move').dataset.kind).toBe('unarchive')
})

// An archived task is done too; counting it in both would double it against
// two chips and show it twice.
test('an archived task appears in archived only, never in done', () => {
  mount(DoneLens)
  expect(titles()).not.toContain('Archived thing')
})

test('each lens has its own calm line when empty', () => {
  mount(DoneLens, EMPTY)
  expect(document.querySelector('.calmline').textContent).toContain('nothing finished yet')
  mount(ArchivedLens, EMPTY)
  expect(document.querySelectorAll('.calmline')[1].textContent).toContain('nothing archived')
})

test('the grid orders newest first and puts an entry with no mtime last', () => {
  const t = (id, mtime) => ({ repo: 'r', id, mtime, task: { title: id } })
  render(h(TaskGrid, { tasks: [t('old', 10), t('broken', undefined), t('new', 99)], now: NOW }))
  expect(titles()).toEqual(['new', 'old', 'broken'])
})

test('the grid renders no move control unless the lens passes one', () => {
  render(h(TaskGrid, { tasks: [{ repo: 'r', id: 'a', mtime: 1, task: { title: 'A' } }], now: NOW }))
  expect(screen.queryByTestId('move')).toBe(null)
})

test('a parse-error entry renders as an error card rather than throwing', () => {
  const db = { feedback: [], repos: [{ repo: 'r', tasks: [{ id: 'b', archived: true, error: 'yaml: bad' }] }] }
  expect(() => mount(ArchivedLens, db)).not.toThrow()
  expect(document.querySelector('.card.err').textContent).toContain('yaml: bad')
})
