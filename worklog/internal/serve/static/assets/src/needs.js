import { h } from 'preact'
import htm from 'htm'
import { needsRows, rowHref } from './rows.js'
import { ago } from './phases.js'
import { copyText } from './clipboard.js'

const html = htm.bind(h)

/** Why approve and answer are inert: writing an answer back needs a server
 *  endpoint and CLI plumbing that do not exist yet — today's only write is
 *  archive/unarchive. adb-checkpoint-answer-endpoint fills them in without
 *  moving the controls, which is the whole point of drawing them now. */
const PENDING = 'not wired up yet — needs adb-checkpoint-answer-endpoint'

const KINDS = { checkpoint: 'checkpoint', question: 'question' }
const kindOf = (entry) => KINDS[entry.type] || 'question'

export function QueuePanel({ row, now, isDesktop }) {
  const kind = kindOf(row.entry)
  const cmd = row.session ? `claude --resume ${row.session}` : ''
  return html`
    <article class=${`qpanel ${kind}`} data-testid="qpanel" data-kind=${kind}>
      <span class=${`qkind ${kind}`}>${kind}</span>

      <div class="qwhat">
        <b class="qtext">${row.entry.text || ''}</b>
        <small class="qsub">
          <a href=${rowHref(row)}>${row.taskTitle}</a>
          ${row.childTitle ? html` · <span class="qchild">${row.childTitle}</span>` : null}
        </small>
      </div>

      ${/* Reserved slot. needs_you entries carry no timestamp today, and the
            task's mtime is right only for the most recently added item — so
            this renders nothing rather than a number that is wrong for every
            older entry. adb-needs-you-since lights it. */
        row.entry.since
          ? html`<span class="qage">${ago(row.entry.since, now)}</span>`
          : null}

      ${row.entry.detail ? html`<pre class="qdetail">${row.entry.detail}</pre>` : null}

      <div class="qcta">
        ${kind === 'checkpoint'
          ? html`<button class="cta go" type="button" disabled title=${`approve: ${PENDING}`}>✓ approve</button>`
          : null}
        <button class="cta" type="button" disabled title=${`answer: ${PENDING}`}>↳ answer</button>
        ${/* Resume works today, so it ships live — but only on desktop: a
              command copied to a phone's clipboard has no terminal to land in
              (the epic's phone rule). */
          isDesktop && cmd
            ? html`<button class="cta" type="button" data-testid="qresume"
                           title=${`copy: ${cmd}`}
                           onClick=${() => copyText(cmd)}>⧉ resume</button>`
            : null}
      </div>
    </article>`
}

export function NeedsYouLens({ db, now, isDesktop }) {
  const rows = needsRows(db)
  // Names what the lens holds rather than echoing the board's attention line:
  // two routes reading "nothing needs you" leaves you unsure which one you are
  // standing in.
  if (!rows.length) return html`<p class="calmline">✓ no questions or checkpoints waiting on you</p>`
  return html`
    <div class="qlist">
      ${rows.map((r) => html`
        <${QueuePanel} key=${`${r.repo}/${r.id}/${r.childId}/${r.entry.text}`}
                       row=${r} now=${now} isDesktop=${isDesktop} />`)}
    </div>`
}
