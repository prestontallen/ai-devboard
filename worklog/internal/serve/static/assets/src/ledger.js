import { h } from 'preact'
import htm from 'htm'
import { itemText } from './phases.js'

const html = htm.bind(h)

/**
 * The contract ledger.
 *
 * Plan and scorecard are task STATE; decisions, code and links are evidence.
 * The outgoing board renders all five through one section template, so the
 * agreement you approved looks like a list of links. This gives the state its
 * own frame and lets the evidence flow beneath it.
 *
 * The two tracks are shaped differently on purpose. Plan steps are threaded on
 * a connected rail because they are ordered: step 5 follows step 4, and the
 * filled rail shows how far the thread has run. Criteria are separate squares
 * because they are independent: #12 passing says nothing about #15. The shapes
 * tell you which list has an order before you read a word.
 */

const STEP_STATE = { done: 'done', in_progress: 'doing', blocked: 'blocked' }
const stepState = (item) => STEP_STATE[itemText(item).state] || 'pending'

const CHECK_STATE = { pass: 'pass', fail: 'fail' }
const checkState = (item) => CHECK_STATE[itemText(item).status] || 'open'

const list = (x) => (Array.isArray(x) ? x : [])

/** Outstanding first, and a failure ahead of a merely-unfinished check: the
 *  scorecard is the one list where the unfinished item matters most. Original
 *  positions ride along, because a criterion's number is its identity in the
 *  contract and must survive the sort. */
const CHECK_ORDER = { fail: 0, open: 1, pass: 2 }
export function orderedChecks(scorecard) {
  return list(scorecard)
    .map((item, i) => ({ item: itemText(item), n: i + 1, state: checkState(item) }))
    .sort((a, b) => CHECK_ORDER[a.state] - CHECK_ORDER[b.state])
}

export function planStats(plan) {
  const items = list(plan)
  return { total: items.length, done: items.filter((p) => stepState(p) === 'done').length }
}

export function checkStats(scorecard) {
  const items = list(scorecard)
  return {
    total: items.length,
    passed: items.filter((c) => checkState(c) === 'pass').length,
    failed: items.filter((c) => checkState(c) === 'fail').length,
  }
}

/** A ratio is green when the list is finished, red when something failed, amber
 *  while work is outstanding. The failed case wins: a failure has to be
 *  visible from the header without reading the list under it. */
function ratioTone({ total, done, failed }) {
  if (failed) return 'bad'
  return total > 0 && done === total ? 'done' : 'open'
}

function Ratio({ tone, value, label }) {
  return html`
    <span class=${`ratio ${tone}`} data-testid=${`ratio-${label}`}>
      <b>${value}</b> ${label}
    </span>`
}

export function LedgerHead({ task, plan, checks }) {
  const phase = (task.task && task.task.phase) || ''
  return html`
    <div class="ledgerhead">
      <span class="lbl">Contract</span>
      ${phase ? html`<span class="phase">${phase}</span>` : null}
      ${/* Reserved slot. phase_transitions shipped at cutover with no writer,
            so there is no elapsed time to show; adb-phase-transition-writer
            lights this and it stays blank until then. */
        task.inPhase ? html`<span class="inphase">${task.inPhase}</span>` : null}
      <span class="ratios">
        ${plan.total > 0
          ? html`<${Ratio} tone=${ratioTone(plan)} value=${`${plan.done}/${plan.total}`} label="plan" />`
          : null}
        ${checks.total > 0
          ? html`<${Ratio} tone=${ratioTone({ ...checks, done: checks.passed })}
                           value=${`${checks.passed}/${checks.total}`} label="checks" />`
          : null}
      </span>
    </div>`
}

export function PlanRail({ plan }) {
  const items = list(plan)
  if (!items.length) return html`<p class="tracknote">no plan recorded</p>`
  return html`
    <ol class="steps">
      ${items.map((raw, i) => {
        const p = itemText(raw)
        const state = stepState(raw)
        return html`
          <li class=${`step ${state}`} key=${i} data-testid="step" data-state=${state}>
            <span class="node" />
            <span class="tx">
              <span class="n">${i + 1}</span>${p.text || ''}
              ${/* Reserved slot: `worklog task plan block` records no reason
                    today, so this lights up only for a file that carries one.
                    adb-plan-block-reason. */
                state === 'blocked' && p.why ? html`<span class="why">blocked: ${p.why}</span>` : null}
            </span>
          </li>`
      })}
    </ol>`
}

/** On a phone the passing checks collapse behind a count so the outstanding
 *  ones fill the first screen. On desktop they stay: the whole scorecard fits,
 *  and hiding it hides the evidence the ledger exists to show. */
export function Scorecard({ scorecard, tier, isDesktop }) {
  const rows = orderedChecks(scorecard)
  if (!rows.length) return html`<${MissingContract} tier=${tier} />`

  const shown = isDesktop ? rows : rows.filter((r) => r.state !== 'pass')
  const hidden = rows.length - shown.length

  return html`
    <ul class="checks">
      ${shown.map((r) => html`
        <li class=${`check ${r.state}`} key=${r.n} data-testid="check" data-state=${r.state}>
          <span class="box" />
          <span class="body">
            <span class="tx"><span class="n">${String(r.n).padStart(2, '0')}</span>${r.item.text || ''}</span>
            ${/* A criterion without a visible verification is an opinion, and
                  the contract skill already refuses to write one. */
              r.item.verify ? html`<span class="vf">verify: ${r.item.verify}</span>` : null}
          </span>
        </li>`)}
      ${hidden > 0
        ? html`<li class="check collapsed" data-testid="collapsed">
            <span class="body"><span class="vf">… ${hidden} more passing, collapsed</span></span>
          </li>`
        : null}
    </ul>`
}

/** An empty scorecard is not neutral emptiness: above tier 1 it means the work
 *  has no agreed definition of done. Absent tier is NOT tier 0 though — most
 *  files carry no tier at all, and warning on those would cry wolf until the
 *  warning meant nothing. */
export function MissingContract({ tier }) {
  const n = Number(tier)
  if (!Number.isFinite(n) || n < 2) return html`<p class="tracknote">no scorecard recorded</p>`
  return html`
    <div class="empty" data-testid="no-contract">
      <span class="t">No scorecard on a tier ${n} task</span>
      <span class="s">acceptance was never written down</span>
    </div>`
}

export function Ledger({ task, isDesktop = true }) {
  const k = (task && task.task) || {}
  const plan = planStats(k.plan)
  const checks = checkStats(k.scorecard)
  return html`
    <section class="ledger" data-testid="ledger">
      <${LedgerHead} task=${task} plan=${plan} checks=${checks} />
      <div class="tracks">
        <div class="ltrack plan">
          <div class="ltrackhead"><h3>Plan</h3><span class="hint">sequential</span></div>
          <${PlanRail} plan=${k.plan} />
        </div>
        <div class="ltrack score">
          <div class="ltrackhead"><h3>Contract scorecard</h3><span class="hint">independent</span></div>
          <${Scorecard} scorecard=${k.scorecard} tier=${k.tier} isDesktop=${isDesktop} />
        </div>
      </div>
    </section>`
}
