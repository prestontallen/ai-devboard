import { act, fireEvent, render, screen, waitFor } from '@testing-library/preact'
import { h } from 'preact'
import { beforeEach, expect, test, vi } from 'vitest'
import { DB, EMPTY, NOW } from './fixture.js'
import { App } from '../worklog/internal/serve/static/assets/src/app.js'
import { LENSES } from '../worklog/internal/serve/static/assets/src/routes.js'

/** Cross-lens behavior: the things that must hold for ALL SIX routes, which is
 *  exactly what a per-lens test file cannot assert. */

const desktop = () => false
const settle = () => act(async () => { await new Promise((r) => setTimeout(r, 0)) })

async function mount(props = {}) {
  const r = render(h(App, {
    load: () => Promise.resolve(DB), now: NOW, matchPhone: desktop, onMove: () => Promise.resolve(), ...props,
  }))
  await settle()
  return r
}

const go = async (lens) => {
  await act(async () => { location.hash = `#/${lens}` })
  await settle()
}

const main = () => screen.getByTestId('lens')
const chip = (lens) => document.querySelector(`.chip[href="#/${lens}"]`)

beforeEach(() => { location.hash = '' })

test('every route renders a body — no placeholder survives', async () => {
  await mount()
  for (const lens of LENSES) {
    await go(lens)
    expect(main().dataset.lens).toBe(lens)
    expect(main().textContent).not.toContain('not built yet')
    expect(main().textContent.trim()).not.toBe('')
  }
})

test('every lens has its own calm line when empty, and its chip dims at 0', async () => {
  await mount({ load: () => Promise.resolve(EMPTY) })
  const seen = new Set()
  for (const lens of LENSES) {
    await go(lens)
    const calm = main().querySelector('.calmline')
    expect(calm, `${lens} must say something when empty`).toBeTruthy()
    seen.add(calm.textContent.trim())
    expect(chip(lens).className).toContain('zero')
  }
  // Six routes, six distinct messages: a shared "nothing here" would tell you
  // nothing about which lens you are standing in.
  expect(seen.size).toBe(LENSES.length)
})

test('no lens throws on the malformed entry, and it stays visible where it belongs', async () => {
  await mount()
  for (const lens of LENSES) {
    await expect(go(lens)).resolves.not.toThrow()
  }
  await go('board')
  expect(main().querySelector('.card.err')).toBeTruthy()
})

test('the needs-you chip and the panels it routes to agree', async () => {
  await mount()
  await go('needs-you')
  expect(chip('needs-you').querySelector('b').textContent)
    .toBe(String(screen.getAllByTestId('qpanel').length))
})

test('archiving from the done lens calls the action and reloads the board', async () => {
  const onMove = vi.fn(() => Promise.resolve({ status: 'archived' }))
  const load = vi.fn(() => Promise.resolve(DB))
  await mount({ onMove, load })
  await go('done')
  const before = load.mock.calls.length
  fireEvent.click(screen.getByTestId('move'))
  await waitFor(() => expect(onMove).toHaveBeenCalledWith('archive', { repo: 'ai-devboard', id: 'scaffold' }))
  // The server emits an SSE tick after a successful move, but the lens must not
  // depend on the transport being up to stop showing the moved card.
  await waitFor(() => expect(load.mock.calls.length).toBeGreaterThan(before))
})

test('a failed archive leaves the lens standing', async () => {
  await mount({ onMove: () => Promise.reject(new Error('destination already exists')) })
  await go('done')
  fireEvent.click(screen.getByTestId('move'))
  await waitFor(() => expect(screen.getByTestId('move').textContent).toContain('failed'))
  expect(main().querySelector('.card')).toBeTruthy()
})
