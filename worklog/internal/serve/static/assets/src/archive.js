import { h } from 'preact'
import { useState, useEffect } from 'preact/hooks'
import htm from 'htm'

const html = htm.bind(h)

/** How long a failure stays on the button before it goes back to offering the
 *  move again. Long enough to read, short enough not to strand the control. */
export const REVERT_MS = 2500

const PATHS = { archive: '/api/archive', unarchive: '/api/unarchive' }

/**
 * The board's only write.
 *
 * Injected rather than called inline, for the same reason the SSE transport is
 * (data.js): happy-dom gives a render test `fetch` but no server, so an
 * un-injected POST leaves the test reaching for the network.
 *
 * `Content-Type: application/json` is the CSRF guard itself, not politeness —
 * cross-origin JSON forces a preflight the server never answers (API.md,
 * "Write endpoints"). Sending the body without it earns a 415.
 */
export function archiveAction(f) {
  const doFetch = f || ((...a) => fetch(...a))
  return async (kind, { repo, id }) => {
    const res = await doFetch(PATHS[kind], {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ repo, id }),
    })
    if (!res.ok) {
      // Every documented failure carries `{"error": "..."}`; a body that is
      // not JSON must still surface as the status rather than as a parse
      // exception.
      const body = await res.json().catch(() => ({}))
      throw new Error(body.error || `HTTP ${res.status}`)
    }
    return res.json().catch(() => ({}))
  }
}

/**
 * The move control. It holds the one piece of local state on a card — the
 * transient failure — and that is why it is its own component: `Card` stays a
 * pure function of props (card.js:9), because per-card view state is exactly
 * what `openFolds` existed to rescue on the outgoing board. This state is
 * momentary feedback, not view state; nothing is lost if an SSE tick discards
 * it, which is the test that separates the two.
 */
export function MoveButton({ task, onMove, revertMs = REVERT_MS }) {
  const kind = task.archived ? 'unarchive' : 'archive'
  const [err, setErr] = useState('')

  useEffect(() => {
    if (!err) return undefined
    // The revert delay is a prop so a test can watch a real timer expire:
    // faking timers here fights Preact's own microtask-scheduled rerender.
    const t = setTimeout(() => setErr(''), revertMs)
    return () => clearTimeout(t)
  }, [err])

  const onClick = async (e) => {
    // The control sits inside the card's anchor, so it must swallow the click
    // or archiving also navigates away from the lens.
    e.preventDefault()
    e.stopPropagation()
    setErr('')
    try {
      await onMove(kind, { repo: task.repo, id: task.id })
    } catch (x) {
      // `String(new Error(''))` is the word "Error", so the message is read
      // off the error rather than stringified — otherwise a blank failure
      // shows the user the class name.
      const msg = x instanceof Error ? x.message : String(x ?? '')
      setErr(msg.trim() || 'move failed')
    }
  }

  const label = kind === 'archive' ? '⌫ archive' : '⤴ un-archive'
  return html`
    <button class=${`act${err ? ' bad' : ''}`} type="button" data-testid="move"
            data-kind=${kind} title=${err || label} onClick=${onClick}>
      ${err ? 'failed ✕' : label}
    </button>`
}
