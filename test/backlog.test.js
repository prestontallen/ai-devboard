import { render } from '@testing-library/preact'
import { h } from 'preact'
import { expect, test } from 'vitest'
import { BacklogLens } from '../worklog/internal/serve/static/assets/src/backlog.js'
import { lensCounts, backlogItems } from '../worklog/internal/serve/static/assets/src/counts.js'
import { LENSES } from '../worklog/internal/serve/static/assets/src/routes.js'
import { EMPTY } from './fixture.js'

/** Shaped to the server's backlog key: a section per WORK.md section, each
 *  carrying model.Block items. The bare item and the javascript: link are the
 *  two that keep the row honest. */
const DB = {
  repos: [],
  feedback: [],
  backlog: [
    {
      name: 'Next',
      items: [
        {
          id: 'nole-docker-net',
          title: 'Fix Docker container internet access for nole embedding stack',
          repo: 'prestontallen/nole',
          tags: ['nole', 'docker', 'ollama'],
          acceptance: 'ollama pull qwen3-embedding:4b succeeds inside the container',
        },
        { id: 'an-epic', title: 'A cross-cutting effort', type: 'epic', repo: 'r' },
        { id: 'a-spike', title: 'Research something', type: 'spike', repo: 'r' },
        // Everything optional absent: the row must omit, not empty.
        { id: 'bare', title: 'Only an id and a title' },
        // A WORK.md block can carry links, and a hand-edited one can carry
        // any scheme. Nothing in this lens may turn one into an href.
        {
          id: 'linked', title: 'Has links', repo: 'r',
          links: [{ name: 'sneaky', url: 'javascript:alert(1)' },
            { name: 'jira', url: 'https://example.test/x' }],
        },
      ],
    },
    { name: 'Someday', items: [{ id: 'csk-onboarding', title: 'Onboarding', tags: [] }] },
  ],
}

const draw = (db) => render(h(BacklogLens, { db })).container
const rows = (c) => [...c.querySelectorAll('[data-testid=brow]')]
const row = (c, id) => c.querySelector(`[data-testid=brow][data-id="${id}"]`)

// ---------- the chip ----------

test('backlog is a lens, in bar order between friction and done', () => {
  expect(LENSES).toContain('backlog')
  expect(LENSES.indexOf('backlog')).toBe(LENSES.indexOf('friction') + 1)
  expect(LENSES.indexOf('backlog')).toBe(LENSES.indexOf('done') - 1)
})

test('the chip counts every item the lens draws, across both sections', () => {
  expect(lensCounts(DB).backlog).toBe(6)
  expect(lensCounts(DB).backlog).toBe(rows(draw(DB)).length)
  expect(backlogItems(DB)).toHaveLength(6)
})

test('the chip reads 0 on an empty backlog and on a payload without one', () => {
  expect(lensCounts({ ...DB, backlog: [{ name: 'Next', items: [] }] }).backlog).toBe(0)
  expect(lensCounts(EMPTY).backlog).toBe(0)
  expect(lensCounts({}).backlog).toBe(0)
})

// ---------- the rows ----------

test('both sections render as groups, each counting its own items', () => {
  const groups = [...draw(DB).querySelectorAll('[data-testid=bgroup]')]
  expect(groups.map((g) => g.dataset.section)).toEqual(['Next', 'Someday'])
  expect(groups[0].querySelector('h3').textContent).toContain('5')
  expect(groups[1].querySelector('h3').textContent).toContain('1')
})

test('a row carries title, type, repo, tags and acceptance', () => {
  const c = draw(DB)
  const r = row(c, 'nole-docker-net')
  expect(r.querySelector('.btitle').textContent).toContain('Fix Docker container')
  expect(r.querySelector('.badge.repo').textContent).toBe('prestontallen/nole')
  expect(r.querySelector('.btags').textContent).toBe('nole · docker · ollama')
  expect(r.querySelector('.bacc').textContent).toContain('ollama pull qwen3-embedding:4b')
  expect(row(c, 'an-epic').querySelector('.badge.epic')).toBeTruthy()
  expect(row(c, 'a-spike').querySelector('.badge.spike')).toBeTruthy()
})

test('a row omits what it lacks rather than rendering it empty', () => {
  const r = row(draw(DB), 'bare')
  expect(r.querySelector('.btitle').textContent).toBe('Only an id and a title')
  for (const sel of ['.badge.repo', '.btags', '.bacc', '.badge.epic', '.badge.spike']) {
    expect(r.querySelector(sel)).toBeNull()
  }
  // An empty tags array is not a tag strip either.
  expect(row(draw(DB), 'csk-onboarding').querySelector('.btags')).toBeNull()
})

// ---------- what a backlog row must never become ----------

test('no row is a link, and no WORK.md url reaches an href', () => {
  const c = draw(DB)
  expect(c.querySelectorAll('a')).toHaveLength(0)
  expect(c.querySelectorAll('[href]')).toHaveLength(0)
  expect(c.innerHTML).not.toContain('javascript:')
  expect(c.innerHTML).not.toContain('example.test')
})

// The guard against the row accreting into a card. There is no phase, plan or
// scorecard behind a backlog item, so drawing any of their furniture would be
// inventing data.
test('no row renders a track, meter or pip', () => {
  const c = draw(DB)
  for (const sel of ['.track', '.meter', '.pip', '.phase-lbl', '.card']) {
    expect(c.querySelector(sel)).toBeNull()
  }
})

// ---------- sad paths ----------

test('an empty section says so instead of rendering a blank group', () => {
  const c = draw({ ...DB, backlog: [{ name: 'Next', items: [] }, { name: 'Someday', items: [] }] })
  expect(rows(c)).toHaveLength(0)
  expect([...c.querySelectorAll('.calmline')].map((e) => e.textContent))
    .toEqual(['nothing queued here', 'nothing queued here'])
})

test('a payload with no backlog key says the server did not report one', () => {
  // Distinct from an empty backlog: an older server is not an empty queue.
  const c = draw(EMPTY)
  expect(c.querySelector('.calmline').textContent).toContain('no backlog reported')
})

test('a block with no title falls back to its id, then to a placeholder', () => {
  const c = draw({ backlog: [{ name: 'Next', items: [{ id: 'only-id' }, {}] }] })
  const titles = rows(c).map((r) => r.querySelector('.btitle').textContent)
  expect(titles).toEqual(['only-id', 'untitled'])
})

test('malformed sections and items do not take the lens down', () => {
  for (const db of [
    { backlog: null }, { backlog: [null, { name: '' }] },
    { backlog: [{ name: 'Next', items: null }] },
    { backlog: [{ name: 'Next', items: [{ id: 'x', tags: 'not-an-array' }] }] },
  ]) {
    expect(() => draw(db)).not.toThrow()
  }
})
