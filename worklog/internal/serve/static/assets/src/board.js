import { h } from 'preact'
import htm from 'htm'
import { inFlight, flatten } from './counts.js'
import { needsRows } from './rows.js'
import { TaskGrid } from './grid.js'

const html = htm.bind(h)

/** Replaces the outgoing board's fold pile: one line that either calls for
 *  attention or confirms there is none, then the grid. No folds.
 *
 *  It counts ITEMS, matching the needs-you chip and the lens it points at. A
 *  line reading "2 tasks need you" above a lens showing three panels is the
 *  unit-mixing the Lens Board set out to end. */
export function AttentionLine({ needsCount }) {
  if (needsCount > 0) {
    return html`
      <p class="attnline">▲ ${needsCount} ${needsCount === 1 ? 'item needs' : 'items need'} you —
        <a href="#/needs-you">open the needs-you lens</a></p>`
  }
  return html`<p class="calmline">✓ nothing needs you</p>`
}

export function BoardLens({ db, now, isDesktop }) {
  return html`
    <div>
      <${AttentionLine} needsCount=${needsRows(db).length} />
      <${TaskGrid} tasks=${inFlight(flatten(db))} now=${now} isDesktop=${isDesktop}
                   empty="nothing in flight" />
    </div>`
}
