import { fireEvent, render, screen, waitFor } from '@testing-library/preact'
import { h } from 'preact'
import { expect, test, vi } from 'vitest'
import { task } from './fixture.js'
import { Card } from '../worklog/internal/serve/static/assets/src/card.js'
import { archiveAction, MoveButton } from '../worklog/internal/serve/static/assets/src/archive.js'

const ok = (body = { status: 'archived' }) => ({ ok: true, json: async () => body })
const bad = (status, body) => ({ ok: false, status, json: async () => body })

const mount = (over = {}, onMove = () => Promise.resolve()) =>
  render(h(Card, { task: task(over), onMove }))

const button = () => screen.getByTestId('move')

test('a card renders no move control unless one is passed', () => {
  render(h(Card, { task: task() }))
  expect(screen.queryByTestId('move')).toBe(null)
})

test('a live task offers archive, an archived one offers un-archive', () => {
  mount()
  expect(button().dataset.kind).toBe('archive')
  expect(button().textContent).toContain('archive')
  mount({ id: 'gone', archived: true })
  const both = screen.getAllByTestId('move')
  expect(both[both.length - 1].dataset.kind).toBe('unarchive')
})

test('clicking calls the action with repo and id, and does not navigate', () => {
  const onMove = vi.fn(() => Promise.resolve())
  mount({}, onMove)
  const ev = new MouseEvent('click', { bubbles: true, cancelable: true })
  button().dispatchEvent(ev)
  expect(onMove).toHaveBeenCalledWith('archive', { repo: 'ai-devboard', id: 'sample' })
  // The control lives inside the card's <a>; an unswallowed click archives AND
  // leaves the lens.
  expect(ev.defaultPrevented).toBe(true)
})

test('the default action POSTs JSON to the right path, with the CSRF header', async () => {
  const f = vi.fn(async () => ok())
  await archiveAction(f)('archive', { repo: 'r', id: 'i' })
  expect(f).toHaveBeenCalledWith('/api/archive', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: '{"repo":"r","id":"i"}',
  })
  await archiveAction(f)('unarchive', { repo: 'r', id: 'i' })
  expect(f.mock.calls[1][0]).toBe('/api/unarchive')
})

test('a non-200 surfaces the server error message', async () => {
  const f = async () => bad(409, { error: 'destination already exists' })
  await expect(archiveAction(f)('archive', { repo: 'r', id: 'i' }))
    .rejects.toThrow('destination already exists')
})

test('a non-200 with an unreadable body falls back to the status', async () => {
  const f = async () => ({ ok: false, status: 500, json: async () => { throw new Error('not json') } })
  await expect(archiveAction(f)('archive', { repo: 'r', id: 'i' })).rejects.toThrow('HTTP 500')
})

test('a failed move flashes on the button and then reverts', async () => {
  render(h(MoveButton, {
    task: task(),
    onMove: () => Promise.reject(new Error('destination already exists')),
    revertMs: 20,
  }))
  fireEvent.click(button())
  await waitFor(() => expect(button().textContent).toContain('failed'))
  expect(button().title).toBe('destination already exists')
  expect(button().className).toContain('bad')
  await waitFor(() => expect(button().textContent).toContain('archive'))
  expect(button().className).not.toContain('bad')
})

test('a rejection with no message still leaves the card rendered', async () => {
  mount({}, () => Promise.reject(new Error('')))
  fireEvent.click(button())
  await waitFor(() => expect(button().textContent).toContain('failed'))
  expect(button().title).toBe('move failed')
  expect(screen.getByText('A sample task')).toBeTruthy()
})
