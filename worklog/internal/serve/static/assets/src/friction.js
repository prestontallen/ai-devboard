import { h } from 'preact'
import htm from 'htm'
import { ago } from './phases.js'
import { copyText } from './clipboard.js'

const html = htm.bind(h)

/**
 * Friction: the global FEEDBACK.md log.
 *
 * Global rather than per-task — no task YAML field is involved. The board only
 * ever reads it: the worklog mount is read-only here, so "resolve" is a
 * copyable command, never a button that writes.
 *
 * Deliberately uncoloured. It is a review queue, not something blocking you,
 * so it must not compete with the amber needs-you signal. Entry shape is
 * devboard/API.md's: `resolved` is an int, 0 = open, never a boolean.
 */
const SIG_ORDER = ['missing-feature', 'tui-error', 'profanity', 'agent-frustration']
const rank = (s) => (SIG_ORDER.indexOf(s) + 1 || 99)
const newestFirst = (xs) => xs.slice().sort((a, b) => (b.timestamp || 0) - (a.timestamp || 0))
const stamp = (t) => (t ? new Date(t * 1000).toLocaleString() : '')

export function FrictionRow({ entry, now }) {
  const cmd = `worklog feedback resolve ${entry.timestamp}`
  return html`
    <article class=${`frrow${entry.resolved ? ' done' : ''}`} data-testid="frrow">
      <span class="frsig">${entry.signal || '?'}</span>

      <div class="qwhat">
        <b class="qtext">${entry.trigger || ''}</b>
        <small class="qsub" title=${stamp(entry.timestamp)}>
          ${ago(entry.timestamp, now)}
          ${entry.resolved ? html` · reviewed ${ago(entry.resolved, now)}` : null}
        </small>
      </div>

      ${entry.resolved
        ? null
        : html`<div class="qcta">
            <button class="cta" type="button" data-testid="frcopy" title=${`copy: ${cmd}`}
                    onClick=${() => copyText(cmd)}>⧉ resolve cmd</button>
          </div>`}

      ${entry.excerpt ? html`<pre class="qdetail">${entry.excerpt}</pre>` : null}
      ${entry.context ? html`<div class="fnote">${entry.context}</div>` : null}
    </article>`
}

export function FrictionLens({ db, now }) {
  const all = ((db && db.feedback) || []).filter(Boolean)
  const open = all.filter((e) => !e.resolved)
  const done = all.filter((e) => e.resolved)
  if (!all.length) return html`<p class="calmline">no friction captured</p>`

  const signals = [...new Set(open.map((e) => e.signal))].sort((a, b) => rank(a) - rank(b))
  return html`
    <div class="frlist">
      ${signals.length
        ? html`<div class="sigcounts">
            ${signals.map((sig) => html`
              <span class="sigcount" key=${sig}>${sig} <b>${open.filter((e) => e.signal === sig).length}</b></span>`)}
          </div>`
        : null}

      ${open.length
        ? newestFirst(open).map((e) => html`<${FrictionRow} key=${e.timestamp} entry=${e} now=${now} />`)
        : html`<p class="calmline">✓ all captured friction reviewed</p>`}

      ${done.length
        ? html`<div class="frdone">
            <div class="frdonehead">resolved · ${done.length}</div>
            ${newestFirst(done).map((e) => html`<${FrictionRow} key=${e.timestamp} entry=${e} now=${now} />`)}
          </div>`
        : null}
    </div>`
}
