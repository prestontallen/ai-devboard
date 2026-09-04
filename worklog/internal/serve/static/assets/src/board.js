import { h } from 'preact'
import htm from 'htm'
import { inFlight, flatten, isError, isStale, isEpic, needsYou, waitingOn, phaseOf } from './counts.js'

const html = htm.bind(h)

/**
 * A deliberately minimal card. The real renderer — meters, pips, freshness,
 * epic nesting, tier and complexity — is adb-lens-card. Keeping a placeholder
 * here is the per-child-cards lesson: two card renderers running in parallel is
 * what went wrong last time, so this one exists only until that child lands.
 */
export function PlaceholderCard({ task, now }) {
  if (isError(task)) {
    return html`
      <article class="card err">
        <span class="ctitle">${task.file || task.id}</span>
        <span class="badge halt">unparseable</span>
      </article>`
  }
  const t = task.task || {}
  const needs = needsYou(task).length
  const waits = waitingOn(task).length
  return html`
    <article class="card">
      <span class="ctitle">${t.title || task.id}</span>
      <span class="crow">
        ${task.repo ? html`<span class="badge">${task.repo}</span>` : null}
        ${isEpic(task) ? html`<span class="badge epic">epic</span>` : null}
        ${phaseOf(task) ? html`<span class="badge phase">${phaseOf(task)}</span>` : null}
        ${needs > 0 ? html`<span class="badge attn">needs you · ${needs}</span>` : null}
        ${waits > 0 ? html`<span class="badge wait">waiting · ${waits}</span>` : null}
        ${isStale(task, now) ? html`<span class="badge stale">stale</span>` : null}
      </span>
    </article>`
}

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

export function BoardLens({ db, now }) {
  const tasks = inFlight(flatten(db))
  const needsCount = tasks.filter((t) => needsYou(t).length > 0).length
  return html`
    <div>
      <${AttentionLine} needsCount=${needsCount} />
      ${tasks.length === 0
        ? html`<p class="calmline">nothing in flight</p>`
        : html`<div class="grid">
            ${tasks.map((t) => html`<${PlaceholderCard} key=${t.id} task=${t} now=${now} />`)}
          </div>`}
    </div>`
}
