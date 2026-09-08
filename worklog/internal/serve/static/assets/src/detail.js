import { h } from 'preact'
import htm from 'htm'
import { flatten, isError, isArchived, isEpic, childState } from './counts.js'
import { phasesFor, phaseIndex, ago } from './phases.js'
import { copyText } from './clipboard.js'
import { MoveButton } from './archive.js'
import { Ledger } from './ledger.js'
import { Record } from './sections.js'
import { NotesFold } from './markdown.js'
import { hashForLens, hashForTask } from './routes.js'
import { EpicView, childTask, childrenOf } from './epic.js'

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

/**
 * `resume` is off for an epic, which is the one place the epic's own fields
 * lie: the file carries a `session`, but schema.md says an epic's top-level
 * fields are unused — each child runs its own agent. A resume button there
 * would hand you a terminal for the wrong one.
 */
function Chips({ task, now, resume = true, crumb = null }) {
  const k = task.task || {}
  const cmd = resume && k.session ? `claude --resume ${k.session}` : ''
  const tier = k.tier === undefined || k.tier === null ? '' : `tier ${k.tier}`
  const cx = k.complexity ? `cx ${k.complexity}` : ''
  const grade = [tier, cx].filter(Boolean).join(' · ')
  return html`
    <div class="metarow">
      ${crumb}
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

/** Back goes one level, not always home: from a child that is its epic, which
 *  is where you were. */
function Back({ href = hashForLens('board'), label = '← board' }) {
  return html`<a class="crumb" href=${href}>${label}</a>`
}

function Missing({ what, back }) {
  return html`
    <div data-testid="detail-missing">
      ${back || html`<${Back} />`}
      <p class="calmline">${what}</p>
    </div>`
}

/**
 * Child detail.
 *
 * Identical to a plain task's below the hero, and deliberately so: a
 * `children[]` entry is the same body a task file carries, so the ledger and
 * the record read it without knowing it is a child (schema.md, "Epic files").
 *
 * What differs is the frame. Back goes to the epic, the hero leads with the
 * epic it belongs to, and there is no archive control — archiving is a
 * whole-file action from the epic's own view, never per child.
 */
function ChildDetail({ epic, child, now, isDesktop }) {
  const t = childTask(epic, child)
  const k = t.task
  const epicTitle = (epic.task || {}).title || epic.id
  const back = html`<${Back} href=${hashForTask(epic.repo, epic.id)} label=${`← ${epicTitle}`} />`
  return html`
    <article data-testid="detail-child" data-task=${`${epic.repo}/${epic.id}/${t.id}`}>
      ${back}
      <header class="hero">
        <h1>${k.title || t.id}</h1>
        <${Chips} task=${t} now=${now}
                  crumb=${html`<span class=${`badge child ${childState(k)}`}>${childState(k)}</span>`} />
        <${Stepper} task=${t} />
      </header>

      <${Ledger} task=${t} isDesktop=${isDesktop} />
      <${Record} task=${t} />
      <${NotesFold} notes=${child.notes} worklog=${t.id} />
    </article>`
}

/**
 * Detail for a task, an epic, or one of an epic's children.
 *
 * The three are one route because they are one question — "show me this" —
 * and the payload resolves all three from the same `<repo>/<id>` entry: a
 * child has no task file of its own, so it is read out of its epic's.
 */
export function DetailView({ db, repo, id, child = null, now, isDesktop = true, onMove }) {
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

  if (child !== null) {
    // A plain task has no children, so a three-segment hash pointing at one is
    // as wrong as a missing id — and says so rather than rendering the parent.
    const entry = isEpic(task) ? childrenOf(task).find((c) => c.id === child) : null
    if (!entry) {
      return html`
        <${Missing}
          what=${`No child ${child} under ${repo}/${id} — it may have been renamed or removed.`}
          back=${html`<${Back} href=${hashForTask(repo, id)} label=${`← ${(task.task || {}).title || id}`} />`} />`
    }
    return html`<${ChildDetail} epic=${task} child=${entry} now=${now} isDesktop=${isDesktop} />`
  }

  const k = task.task || {}
  const hero = html`
    <header class="hero">
      <h1>${k.title || task.id}</h1>
      <${Chips} task=${task} now=${now} resume=${!isEpic(task)} />
      ${isEpic(task) ? null : html`<${Stepper} task=${task} />`}
      ${onMove ? html`<div class="heroacts"><${MoveButton} task=${task} onMove=${onMove} /></div>` : null}
    </header>`

  if (isEpic(task)) {
    return html`
      <div>
        <${Back} />
        <${EpicView} epic=${task} now=${now} isDesktop=${isDesktop} hero=${hero} />
      </div>`
  }

  return html`
    <article data-testid="detail" data-task=${`${task.repo}/${task.id}`}>
      <${Back} />
      ${hero}
      <${Ledger} task=${task} isDesktop=${isDesktop} />
      <${Record} task=${task} />
      <${NotesFold} notes=${task.notes} worklog=${k.worklog} />
    </article>`
}
