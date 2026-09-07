import { h } from 'preact'
import htm from 'htm'
import { itemText, ago } from './phases.js'
import { safeHref } from './clipboard.js'
import { askedDays } from './rows.js'

const html = htm.bind(h)

/**
 * The record: everything below the ledger.
 *
 * These are evidence, not state — what happened and why, newest thinking last.
 * Each renders only when it has something to say; an empty section is worse
 * than no section, because it reads as "nothing happened here" when the truth
 * is "this was never recorded".
 */

const list = (x) => (Array.isArray(x) ? x : [])

function Section({ title, count, tone, children }) {
  return html`
    <section class=${`sec${tone ? ` ${tone}` : ''}`} data-testid="section" data-sec=${title}>
      <h3>${title}${count === undefined ? null : html` <span class="count">${count}</span>`}</h3>
      ${children}
    </section>`
}

/** The task's own attention queue, shown here in full: the needs-you lens
 *  carries the same entries, but a detail view is where you come to read the
 *  detail body rather than act on the summons. */
export function Queues({ task }) {
  const k = task.task || {}
  const needs = list(k.needs_you)
  const waits = list(k.waiting_on)
  if (!needs.length && !waits.length) return null
  return html`
    <div>
      ${needs.length ? html`
        <${Section} title="Needs you" count=${needs.length} tone="warn">
          ${needs.map((raw, i) => {
            const n = itemText(raw)
            const kind = n.type === 'checkpoint' ? 'checkpoint' : 'question'
            return html`
              <div class="qpanel" key=${i} data-testid="detail-need">
                <span class=${`qkind ${kind}`}>${kind}</span>
                <div class="qwhat"><b class="qtext">${n.text || ''}</b></div>
                ${n.detail ? html`<pre class="qdetail">${n.detail}</pre>` : null}
              </div>`
          })}
        <//>` : null}

      ${waits.length ? html`
        <${Section} title="Waiting on" count=${waits.length}>
          ${waits.map((raw, i) => {
            const w = itemText(raw)
            const days = askedDays(w.asked)
            const href = w.link ? safeHref(w.link) : null
            return html`
              <div class="qpanel wait" key=${i} data-testid="detail-wait">
                <span class="qkind wait">${w.who || '?'}</span>
                <div class="qwhat"><b class="qtext">${w.text || ''}</b></div>
                ${days === null ? null : html`<span class="qage">${days === 0 ? 'today' : `${days}d`}</span>`}
                <div class="qcta">
                  ${w.link && href
                    ? html`<a class="qlink" href=${href} target="_blank" rel="noopener">↗ asked</a>`
                    : w.link ? html`<span class="qlink dead" title="link scheme refused">↗ asked</span>` : null}
                </div>
                ${w.detail ? html`<pre class="qdetail">${w.detail}</pre>` : null}
              </div>`
          })}
        <//>` : null}
    </div>`
}

/**
 * The scout gate. Unlike every other section, this one renders on ABSENCE:
 * medium or high complexity with no scout attested is itself the finding, and
 * nothing else on the page would report it.
 */
const GATED = new Set(['medium', 'high'])

export function RiskScout({ task }) {
  const k = task.task || {}
  const scout = k.scout
  const owed = GATED.has(String(k.complexity || '').toLowerCase())
  if (!scout || !scout.mode) {
    if (!owed) return null
    return html`
      <${Section} title="Risk scout">
        <div class="tl">
          <div class="ev" data-testid="scout-missing">
            <b>not attested</b>
            <small>${k.complexity}-complexity work with no scout record —
              worklog task scout ran|inline|skipped --why "&lt;why&gt;"</small>
          </div>
        </div>
      <//>`
  }
  return html`
    <${Section} title="Risk scout">
      <div class="tl">
        <div class="ev" data-testid="scout">
          <b>${scout.mode}</b>
          ${scout.why ? html`<small>${scout.why}</small>` : null}
          ${scout.when ? html`<span class="when">${scout.when}</span>` : null}
        </div>
      </div>
    <//>`
}

export function Decisions({ task }) {
  const items = list((task.task || {}).decisions)
  if (!items.length) return null
  return html`
    <${Section} title="Decisions" count=${items.length}>
      <div class="tl">
        ${items.map((raw, i) => {
          const d = itemText(raw)
          return html`
            <div class="ev" key=${i} data-testid="decision">
              <b>${d.what || d.text || ''}</b>
              ${d.why ? html`<small>${d.why}</small>` : null}
              ${d.complexity ? html`<small>complexity: ${d.complexity}</small>` : null}
              ${d.when ? html`<span class="when">${d.when}</span>` : null}
            </div>`
        })}
      </div>
    <//>`
}

/** Snippets stay folded — they are reference, and an unfolded one pushes the
 *  rest of the record off the screen. Rendered as plain monospace: the
 *  outgoing highlighter is a regex keyword painter feeding innerHTML, and
 *  colour is not worth reopening that. */
export function CodeToKnow({ task }) {
  const items = list((task.task || {}).code)
  if (!items.length) return null
  return html`
    <${Section} title="Code to know" count=${items.length}>
      ${items.map((c, i) => html`
        <div class="codeblock" key=${i} data-testid="code">
          <span class="path">${c.file || ''}${c.lines ? `:${c.lines}` : ''}</span>
          ${c.lang ? html`<span class="lang">${c.lang}</span>` : null}
          ${c.note ? html`<div class="cnote">${c.note}</div>` : null}
          ${c.snippet
            ? html`<details class="fold"><summary>snippet</summary><pre>${c.snippet}</pre></details>`
            : null}
        </div>`)}
    <//>`
}

export function Links({ task }) {
  const items = list((task.task || {}).links)
  if (!items.length) return null
  return html`
    <${Section} title="Links">
      <ul class="plain">
        ${items.map((raw, i) => {
          const l = itemText(raw)
          const label = l.label || l.url || l.text || ''
          const href = safeHref(l.url)
          return html`
            <li key=${i} data-testid="link">
              <span class="mk">↗</span>
              ${href
                ? html`<a href=${href} target="_blank" rel="noopener">${label}</a>`
                : html`<span class="dead" title="link scheme refused">${label}</span>`}
            </li>`
        })}
      </ul>
    <//>`
}

/**
 * Unknown top-level keys.
 *
 * The payload is additive by contract — new keys may appear at any time and
 * consumers must ignore what they don't know (API.md). Rendering them anyway
 * is how a schema addition stays visible instead of silently invisible until
 * someone teaches the UI about it.
 */
export const KNOWN = new Set([
  'schema', 'title', 'branch', 'tier', 'phase', 'plan', 'scorecard', 'decisions',
  'code', 'needs_you', 'waiting_on', 'links', 'session', 'worklog', 'complexity',
  'type', 'children', 'repo_path', 'scout',
])

export function Other({ task }) {
  const k = task.task || {}
  const extra = Object.keys(k).filter((key) => !KNOWN.has(key))
  if (!extra.length) return null
  return html`
    <${Section} title="Other">
      <table class="kv">
        <tbody>
          ${extra.map((key) => html`
            <tr key=${key} data-testid="other-row">
              <td>${key}</td>
              <td>${typeof k[key] === 'object' ? JSON.stringify(k[key], null, 1) : String(k[key])}</td>
            </tr>`)}
        </tbody>
      </table>
    <//>`
}

export function Record({ task }) {
  return html`
    <div class="record">
      <${Queues} task=${task} />
      <${RiskScout} task=${task} />
      <${Decisions} task=${task} />
      <${CodeToKnow} task=${task} />
      <${Links} task=${task} />
      <${Other} task=${task} />
    </div>`
}

export { ago }
