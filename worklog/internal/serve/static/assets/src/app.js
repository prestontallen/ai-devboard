import { h } from 'preact'
import htm from 'htm'
import { Chip } from './chip.js'
import { BoardLens } from './board.js'
import { lensCounts, TONES } from './counts.js'
import { LENSES, PHONE, useRoute, navigate, defaultRoute } from './routes.js'
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

/** Only the Board lens has a body in this ticket; the rest are placeholders
 *  that adb-lens-views fills in. They still route, so the nav is honest about
 *  where you are. */
function Lens({ route, db, now }) {
  if (route === 'board') return html`<${BoardLens} db=${db} now=${now} />`
  return html`<p class="calmline">the ${label(route)} lens is not built yet</p>`
}

export function App({ transport, load, now, matchPhone }) {
  const route = useRoute()
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

  const { db, status } = useBoardData({ transport, load, onFirstPayload })
  const counts = lensCounts(db)

  return html`
    <div>
      <header class="topbar">
        <h1>devboard</h1>
        <span id="conn" class=${status} data-testid="conn">${status}</span>
      </header>
      <${ChipBar} counts=${counts} route=${route} />
      <main data-testid="lens" data-lens=${route}>
        <${Lens} route=${route} db=${db} now=${now} />
      </main>
    </div>`
}

export const defaultTransport = sseTransport
