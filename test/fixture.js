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

export const DB = {
  feedback: [
    { text: 'tui error on resize', resolved: false },
    { text: 'already handled', resolved: true },
  ],
  repos: [
    {
      repo: 'ai-devboard',
      tasks: [
        // in flight, needs you, and stale
        {
          id: 'router', file: 'ai-devboard/router.yaml', mtime: old,
          task: { title: 'Lens router', phase: 'implement', needs_you: [{ type: 'checkpoint', text: 'approve' }] },
        },
        // in flight, waiting on someone else
        {
          id: 'ledger', file: 'ai-devboard/ledger.yaml', mtime: fresh,
          task: { title: 'Contract ledger', phase: 'plan', waiting_on: [{ text: 'q', who: 'platform' }] },
        },
        // an epic: its queues live on children, never on the top level
        {
          id: 'lens-board', file: 'ai-devboard/lens-board.yaml', mtime: fresh,
          task: {
            title: 'Lens Board', type: 'epic',
            children: [
              { id: 'a', title: 'child a', needs_you: [{ type: 'question', text: 'which?' }] },
              { id: 'b', title: 'child b', waiting_on: [{ text: 'w', who: 'them' }] },
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
