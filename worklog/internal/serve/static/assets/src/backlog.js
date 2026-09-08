import { h } from 'preact'
import htm from 'htm'
import { backlogSections } from './counts.js'

const html = htm.bind(h)

/**
 * The backlog lens: WORK.md's Next and Someday.
 *
 * A backlog item is NOT a task. It has no phase, no plan, no scorecard, no
 * mtime and no task file — it is a WORK.md block. So it does not go through
 * the card renderer: a card would draw an empty ten-segment track labelled
 * "no phase", which is how a missing field reads, not how "not started yet"
 * reads. needs-you already renders panels rather than cards for the same
 * reason. The invariant is one CARD renderer, not that everything is a card.
 *
 * Nothing here is a link. There is no task file behind a backlog item, so
 * there is no detail route to open, and a row that looks clickable and is not
 * is worse than a row that does not. That also settles what to do with a
 * block's `**Link**:` entries: they are not rendered, so no WORK.md-supplied
 * URL ever reaches an href. If they are ever wanted, they go through
 * `safeHref` like every other task-supplied URL.
 */

const list = (x) => (Array.isArray(x) ? x : [])

/** Only epic and spike earn a badge — `ticket` and `chore` are what an
 *  ordinary item already is, exactly as the card's TypeBadge decides it. */
function TypeBadge({ type }) {
  if (type !== 'epic' && type !== 'spike') return null
  return type === 'epic'
    ? html`<span class="badge epic">◆ epic</span>`
    : html`<span class="badge spike">◇ spike</span>`
}

/**
 * One row. Every part below the title is omitted rather than emptied when the
 * field is absent: an empty tag strip or a bare "done when:" reads as data
 * that went missing, when the truth is it was never written.
 */
export function BacklogRow({ item }) {
  const tags = list(item.tags).filter(Boolean)
  return html`
    <li class="brow" data-testid="brow" data-id=${item.id || ''}>
      <div class="bhead">
        <span class="btitle">${item.title || item.id || 'untitled'}</span>
        <${TypeBadge} type=${item.type} />
        ${item.repo ? html`<span class="badge repo">${item.repo}</span>` : null}
      </div>
      ${tags.length ? html`<div class="btags">${tags.join(' · ')}</div>` : null}
      ${item.acceptance
        ? html`<div class="bacc"><span class="k">done when</span>${item.acceptance}</div>`
        : null}
    </li>`
}

/** A section renders even when empty, so "nothing queued here" is visibly
 *  different from a section the payload never carried. */
export function BacklogGroup({ section }) {
  const items = list(section.items)
  return html`
    <section class="bgroup" data-testid="bgroup" data-section=${section.name}>
      <h3>${section.name}<span class="count">${items.length}</span></h3>
      ${items.length
        ? html`<ul class="brows">
            ${items.map((item, i) => html`<${BacklogRow} key=${item.id || i} item=${item} />`)}
          </ul>`
        : html`<p class="calmline">nothing queued here</p>`}
    </section>`
}

export function BacklogLens({ db }) {
  const sections = backlogSections(db)
  // No sections at all means a server older than this lens, not an empty
  // backlog — and those are different things to say.
  if (!sections.length) return html`<p class="calmline">no backlog reported by the server</p>`
  return html`
    <div class="backlog">
      ${sections.map((s) => html`<${BacklogGroup} key=${s.name} section=${s} />`)}
    </div>`
}
