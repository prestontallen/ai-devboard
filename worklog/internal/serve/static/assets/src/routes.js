import { useState, useEffect } from 'preact/hooks'

/** The lens set, in bar order. `backlog` sits between friction and done, where
 *  the design brief put it: the four before it are work you already own, the
 *  two after it are work you are finished with, and the backlog is the seam. */
export const LENSES = ['board', 'needs-you', 'waiting', 'friction', 'backlog', 'done', 'archived']

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
 * Task detail: `#/task/<repo>/<id>`, and a child of an epic at
 * `#/task/<repo>/<epic-id>/<child-id>`.
 *
 * Same leading-slash namespace as the lenses, deliberately. The outgoing
 * board's `#<repo>/<task>` grammar keeps meaning what it always did (a link
 * out to `/`), so nothing has to guess which board a pasted hash was copied
 * from. repo and id are the ones the payload carries, so both are matched
 * loosely and compared, never parsed for meaning.
 *
 * The third segment is optional rather than its own `#/child/...` namespace:
 * one grammar, and it mirrors the legacy `#<repo>/<epic>/<child>` shape a
 * reader already knows. `child` is always present and null for a plain task,
 * so a caller dispatches on its value instead of on the key's existence.
 */
export function taskFromHash(hash) {
  const m = /^#\/task\/([^/]+)\/([^/]+)(?:\/([^/]+))?$/.exec(hash || '')
  if (!m) return null
  try {
    return {
      repo: decodeURIComponent(m[1]),
      id: decodeURIComponent(m[2]),
      child: m[3] === undefined ? null : decodeURIComponent(m[3]),
    }
  } catch {
    // A malformed %-escape must not take the page down; it is simply not a route.
    return null
  }
}

export function hashForTask(repo, id) {
  return `#/task/${encodeURIComponent(repo)}/${encodeURIComponent(id)}`
}

/** A child is addressed through its epic, never on its own: no child of an
 *  epic gets a task file, so `<repo>/<epic-id>` is what resolves the payload
 *  entry the child entry is read out of (schema.md, "Epic files"). */
export function hashForChild(repo, epicId, childId) {
  return `${hashForTask(repo, epicId)}/${encodeURIComponent(childId)}`
}

/** Any hash this app resolves to a view of its own. The lens set and the task
 *  route are two grammars but one question: did the user ask for something
 *  specific? */
export const isOwnRoute = (hash) => !!(routeFromHash(hash) || taskFromHash(hash))

/**
 * Which lens to open on first paint. An explicit hash always wins — returning
 * null means "leave it alone". Otherwise a phone with something waiting opens
 * on needs-you, because on a phone you are checking whether you are needed,
 * not reading a work breakdown.
 */
export function defaultRoute({ hash, phone, needsYou }) {
  // Every route this app owns counts as explicit, not just the lenses. Asking
  // only about lenses would bounce a pasted task link off a phone straight to
  // needs-you, with every test still green.
  if (isOwnRoute(hash)) return null
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

/**
 * The view to render: a lens, or a task. Derived from the hash on every
 * hashchange for the same reason `useRoute` is — a state-held route freezes
 * the address bar and breaks back/forward.
 */
export function useView(fallback = 'board') {
  const read = () => {
    const task = taskFromHash(location.hash)
    return task ? { kind: 'task', task, lens: null } : { kind: 'lens', task: null, lens: routeFromHash(location.hash) || fallback }
  }
  const [view, setView] = useState(read)
  useEffect(() => {
    const on = () => setView(read())
    addEventListener('hashchange', on)
    return () => removeEventListener('hashchange', on)
  }, [fallback])
  return view
}
