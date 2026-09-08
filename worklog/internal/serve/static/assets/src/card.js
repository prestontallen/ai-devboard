import { h } from 'preact'
import htm from 'htm'
import { isError, isStale, isEpic, needsYou, waitingOn, phaseOf, childState, byChildState } from './counts.js'
import { phasesFor, phaseIndex, planStats, checkStatuses, ago, itemText } from './phases.js'
import { copyText } from './clipboard.js'
import { MoveButton } from './archive.js'
import { hashForTask, hashForChild } from './routes.js'

const html = htm.bind(h)

/** There is exactly ONE card renderer. The previous attempt at parallel render
 *  modes (adb-epic-per-child-cards) was pivoted out mid-flight; nothing here
 *  should grow a second mode. Epic and error are variants inside this file,
 *  not separate components with their own layout rules.
 *
 *  The card is a pure function of props — no local state. Tier and complexity
 *  live in the detail view, not behind a per-card toggle, because per-card
 *  expand state is what `openFolds` existed to rescue on the old board. */

/** Every card opens in this app now — epics included, since adb-lens-epic-detail
 *  built the view that can render them. A child carries `parent` (the adapter
 *  in epic.js sets it) and is addressed through its epic, because no child of
 *  an epic has a task file of its own to address directly. */
export const detailHref = (task) =>
  task.parent
    ? hashForChild(task.repo, task.parent, task.id)
    : hashForTask(task.repo, task.id)

/** Interactive controls sit inside the card's anchor, so each one must stop the
 *  click from also navigating the card — exactly as index.html does. */
function swallow(e) {
  e.preventDefault()
  e.stopPropagation()
}

function TypeBadge({ task }) {
  const type = task.task && task.task.type
  // `ticket` and `chore` exist in real files but are not badge-worthy; the
  // schema treats type as absent for an ordinary ticket.
  if (type !== 'epic' && type !== 'spike') return null
  return type === 'epic'
    ? html`<span class="badge epic">◆ epic</span>`
    : html`<span class="badge spike">◇ spike</span>`
}

/** A phase-less task is a missing field and reads as one. A phase-less PENDING
 *  child is not: `children[].state` says it has not been started, which is a
 *  normal place to be, and labelling it "no phase" would report a healthy
 *  roster entry as a data error. `state` is a children-only field, so a
 *  standalone task file can never take this branch. */
function emptyPhaseLabel(task) {
  const k = task.task || {}
  if (phaseOf(task)) return { text: `${k.phase} · unknown phase`, tone: 'unknown' }
  return k.state && childState(k) === 'pending'
    ? { text: 'not started', tone: 'idle' }
    : { text: 'no phase', tone: 'unknown' }
}

function Track({ task }) {
  const type = task.task && task.task.type
  const phase = phaseOf(task)
  const list = phasesFor(type, phase)
  const i = phaseIndex(type, phase)
  const empty = emptyPhaseLabel(task)
  return html`
    <div>
      <div class="track">
        ${list.map((_, n) => html`
          <i class=${i < 0 ? '' : n < i ? 'on' : n === i ? 'now' : ''} />`)}
      </div>
      ${i >= 0
        ? html`<div class="phase-lbl">${phase} <small>· ${i + 1}/${list.length}</small></div>`
        : html`<div class=${`phase-lbl ${empty.tone}`}>${empty.text}</div>`}
    </div>`
}

function Meters({ task }) {
  const k = task.task || {}
  const { done, total } = planStats(k.plan)
  const checks = checkStatuses(k.scorecard)
  if (!total && !checks.length) return null
  return html`
    <div class="meters">
      ${total > 0 ? html`
        <div class="meter">
          <div class="lbl"><span>plan</span><b>${done}/${total}</b></div>
          <div class="bar"><i style=${`width:${Math.round((100 * done) / total)}%`} /></div>
        </div>` : null}
      ${checks.length > 0 ? html`
        <div class="pips">
          <span class="lbl">checks</span>
          ${checks.map((st, n) => html`
            <span class=${`pip ${st}`} title=${itemText((k.scorecard || [])[n]).text || ''}>
              ${st === 'pass' ? '✓' : st === 'fail' ? '✕' : ''}
            </span>`)}
        </div>` : null}
    </div>`
}

function Resume({ task, isDesktop }) {
  const session = task.task && task.task.session
  // No session means no affordance at all — not an empty slot. The field is
  // absent from every task file in the frozen corpus.
  if (!session || !isDesktop) return null
  const cmd = `claude --resume ${session}`
  const onClick = (e) => { swallow(e); copyText(cmd) }
  return html`
    <button class="act" type="button" title=${`copy: ${cmd}`}
            data-testid="resume" onClick=${onClick}>⧉</button>`
}

function Roster({ task, onChild }) {
  const children = (task.task && task.task.children) || []
  if (!children.length) return html`<div class="rosterempty">no children started yet</div>`
  const mark = { active: '▶ ', pending: '○ ', done: '✓ ' }
  // Same comparator the epic page's child grid uses, from counts.js — two
  // copies of the order is how the roster and the grid drift apart.
  const sorted = children.slice().sort(byChildState)
  return html`
    <div class="roster">
      ${sorted.map((c) => {
        const state = childState(c)
        const id = c.id || ''
        const go = (e) => {
          swallow(e)
          if (onChild) onChild(task.repo, task.id, id)
          else location.assign(hashForChild(task.repo, task.id, id))
        }
        return html`
          <span class=${`childchip ${state}`} data-child=${id} data-state=${state}
                title=${`${c.title || id} · ${state}`} onClick=${go}>
            ${mark[state] || ''}${c.title || id}
          </span>`
      })}
    </div>`
}

/** `onMove` is an optional slot, not a second card: Done and Archived pass one
 *  and get the control, Board passes none and is unchanged. A parallel card
 *  renderer is what adb-epic-per-child-cards died of. */
export function Card({ task, now, isDesktop = true, onChild, onMove }) {
  if (isError(task)) {
    return html`
      <article class="card err">
        <div class="head"><span class="ctitle">${task.file || task.id}</span>
          <span class="badge halt">parse error</span></div>
        <div class="errmsg">${task.error}</div>
      </article>`
  }

  const k = task.task || {}
  const needs = needsYou(task).length
  const waits = waitingOn(task).length
  const stale = isStale(task, now)

  return html`
    <a class=${['card', needs && 'attn', stale && 'stale'].filter(Boolean).join(' ')}
       href=${detailHref(task)}>
      <div class="head">
        <span class="ctitle">${k.title || task.id}</span>
        ${needs > 0 ? html`<span class="flag">needs you · ${needs}</span>` : null}
        ${waits > 0 ? html`<span class="badge wait">⧖ ${waits}</span>` : null}
        <${TypeBadge} task=${task} />
        <span class="badge repo">${task.repo}</span>
      </div>

      ${isEpic(task)
        ? html`<${Roster} task=${task} onChild=${onChild} />`
        : html`<${Track} task=${task} /><${Meters} task=${task} />`}

      <div class="foot">
        ${task.mtime
          ? html`<span class=${`dot${stale ? ' old' : ''}`} />
                 <span class="age">${ago(task.mtime, now)}${stale ? ' — stale' : ''}</span>`
          : null}
        <span class="sp" />
        ${k.worklog ? html`<span class="wl">☰ ${k.worklog}</span>` : null}
        <${Resume} task=${task} isDesktop=${isDesktop} />
        ${onMove ? html`<${MoveButton} task=${task} onMove=${onMove} />` : null}
      </div>
    </a>`
}
