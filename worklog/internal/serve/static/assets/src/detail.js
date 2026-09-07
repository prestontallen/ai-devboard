import { h } from 'preact'
import htm from 'htm'
import { flatten, isError, isArchived, isEpic } from './counts.js'
import { phasesFor, phaseIndex, ago } from './phases.js'
import { copyText } from './clipboard.js'
import { MoveButton } from './archive.js'
import { Ledger } from './ledger.js'
import { Record } from './sections.js'
import { NotesFold } from './markdown.js'
import { hashForLens } from './routes.js'

const html = htm.bind(h)

export const findTask = (db, repo, id) =>
  flatten(db).find((t) => t.repo === repo && t.id === id) || null

/** The card's track compressed a phase list into bare segments because a card
 *  has no room for labels. Detail does, and the labels are the point: this is
 *  where you come to read where the work actually is. */
export function Stepper({ task }) {
  const k = task.task || {}
  const steps = phasesFor(k.type, k.phase)
  const i = phaseIndex(k.type, k.phase)
  return html`
    <div>
      <div class="stepper" style=${`--steps:${steps.length}`} data-testid="stepper">
        ${steps.map((p, n) => html`
          <div class=${`sstep ${i < 0 ? '' : n < i ? 'on' : n === i ? 'now' : ''}`} key=${p}>
            <i /><span>${p}</span>
          </div>`)}
      </div>
      ${i < 0
        ? html`<div class="stepper-note">${k.phase ? `${k.phase} — not a known phase` : 'no phase set'}</div>`
        : null}
    </div>`
}

function Chips({ task, now }) {
  const k = task.task || {}
  const cmd = k.session ? `claude --resume ${k.session}` : ''
  const tier = k.tier === undefined || k.tier === null ? '' : `tier ${k.tier}`
  const cx = k.complexity ? `cx ${k.complexity}` : ''
  const grade = [tier, cx].filter(Boolean).join(' · ')
  return html`
    <div class="metarow">
      <span class="badge repo">${task.repo}</span>
      ${isEpic(task) ? html`<span class="badge epic">◆ epic</span>` : null}
      ${k.type === 'spike' ? html`<span class="badge spike">◇ spike</span>` : null}
      ${k.branch ? html`<span class="badge">⎇ ${k.branch}</span>` : null}
      ${grade ? html`<span class="badge">${grade}</span>` : null}
      ${k.worklog ? html`<span class="badge">☰ ${k.worklog}</span>` : null}
      ${task.mtime ? html`<span class="badge">${ago(task.mtime, now)}</span>` : null}
      ${isArchived(task) ? html`<span class="badge halt">archived</span>` : null}
      ${cmd
        ? html`<button class="act" type="button" data-testid="detail-resume"
                       title=${`copy: ${cmd}`} onClick=${() => copyText(cmd)}>⧉ claude --resume</button>`
        : null}
    </div>`
}

function Back() {
  return html`<a class="crumb" href=${hashForLens('board')}>← board</a>`
}

function Missing({ what }) {
  return html`
    <div data-testid="detail-missing">
      <${Back} />
      <p class="calmline">${what}</p>
    </div>`
}

/**
 * Task detail for a plain ticket.
 *
 * Epics and their children route elsewhere — `adb-lens-epic-detail`. An epic
 * reaching here would render its unused top-level fields as an empty ledger
 * and say nothing about its children (schema.md, "Epic files"), so it is
 * refused with a way onward rather than half-rendered.
 */
export function DetailView({ db, repo, id, now, isDesktop = true, onMove }) {
  const task = findTask(db, repo, id)
  if (!task) return html`<${Missing} what=${`No task ${repo}/${id} — it may have been archived or removed.`} />`
  if (isError(task)) {
    return html`
      <div>
        <${Back} />
        <article class="card err" data-testid="detail-error">
          <div class="head"><span class="ctitle">${task.file || task.id}</span>
            <span class="badge halt">parse error</span></div>
          <div class="errmsg">${task.error}</div>
        </article>
      </div>`
  }
  if (isEpic(task)) {
    return html`
      <div data-testid="detail-epic">
        <${Back} />
        <p class="calmline">
          ${(task.task || {}).title || task.id} is an epic — its children each carry their own
          plan and scorecard. <a href=${`/#${task.repo}/${task.id}`}>Open it on the board</a>.
        </p>
      </div>`
  }

  const k = task.task || {}
  return html`
    <article data-testid="detail" data-task=${`${task.repo}/${task.id}`}>
      <${Back} />
      <header class="hero">
        <h1>${k.title || task.id}</h1>
        <${Chips} task=${task} now=${now} />
        <${Stepper} task=${task} />
        ${onMove ? html`<div class="heroacts"><${MoveButton} task=${task} onMove=${onMove} /></div>` : null}
      </header>

      <${Ledger} task=${task} isDesktop=${isDesktop} />
      <${Record} task=${task} />
      <${NotesFold} notes=${task.notes} worklog=${k.worklog} />
    </article>`
}
