import { h } from 'preact'
import htm from 'htm'
import { Card } from './card.js'
import { byRecency } from './rows.js'

const html = htm.bind(h)

/**
 * The card grid, shared by every lens that shows cards.
 *
 * One grid and one order: Board, Done and Archived differ in which tasks they
 * are handed and whether a move control comes with them, never in how a card
 * is drawn or sorted. Cards key on repo+id because `id` is only the filename
 * stem, so two repos can hold the same one.
 */
export function TaskGrid({ tasks, now, isDesktop, onMove, empty = 'nothing here' }) {
  if (!tasks.length) return html`<p class="calmline">${empty}</p>`
  return html`
    <div class="grid">
      ${tasks.slice().sort(byRecency).map((t) => html`
        <${Card} key=${`${t.repo}/${t.id}`} task=${t} now=${now}
                 isDesktop=${isDesktop} onMove=${onMove} />`)}
    </div>`
}
