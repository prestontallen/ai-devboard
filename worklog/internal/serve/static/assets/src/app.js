import { h } from 'preact'
import htm from 'htm'
import { Chip } from './chip.js'
import { BoardLens } from './board.js'
import { NeedsYouLens } from './needs.js'
import { WaitingLens } from './waiting.js'
import { FrictionLens } from './friction.js'
import { BacklogLens } from './backlog.js'
import { DoneLens, ArchivedLens } from './done.js'
import { DetailView } from './detail.js'
import { archiveAction } from './archive.js'
import { lensCounts, TONES } from './counts.js'
import { LENSES, PHONE, useView, navigate, defaultRoute } from './routes.js'
import { useBoardData, sseTransport } from './data.js'

const html = htm.bind(h)

const LABELS = {
  'needs-you': 'needs you',
}
const label = (lens) => LABELS[lens] || lens

export function ChipBar({ counts, route }) {
  return html`
    <nav class="chiprow" aria-label="lenses">
      ${LENSES.map((lens) => html`
        <${Chip}
          key=${lens}
          route=${lens}
          label=${label(lens)}
          count=${counts[lens]}
          tone=${TONES[lens] || 'neutral'}
          active=${lens === route}
          onNavigate=${navigate}
        />`)}
    </nav>`
}

/** Every route has a body. A lens takes the whole payload rather than a
 *  pre-filtered slice, because which tasks belong to it IS the lens — putting
 *  that decision out here would spread one lens across two files. */
const LENS = {
  board: BoardLens,
  'needs-you': NeedsYouLens,
  waiting: WaitingLens,
  friction: FrictionLens,
  backlog: BacklogLens,
  done: DoneLens,
  archived: ArchivedLens,
}

function Lens({ route, db, now, isDesktop, onMove }) {
  const View = LENS[route] || BoardLens
  return html`<${View} db=${db} now=${now} isDesktop=${isDesktop} onMove=${onMove} />`
}

export function App({ transport, load, now, matchPhone, onMove }) {
  const view = useView()
  const route = view.lens
  // happy-dom resolves hover/pointer media queries from navigator.maxTouchPoints,
  // which is fixed per environment, so desktop-ness is injected rather than
  // sniffed — otherwise neither branch is testable in one file.
  const isPhone = () =>
    matchPhone ? matchPhone() : typeof matchMedia === 'function' && matchMedia(PHONE).matches

  // The phone rule needs a count that does not exist until the first payload
  // lands, so it runs there — once — and never clobbers an explicit hash.
  const onFirstPayload = (db) => {
    const lens = defaultRoute({
      hash: location.hash,
      phone: isPhone(),
      needsYou: lensCounts(db)['needs-you'],
    })
    if (lens) navigate(lens)
  }

  const { db, status, refresh } = useBoardData({ transport, load, onFirstPayload })
  const counts = lensCounts(db)

  // A successful move emits an SSE tick server-side, which redraws the board on
  // its own; the explicit refresh is what keeps the lens honest when the
  // transport is down, and it is why the action is wrapped rather than passed
  // straight through.
  const move = (kind, task) => (onMove || defaultMove)(kind, task).then(refresh)

  return html`
    <div>
      <header class="topbar">
        <h1>devboard</h1>
        <span id="conn" class=${status} data-testid="conn">${status}</span>
      </header>
      <${ChipBar} counts=${counts} route=${route} />
      <main data-testid="lens" data-lens=${route || 'task'}>
        ${view.kind === 'task'
          ? html`<${DetailView} db=${db} repo=${view.task.repo} id=${view.task.id} child=${view.task.child}
                                now=${now} isDesktop=${!isPhone()} onMove=${move} />`
          : html`<${Lens} route=${route} db=${db} now=${now} isDesktop=${!isPhone()} onMove=${move} />`}
      </main>
    </div>`
}

export const defaultTransport = sseTransport
const defaultMove = archiveAction()
