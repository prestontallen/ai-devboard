import { render, screen } from '@testing-library/preact'
import { h } from 'preact'
import { readFileSync, readdirSync } from 'node:fs'
import { expect, test } from 'vitest'
import { Markdown, NotesFold } from '../worklog/internal/serve/static/assets/src/markdown.js'

const md = (text) => render(h(Markdown, { text })).container

test('headings become real elements, starting at h3', () => {
  const c = md('# top\n## next\n')
  expect(c.querySelector('h3').textContent).toBe('top')
  expect(c.querySelector('h4').textContent).toBe('next')
})

// The outgoing renderer emitted <h8> for six hashes, which is not an element.
test('a six-hash heading clamps to h6 instead of inventing a tag', () => {
  const c = md('###### deep\n')
  expect(c.querySelector('h6').textContent).toBe('deep')
  expect(c.querySelector('h7, h8')).toBe(null)
})

test('bullets collect into one list, and prose after them closes it', () => {
  const c = md('- one\n- two\nafter\n- three\n')
  const lists = c.querySelectorAll('ul')
  expect(lists).toHaveLength(2)
  expect([...lists[0].querySelectorAll('li')].map((l) => l.textContent)).toEqual(['one', 'two'])
  expect(c.querySelector('p').textContent).toBe('after')
})

test('bold and inline code become elements, not literal asterisks', () => {
  const c = md('a **bold** and `code` here\n')
  expect(c.querySelector('b').textContent).toBe('bold')
  expect(c.querySelector('code').textContent).toBe('code')
  expect(c.textContent).not.toContain('**')
})

// A shared /g regex keeps lastIndex between calls, which drops the first match
// on every other line. This is the test for that.
test('inline spans match on every line, not every other one', () => {
  const c = md('`one`\n`two`\n`three`\n')
  expect([...c.querySelectorAll('code')].map((e) => e.textContent)).toEqual(['one', 'two', 'three'])
})

test('a fenced block renders verbatim, markdown inside it untouched', () => {
  const c = md('before\n```\n**not bold** and `not code`\n```\nafter\n')
  const pre = c.querySelector('pre')
  expect(pre.textContent).toBe('**not bold** and `not code`')
  expect(pre.querySelector('b')).toBe(null)
  expect(c.querySelectorAll('p')).toHaveLength(2)
})

test('an unterminated fence still renders as code rather than vanishing', () => {
  const c = md('text\n```\nstill typing')
  expect(c.querySelector('pre').textContent).toBe('still typing')
})

test('markup in the source is text, never elements', () => {
  const c = md('a <script>alert(1)</script> and <b>tag</b>\n')
  expect(c.querySelector('script')).toBe(null)
  expect(c.querySelector('p b')).toBe(null)
  expect(c.textContent).toContain('<script>alert(1)</script>')
})

test('empty and missing input render nothing and do not throw', () => {
  for (const v of ['', null, undefined]) {
    expect(() => md(v)).not.toThrow()
    expect(md(v).textContent.trim()).toBe('')
  }
})

test('the notes fold is present only when there are notes', () => {
  render(h(NotesFold, { notes: '# hi', worklog: 'adb-thing' }))
  expect(screen.getByTestId('notes').textContent).toContain('adb-thing')
  expect(screen.getByTestId('notes').tagName).toBe('DETAILS')
  document.body.innerHTML = ''
  render(h(NotesFold, { notes: '' }))
  expect(screen.queryByTestId('notes')).toBe(null)
})

// The point of the port: the escaping burden is gone, not relocated.
test('no module in the app sets HTML directly', () => {
  const dir = 'worklog/internal/serve/static/assets/src'
  // Comments are stripped first: this is a claim about what the code does, and
  // a doc comment explaining why we avoid innerHTML must not read as using it.
  const code = (f) => readFileSync(`${dir}/${f}`, 'utf8')
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .replace(/(^|[^:])\/\/.*$/gm, '$1')
  const offenders = readdirSync(dir)
    .filter((f) => f.endsWith('.js'))
    .filter((f) => /dangerouslySetInnerHTML|\.innerHTML\s*=/.test(code(f)))
  expect(offenders).toEqual([])
})
