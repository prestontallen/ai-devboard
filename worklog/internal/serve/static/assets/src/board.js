import { h } from 'preact'
import htm from 'htm'
import { inFlight, flatten, needsYou } from './counts.js'
import { Card } from './card.js'

const html = htm.bind(h)

/** Replaces the outgoing board's fold pile: one line that either calls for
 *  attention or confirms there is none, then the grid. No folds. */
export function AttentionLine({ needsCount }) {
  if (needsCount > 0) {
    return html`
      <p class="attnline">▲ ${needsCount} ${needsCount === 1 ? 'task needs' : 'tasks need'} you —
        <a href="#/needs-you">open the needs-you lens</a></p>`
  }
  return html`<p class="calmline">✓ nothing needs you</p>`
}

export function BoardLens({ db, now, isDesktop }) {
  const tasks = inFlight(flatten(db))
  const needsCount = tasks.filter((t) => needsYou(t).length > 0).length
  return html`
    <div>
      <${AttentionLine} needsCount=${needsCount} />
      ${tasks.length === 0
        ? html`<p class="calmline">nothing in flight</p>`
        : html`<div class="grid">
            ${tasks.map((t) => html`
              <${Card} key=${`${t.repo}/${t.id}`} task=${t} now=${now} isDesktop=${isDesktop} />`)}
          </div>`}
    </div>`
}
