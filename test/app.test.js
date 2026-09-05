import { act, render, screen } from '@testing-library/preact'
import { h } from 'preact'
import { expect, test, vi } from 'vitest'
import { DB, NOW } from './fixture.js'
import { App } from '../worklog/internal/serve/static/assets/src/app.js'

const load = () => Promise.resolve(DB)
const desktop = () => false
const phone = () => true

async function mount(props = {}) {
  const r = render(h(App, { load, now: NOW, matchPhone: desktop, ...props }))
  await settle()
  return r
}

// hashchange is dispatched on a macrotask, in happy-dom as in a real browser,
// so flushing microtasks alone leaves the router a beat behind.
const settle = () => act(async () => { await new Promise((r) => setTimeout(r, 0)) })

const chip = (lens) => document.querySelector(`.chip[href="#/${lens}"]`)
const lensName = () => screen.getByTestId('lens').dataset.lens

test('the shell mounts and renders with EventSource undefined', async () => {
  expect(typeof globalThis.EventSource).toBe('undefined')
  await mount()
  expect(lensName()).toBe('board')
})

test('exactly six chips render, in bar order, with no backlog chip', async () => {
  await mount()
  const labels = [...document.querySelectorAll('.chip')].map((c) => c.getAttribute('href'))
  expect(labels).toEqual(['#/board', '#/needs-you', '#/waiting', '#/friction', '#/done', '#/archived'])
  expect(chip('backlog')).toBe(null)
})

test('a zero-count chip stays in the DOM and dims', async () => {
  await mount({ load: () => Promise.resolve({ repos: [], feedback: [] }) })
  for (const lens of ['board', 'needs-you', 'waiting', 'friction', 'done', 'archived']) {
    const el = chip(lens)
    expect(el, `${lens} chip must not be removed`).toBeTruthy()
    expect(el.className).toContain('zero')
  }
})

test('clicking a chip moves the address bar and the active marker', async () => {
  await mount()
  await act(async () => { chip('waiting').click() })
  await settle()
  expect(location.hash).toBe('#/waiting')
  expect(chip('waiting').getAttribute('aria-current')).toBe('page')
  expect(chip('board').hasAttribute('aria-current')).toBe(false)
  expect(lensName()).toBe('waiting')
})

// back/forward and a pasted link both arrive as a hashchange, which is exactly
// why the route is derived from the hash rather than held in state.
test('an external hash change drives the lens', async () => {
  await mount()
  await act(async () => { location.hash = '#/friction' })
  await settle()
  expect(lensName()).toBe('friction')
})

test('a legacy deep link resolves to the default lens instead of throwing', async () => {
  location.hash = '#ai-devboard/adb-lens-router'
  await mount()
  expect(lensName()).toBe('board')
})

test('the injected transport drives the connection indicator', async () => {
  let handlers
  const transport = (h2) => { handlers = h2; return () => {} }
  await mount({ transport })
  expect(screen.getByTestId('conn').textContent).toBe('offline')
  await act(async () => handlers.onStatus('live'))
  expect(screen.getByTestId('conn').textContent).toBe('live')
  await act(async () => handlers.onStatus('reconnecting'))
  expect(screen.getByTestId('conn').textContent).toBe('reconnecting')
})

test('the board lists in-flight tasks and shows the amber attention line', async () => {
  await mount()
  expect(document.querySelector('.attnline')).toBeTruthy()
  expect(document.querySelector('.calmline')).toBe(null)
  expect(document.querySelectorAll('.card')).toHaveLength(4)
})

test('with nothing needing you the board shows the calm line instead', async () => {
  const calm = { feedback: [], repos: [{ repo: 'r', tasks: [
    { id: 'x', mtime: NOW / 1000, task: { title: 'quiet', phase: 'implement' } },
  ] }] }
  await mount({ load: () => Promise.resolve(calm) })
  expect(document.querySelector('.calmline')).toBeTruthy()
  expect(document.querySelector('.attnline')).toBe(null)
})

test('a malformed task renders as an error card rather than disappearing', async () => {
  await mount()
  const err = document.querySelector('.card.err')
  expect(err).toBeTruthy()
  expect(err.textContent).toContain('ai-devboard/broken.yaml')
  expect(err.textContent).toContain('unclosed') // the raw parse error, not a label
})

// Amended 2026-09-05: the real card marks staleness the way / does — card
// dimming, an aged dot, and "— stale" in the age line. The .badge.stale this
// once asserted belonged to the placeholder card, now deleted.
test('stale shows as card dimming and an aged dot, never as a chip', async () => {
  await mount()
  const stale = [...document.querySelectorAll('.card.stale')]
  expect(stale).toHaveLength(1)
  expect(stale[0].querySelector('.dot.old')).toBeTruthy()
  expect(stale[0].textContent).toContain('— stale')
  expect(chip('stale')).toBe(null)
})

test('on a phone with work waiting, the first payload opens needs-you', async () => {
  await mount({ matchPhone: phone })
  expect(location.hash).toBe('#/needs-you')
})

test('an explicit hash survives the phone rule', async () => {
  location.hash = '#/archived'
  await mount({ matchPhone: phone })
  expect(location.hash).toBe('#/archived')
  expect(lensName()).toBe('archived')
})

// The rule reads a count that only exists after the first payload, so it has
// to run there — but running on every SSE tick would yank the user back.
test('a later payload does not re-route the user', async () => {
  let handlers
  const transport = (h2) => { handlers = h2; return () => {} }
  const load2 = vi.fn(() => Promise.resolve(DB))
  await mount({ matchPhone: phone, transport, load: load2 })
  expect(location.hash).toBe('#/needs-you')

  await act(async () => { chip('done').click() })
  await settle()
  expect(location.hash).toBe('#/done')

  await act(async () => { await handlers.onChange() })
  expect(load2.mock.calls.length).toBeGreaterThan(1)
  expect(location.hash).toBe('#/done')
})
