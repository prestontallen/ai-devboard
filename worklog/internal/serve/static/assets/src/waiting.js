import { h } from 'preact'
import htm from 'htm'
import { waitRows, askedDays, byAsked } from './rows.js'
import { safeHref } from './clipboard.js'
import { hashForTask } from './routes.js'

const html = htm.bind(h)

/** Blue, never amber. This queue is blocked on someone ELSE — it is a status,
 *  not a summons, and it must not compete with needs-you for your eye. */
const age = (days) => (days === null ? '' : days === 0 ? 'today' : `${days}d`)

/** A task file is hand-editable, so `link` is untrusted input on its way to an
 *  href. A rejected scheme still shows — silently dropping the value would
 *  hide that the entry has a link at all — but as text that cannot be
 *  clicked. */
function Asked({ link }) {
  if (!link) return null
  const href = safeHref(link)
  return href
    ? html`<a class="qlink" href=${href} target="_blank" rel="noopener">↗ asked</a>`
    : html`<span class="qlink dead" title="link scheme refused">↗ asked</span>`
}

export function WaitRow({ row, now }) {
  const days = askedDays(row.entry.asked, now)
  return html`
    <article class="qpanel wait" data-testid="waitrow">
      <span class="qkind wait">${row.entry.who || '?'}</span>

      <div class="qwhat">
        <b class="qtext">${row.entry.text || ''}</b>
        <small class="qsub">
          <a href=${row.childId ? `/#${row.repo}/${row.id}/${row.childId}` : hashForTask(row.repo, row.id)}>
            ${row.taskTitle}
          </a>
          ${row.childTitle ? html` · <span class="qchild">${row.childTitle}</span>` : null}
        </small>
      </div>

      ${days === null ? null : html`<span class="qage">${age(days)}</span>`}
      <div class="qcta"><${Asked} link=${row.entry.link} /></div>
    </article>`
}

export function WaitingLens({ db, now }) {
  // Longest-waiting first: the whole point of the lens is spotting the thread
  // that has gone quiet, which is the oldest one, not the newest.
  const rows = waitRows(db).sort(byAsked(now))
  if (!rows.length) return html`<p class="calmline">✓ waiting on no one</p>`
  return html`
    <div class="qlist">
      ${rows.map((r) => html`
        <${WaitRow} key=${`${r.repo}/${r.id}/${r.childId}/${r.entry.text}`} row=${r} now=${now} />`)}
    </div>`
}
