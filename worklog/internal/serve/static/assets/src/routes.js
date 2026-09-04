import { useState, useEffect } from 'preact/hooks'

/** The lens set, in bar order. `backlog` is deliberately absent: /api/tasks
 *  carries only started work, so its chip would read 0 forever. It returns
 *  with adb-lens-backlog, which adds the server half too. */
export const LENSES = ['board', 'needs-you', 'waiting', 'friction', 'done', 'archived']

export const PHONE = '(max-width: 640px)'

/**
 * Lens routes are namespaced with a leading slash (`#/board`). The existing
 * board's deep links are `#<repo>/<task>` and `#<repo>/<epic>/<child>` with
 * no leading slash, so the two grammars never collide — a legacy hash simply
 * matches no lens and falls back, rather than resolving to the wrong view.
 */
export function routeFromHash(hash) {
  const m = /^#\/([a-z-]+)$/.exec(hash || '')
  return m && LENSES.includes(m[1]) ? m[1] : null
}

export function hashForLens(lens) {
  return `#/${lens}`
}

/**
 * Which lens to open on first paint. An explicit hash always wins — returning
 * null means "leave it alone". Otherwise a phone with something waiting opens
 * on needs-you, because on a phone you are checking whether you are needed,
 * not reading a work breakdown.
 */
export function defaultRoute({ hash, phone, needsYou }) {
  if (routeFromHash(hash)) return null
  return phone && needsYou > 0 ? 'needs-you' : 'board'
}

/**
 * Route is derived from the hash rather than held in state. Chip preventDefaults
 * its own click, so a state-held route would leave the address bar frozen and
 * silently break back/forward, bookmarking and middle-click.
 */
export function useRoute(fallback = 'board') {
  const read = () => routeFromHash(location.hash) || fallback
  const [route, setRoute] = useState(read)
  useEffect(() => {
    const on = () => setRoute(read())
    addEventListener('hashchange', on)
    return () => removeEventListener('hashchange', on)
  }, [fallback])
  return route
}

export function navigate(lens) {
  location.hash = hashForLens(lens)
}
