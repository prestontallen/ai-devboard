import { h } from 'preact'
import htm from 'htm'
import { Card } from './card.js'
import { byRecency } from './rows.js'

const html = htm.bind(h)

/**
 * The card grid, shared by every lens that shows cards.
 *
 * One grid and one card: Board, Done and Archived differ in which tasks they
 * are handed and whether a move control comes with them, never in how a card
 * is drawn. Cards key on repo+id because `id` is only the filename stem, so
 * two repos can hold the same one.
 *
 * `order` is the one seam, and it exists for exactly one caller: an epic's
 * children all share the epic file's mtime, so `byRecency` would be a constant
 * across that grid — sorting nothing while looking like it sorted something.
 * Every lens leaves it unset and keeps recency.
 */
export function TaskGrid({ tasks, now, isDesktop, onMove, order = byRecency, empty = 'nothing here' }) {
  if (!tasks.length) return html`<p class="calmline">${empty}</p>`
  return html`
    <div class="grid">
      ${tasks.slice().sort(order).map((t) => html`
        <${Card} key=${`${t.repo}/${t.id}`} task=${t} now=${now}
                 isDesktop=${isDesktop} onMove=${onMove} />`)}
    </div>`
}
