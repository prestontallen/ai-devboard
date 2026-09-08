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

/**
 * The outgoing board's grammar: `#<repo>/<id>` and `#<repo>/<epic>/<child>`,
 * with no leading slash.
 *
 * Translating rather than ignoring is safe for exactly the reason the leading
 * slash was chosen in the first place: the two grammars cannot collide, so a
 * hash without one is unambiguously the old board's and means precisely what
 * the new route means. Links in this shape are in archive summaries, notes and
 * anything Preston bookmarked before the cutover.
 *
 * The segments are matched loosely and never parsed for meaning — the same
 * rule taskFromHash follows. `[^/]+` cannot span a `/`, so nothing here can
 * produce a protocol-relative `//host` or reach past the fragment.
 */
const LEGACY = /^#([^/#]+)\/([^/#]+)(?:\/([^/#]+))?$/

export function legacyHash(hash) {
  const m = LEGACY.exec(hash || '')
  // A lens hash (`#/board`) starts with the slash, so it never matches; the
  // explicit checks guard anyway, since a silent mistranslation is worse than
  // no translation at all.
  if (!m || routeFromHash(hash) || taskFromHash(hash)) return null
  const [, repo, id, child] = m
  return child ? `${hashForTask(repo, id)}/${child}` : hashForTask(repo, id)
}

/**
 * Any hash this app resolves to a view of its own. The lens set, the task
 * route and the outgoing board's grammar are three shapes but one question:
 * did the user ask for something specific?
 *
 * Legacy hashes joined the set at the cutover. Before it they were a link to
 * another page and so genuinely not this app's business, which is why the
 * phone rule was allowed to override one. Now they resolve to a real view, and
 * bouncing a phone off a pasted task link to needs-you would be the same bug
 * the task route already fixed once.
 */
export const isOwnRoute = (hash) =>
  !!(routeFromHash(hash) || taskFromHash(hash) || legacyHash(hash))

/**
 * Rewrite a legacy hash in place, once, and report whether it happened.
 *
 * `replaceState` rather than assigning to `location.hash`: an assignment
 * pushes a history entry, so Back would return to the legacy hash and
 * translate it again — a trap the user cannot escape with the browser's own
 * control. Replacing leaves history exactly as long as it was.
 *
 * Callers still read the route from `location.hash` afterwards, so this only
 * has to fix the URL; it deliberately does not navigate.
 */
export function adoptLegacyHash(loc = location, history = window.history) {
  const next = legacyHash(loc.hash)
  if (!next) return false
  // Only the fragment is rewritten. Everything before the `#` is whatever the
  // page was already served from, so this cannot move origin.
  history.replaceState(null, '', loc.pathname + loc.search + next)
  return true
}

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
  // Resolving reads through the translation but does not perform it: the view
  // is correct on the very first paint, and the URL is tidied in the effect
  // below. Doing the rewrite here instead would put a history side effect in
  // a render.
  const read = () => {
    const hash = legacyHash(location.hash) || location.hash
    const task = taskFromHash(hash)
    return task ? { kind: 'task', task, lens: null } : { kind: 'lens', task: null, lens: routeFromHash(hash) || fallback }
  }
  const [view, setView] = useState(read)
  useEffect(() => {
    // replaceState fires no hashchange, so this cannot re-enter.
    adoptLegacyHash()
    const on = () => { adoptLegacyHash(); setView(read()) }
    addEventListener('hashchange', on)
    return () => removeEventListener('hashchange', on)
  }, [fallback])
  return view
}
