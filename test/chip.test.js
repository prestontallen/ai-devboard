import { readFileSync } from 'node:fs'
import { render, screen } from '@testing-library/preact'
import { h } from 'preact'
import { expect, test } from 'vitest'
import { Chip } from '../worklog/internal/serve/static/assets/src/chip.js'

const chip = (props) => render(h(Chip, props)).container.querySelector('.chip')

test('renders its label and count', () => {
  chip({ label: 'needs you', count: 2, route: 'needs-you', tone: 'attn' })
  expect(screen.getByText('needs you')).toBeTruthy()
  expect(screen.getByText('2')).toBeTruthy()
})

// The behavior the redesign turns on its head: today's board drops a
// zero-count chip with a ternary, so an empty queue and an untracked one
// look identical. The chip must stay put and dim.
test('a zero count dims instead of disappearing', () => {
  const el = chip({ label: 'friction', count: 0, route: 'friction' })
  expect(el).toBeTruthy()
  expect(el.className).toContain('zero')
  expect(screen.getByText('friction')).toBeTruthy()
  expect(screen.getByText('0')).toBeTruthy()
})

test('a non-zero count is not dimmed', () => {
  expect(chip({ label: 'done', count: 29, route: 'done' }).className).not.toContain('zero')
})

test('the active chip is the one marked aria-current', () => {
  expect(chip({ label: 'board', count: 4, route: 'board', active: true }).getAttribute('aria-current')).toBe('page')
})

test('an inactive chip carries no aria-current at all', () => {
  expect(chip({ label: 'board', count: 4, route: 'board' }).hasAttribute('aria-current')).toBe(false)
})

// Tone is the kept color semantics, not decoration — amber for needs-you,
// blue for waiting, uncoloured for the rest.
test('tone reaches the class list, and neutral is the default', () => {
  expect(chip({ label: 'needs you', count: 2, route: 'needs-you', tone: 'attn' }).className).toContain('attn')
  expect(chip({ label: 'waiting', count: 1, route: 'waiting', tone: 'wait' }).className).toContain('wait')
  expect(chip({ label: 'backlog', count: 9, route: 'backlog' }).className).toContain('neutral')
})

test('each chip links to its own route', () => {
  expect(chip({ label: 'archived', count: 3, route: 'archived' }).getAttribute('href')).toBe('#/archived')
})

// The npm-installed Preact is what these tests exercise; the vendored file
// is what ships. If they drift, the suite silently stops testing the code
// the browser runs.
test('the tested Preact is the version that is vendored', async () => {
  const installed = JSON.parse(readFileSync('node_modules/preact/package.json', 'utf8')).version
  const readme = readFileSync('worklog/internal/serve/static/assets/vendor/README.md', 'utf8')
  expect(readme).toContain(`| ${installed} |`)
})
