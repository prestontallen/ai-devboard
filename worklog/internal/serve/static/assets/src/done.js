import { h } from 'preact'
import htm from 'htm'
import { flatten, isArchived, isDone } from './counts.js'
import { TaskGrid } from './grid.js'

const html = htm.bind(h)

/**
 * Done and Archived: the same lens twice, differing only in which tasks it is
 * handed. They are the two views that carry the board's only write, because
 * they are where you decide something is finished with.
 *
 * `done` deliberately excludes archived work — an archived task is done too,
 * and showing it in both would double every count against its chip.
 */
function MovableLens({ tasks, now, isDesktop, onMove, empty }) {
  return html`<${TaskGrid} tasks=${tasks} now=${now} isDesktop=${isDesktop}
                           onMove=${onMove} empty=${empty} />`
}

export function DoneLens({ db, now, isDesktop, onMove }) {
  const tasks = flatten(db).filter((t) => !isArchived(t) && isDone(t))
  return html`<${MovableLens} tasks=${tasks} now=${now} isDesktop=${isDesktop}
                              onMove=${onMove} empty="nothing finished yet" />`
}

export function ArchivedLens({ db, now, isDesktop, onMove }) {
  return html`<${MovableLens} tasks=${flatten(db).filter(isArchived)} now=${now}
                              isDesktop=${isDesktop} onMove=${onMove} empty="nothing archived" />`
}
