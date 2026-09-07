import { h } from 'preact'
import htm from 'htm'

const html = htm.bind(h)

/**
 * Minimal markdown for worklog notes: headings, bullets, bold, inline code,
 * fenced blocks. Everything else renders as its own paragraph text.
 *
 * This returns Preact nodes, not an HTML string. The outgoing board builds
 * markup by concatenation and hand-escapes at ~90 call sites; one missed
 * `esc()` there is an injection from a file on disk. Nodes make the escaping
 * the renderer's job instead of the author's, which is the whole reason the
 * app moved to Preact — porting the string version behind
 * dangerouslySetInnerHTML would have relocated the hazard, not removed it.
 */

/** Inline spans. A fresh regex per call: a shared /g one carries `lastIndex`
 *  between calls and silently skips the first match of every other line. */
function inline(text) {
  const re = /`([^`]+)`|\*\*([^*]+)\*\*/g
  const out = []
  let last = 0
  let m
  while ((m = re.exec(text))) {
    if (m.index > last) out.push(text.slice(last, m.index))
    out.push(m[1] !== undefined ? html`<code>${m[1]}</code>` : html`<b>${m[2]}</b>`)
    last = m.index + m[0].length
  }
  if (last < text.length) out.push(text.slice(last))
  return out.length ? out : ['']
}

/** Notes are rendered inside a page that already owns h1 and h2, so a note's
 *  `#` starts at h3. Capped at h6: the outgoing renderer emitted `<h8>` for a
 *  six-hash heading, which is not an element. */
const heading = (hashes, body) => {
  const level = Math.min(hashes.length + 2, 6)
  return html`<${`h${level}`}>${inline(body)}<//>`
}

function prose(part, keyBase) {
  const out = []
  let bullets = null
  const flush = () => {
    if (bullets) out.push(html`<ul key=${`${keyBase}-ul-${out.length}`}>${bullets}</ul>`)
    bullets = null
  }
  part.split('\n').forEach((line, i) => {
    const key = `${keyBase}-${i}`
    const li = /^\s*[-*]\s+(.*)/.exec(line)
    if (li) {
      bullets = bullets || []
      bullets.push(html`<li key=${key}>${inline(li[1])}</li>`)
      return
    }
    flush()
    const hd = /^(#{1,6})\s+(.*)/.exec(line)
    if (hd) out.push(heading(hd[1], hd[2]))
    else if (line.trim()) out.push(html`<p key=${key}>${inline(line)}</p>`)
  })
  flush()
  return out
}

/**
 * Fences split the source into alternating prose and code. An unterminated
 * fence leaves a final odd part, which still renders as code — the same
 * forgiving behavior the outgoing renderer had, and the right one for a notes
 * file someone is still typing into.
 */
export function parseMarkdown(src) {
  return String(src ?? '')
    .split(/^```.*$/m)
    .flatMap((part, i) =>
      i % 2
        ? [html`<pre key=${`f${i}`}>${part.replace(/^\n|\n$/g, '')}</pre>`]
        : prose(part, `p${i}`))
}

export function Markdown({ text }) {
  return html`<div class="mdbody">${parseMarkdown(text)}</div>`
}

/** Notes stay folded. The board's folds died with adb-lens-router, but detail
 *  keeps them: a notes file runs to thousands of words and is reference
 *  material, not something you read on the way past. */
export function NotesFold({ notes, worklog }) {
  if (!notes) return null
  return html`
    <details class="fold notes" data-testid="notes">
      <summary>Worklog notes${worklog ? ` · ${worklog}` : ''}</summary>
      <${Markdown} text=${notes} />
    </details>`
}
