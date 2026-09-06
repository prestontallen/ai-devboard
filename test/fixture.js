/**
 * Render-test fixture.
 *
 * Hand-authored rather than derived from worklog/internal/serve/testdata/
 * golden_tasks.json: that corpus has zero needs_you and zero waiting_on
 * entries, so it cannot exercise those counts, and extending it to serve a
 * front-end test would mean editing a payload frozen precisely so the server
 * cannot drift. The trade is that this file can drift from the golden; it is
 * shaped to devboard/schema.md, not to the server.
 */

const now = 1_800_000_000_000 // fixed clock; stale threshold is 2h of mtime
export const NOW = now
const fresh = now / 1000 - 60
const old = now / 1000 - 5 * 3600
/** `waiting_on.asked` is an ISO date string, so it is derived from the same
 *  fixed clock — a hardcoded date would drift against NOW and make the age
 *  assertions meaningless. */
export const daysAgo = (n) => new Date(now - n * 86400000).toISOString().slice(0, 10)

export const DB = {
  // Shaped to devboard/API.md's feedback entry, not to `{text, resolved}`:
  // the friction lens renders signal/trigger/timestamp, and a fixture that
  // carries none of them would let a wrong renderer pass green. `resolved` is
  // an int, 0 = open — never a boolean.
  feedback: [
    {
      timestamp: 1_799_990_000, signal: 'tui-error', trigger: 'tui error on resize',
      excerpt: 'panic: index out of range', context: 'resizing below 40 cols',
      resolved: 0,
    },
    {
      timestamp: 1_799_900_000, signal: 'missing-feature', trigger: 'already handled',
      excerpt: '', context: '', resolved: 1_799_950_000,
    },
  ],
  repos: [
    {
      repo: 'ai-devboard',
      tasks: [
        // in flight, needs you, and stale. `implementing` is the real enum
        // member; `implement` would render on the unknown-phase branch.
        {
          id: 'router', file: 'ai-devboard/router.yaml', mtime: old,
          task: {
            title: 'Lens router', phase: 'implementing', worklog: 'adb-lens-router',
            session: 'sess-abc123',
            // Two entries on ONE task: this is what separates the needs-you
            // chip's item count from a task count, and what a grouped-row
            // layout would have collapsed.
            needs_you: [
              { type: 'checkpoint', text: 'approve', detail: 'Summary: 2 files.\nMessage: "wire it up"' },
              { type: 'question', text: 'squash or keep the history?' },
            ],
            plan: [{ text: 'one', state: 'done' }, { text: 'two', state: 'done' }, { text: 'three' }],
            scorecard: [
              { text: 'a', status: 'pass' }, { text: 'b', status: 'fail' }, { text: 'c' },
            ],
          },
        },
        // in flight, waiting on someone else; scalar plan/scorecard entries,
        // which a hand-written file may legally carry
        {
          id: 'ledger', file: 'ai-devboard/ledger.yaml', mtime: fresh,
          task: {
            title: 'Contract ledger', phase: 'plan', type: 'ticket',
            // `asked` is an ISO date, not epoch seconds. The link is the live
            // half of the scheme guard.
            waiting_on: [{
              text: 'q', who: 'platform', asked: daysAgo(6),
              link: 'https://example.test/thread/1',
            }],
            plan: ['bare string step', 'another'],
            scorecard: ['bare criterion'],
          },
        },
        // an epic: its queues live on children, never on the top level. One
        // child deliberately carries no `state`.
        {
          id: 'lens-board', file: 'ai-devboard/lens-board.yaml', mtime: fresh,
          task: {
            // The epic's own session is here to be NOT used: each child runs
            // its own agent, so a row must resume the child's.
            title: 'Lens Board', type: 'epic', session: 'sess-epic',
            children: [
              {
                id: 'a', title: 'child a', state: 'active', session: 'sess-child-a',
                needs_you: [{ type: 'question', text: 'which?' }],
              },
              {
                id: 'b',
                title: 'child b',
                // A hand-editable file can put any scheme in `link`.
                waiting_on: [{ text: 'w', who: 'them', asked: daysAgo(2), link: 'javascript:alert(1)' }],
              },
            ],
          },
        },
        // done
        { id: 'scaffold', file: 'ai-devboard/scaffold.yaml', mtime: fresh, task: { title: 'Stack scaffold', phase: 'done' } },
        // archived
        { id: 'old-thing', file: 'ai-devboard/old.yaml', mtime: fresh, archived: true, task: { title: 'Archived thing', phase: 'done' } },
        // malformed: no task, no mtime — every helper must guard on error first
        { id: 'broken', file: 'ai-devboard/broken.yaml', error: 'yaml: line 3: unclosed' },
      ],
    },
  ],
}

export const EMPTY = { repos: [], feedback: [] }

/** Single-task builders for card tests. Kept out of DB so the count
 *  assertions in counts.test.js stay valid. */
export const task = (over = {}) => ({
  repo: 'ai-devboard', id: 'sample', file: 'ai-devboard/sample.yaml', mtime: fresh,
  ...over,
  task: { title: 'A sample task', phase: 'implementing', ...(over.task || {}) },
})

export const SPIKE = task({ id: 'spike', task: { title: 'A spike', type: 'spike', phase: 'research' } })
export const SPIKE_OFF_TRACK = task({ id: 'spikeoff', task: { title: 'Spike off track', type: 'spike', phase: 'verify' } })
export const NO_PHASE = task({ id: 'nophase', task: { title: 'No phase', phase: undefined } })
export const OFF_ENUM = task({ id: 'offenum', task: { title: 'Off enum', phase: 'brainstorming' } })
export const CHORE = task({ id: 'chore', task: { title: 'A chore', type: 'chore' } })
export const LONG = task({
  id: 'long',
  task: {
    title: 'A deliberately very long task title that must keep its column and not be squeezed by a neighbouring badge',
    type: 'epic',
    children: [{ id: 'kid', title: 'An extremely long child title that would otherwise stretch its chip without bound', state: 'active' }],
  },
})
