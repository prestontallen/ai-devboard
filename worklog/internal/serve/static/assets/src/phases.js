/**
 * The phase vocabulary.
 *
 * This is the fourth copy in the repo (devboard/schema.md,
 * worklog/internal/cli/task.go, and index.html hold the others). Extracting a
 * shared one would mean editing index.html, which this epic deliberately does
 * not touch; adb-lens-cutover deletes that copy and leaves three.
 */

export const PHASES = [
  'intake', 'clarify', 'research', 'contract', 'plan',
  'implementing', 'verify', 'present', 'ship', 'done',
]

// A spike answers a question instead of shipping a change, so it runs a short
// track. Every SPIKE_PHASES entry is also in PHASES, which keeps sorting and
// known-phase checks global — only the rendering is short.
export const SPIKE_PHASES = ['intake', 'research', 'present', 'done']

/**
 * A spike parked off its short track (rare, but possible) renders on the full
 * track rather than as an unknown phase.
 */
export function phasesFor(type, phase) {
  return type === 'spike' && (!phase || SPIKE_PHASES.includes(String(phase)))
    ? SPIKE_PHASES
    : PHASES
}

/** Index of `phase` within the track it belongs to, or -1 when off-enum. */
export function phaseIndex(type, phase) {
  return phasesFor(type, phase).indexOf(String(phase ?? ''))
}

/**
 * Scalar tolerance. A hand-written task file may carry `plan: ["bare string"]`
 * instead of `plan: [{text: ...}]`, and rendering those as 0/N with all-pending
 * pips is silently wrong rather than loudly wrong. Shipped criterion of the
 * 2026-09-01 redesign, ported here because counts.js had no equivalent.
 */
export function itemText(item) {
  return item && typeof item === 'object' ? item : { text: String(item ?? '') }
}

export function planStats(plan) {
  const list = Array.isArray(plan) ? plan : []
  return {
    total: list.length,
    done: list.filter((p) => itemText(p).state === 'done').length,
  }
}

export function checkStatuses(scorecard) {
  return (Array.isArray(scorecard) ? scorecard : []).map((c) => {
    const st = itemText(c).status
    return st === 'pass' || st === 'fail' ? st : 'pending'
  })
}

/** Relative age. Defaults its clock so the live page, which mounts without a
 *  `now` prop, agrees with the tests, which always pass one. */
export function ago(seconds, now = Date.now()) {
  if (!seconds) return ''
  const s = Math.max(0, now / 1000 - seconds)
  if (s < 90) return 'just now'
  if (s < 3600) return `${Math.round(s / 60)}m ago`
  if (s < 86400) return `${Math.round(s / 3600)}h ago`
  return `${Math.round(s / 86400)}d ago`
}
