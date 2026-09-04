import { h } from 'preact'
import htm from 'htm'

const html = htm.bind(h)

/**
 * The Lens Board's router element: a chip is a view, not a stat.
 *
 * Two behaviors carry the design and are the ones under test. A zero count
 * dims instead of vanishing — today's board drops those chips with a
 * ternary, which makes an empty queue indistinguishable from one that is
 * not being tracked. And the active chip is marked with aria-current, so
 * "which lens am I in" is answerable without reading colors.
 *
 * `tone` is the color semantics the redesign kept, not decoration:
 * attn = needs-you (amber), wait = waiting/active (blue), neutral
 * = uncoloured (friction, backlog, done, archived).
 */
export function Chip({ label, count, route, active = false, tone = 'neutral', onNavigate }) {
  const cls = ['chip', tone, count === 0 && 'zero', active && 'on']
    .filter(Boolean)
    .join(' ')

  const click = (e) => {
    if (!onNavigate) return
    e.preventDefault()
    onNavigate(route)
  }

  return html`
    <a
      class=${cls}
      href=${`#/${route}`}
      aria-current=${active ? 'page' : null}
      onClick=${click}
    >
      <b>${count}</b><span>${label}</span>
    </a>
  `
}
