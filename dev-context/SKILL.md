---
name: dev-context
description: >-
  Development workflow process governing how work moves from "topic introduced"
  to "code shown to the human". Use at the START of any dev task — bugfix,
  feature, refactor, or project — to classify the task, decide which checkpoints
  the human sees, and when code is presented. Pairs with the contract skill.
---

# Dev Context: from topic to code

This skill defines the collaboration process for development work. Its core
rule: **the human approves the shape of the work before code exists, and sees
code only after it has been verified against that shape.** No silent scope,
no surprise diffs.

## Task tiers

Classify every task at intake. The tier decides how much process applies.

| Tier | Name    | Signals                                                        | Contract form                     | Human checkpoints                 |
|------|---------|----------------------------------------------------------------|-----------------------------------|-----------------------------------|
| 0    | Trivial | Typo, rename, comment, config value; one obvious edit          | One "done when" sentence, inline  | Code review only                  |
| 1    | Small   | Single-file bugfix or tweak; behavior change is local          | Mini-contract, inline (3–6 lines) | Contract nod → code review        |
| 2    | Feature | Multi-file change, new behavior, or user-visible surface       | Full contract, written to file    | Contract approval → code review   |
| 3    | Major   | Cross-cutting, architectural, multi-session, or risky          | Contract + milestones, to file    | Contract → plan → per-milestone review |

When unsure between two tiers, pick the higher one — downgrading mid-task is
cheap, discovering missing process isn't.

## Phases

### 1. Intake
The human introduces the topic. Before doing anything else:
- Restate the task in your own words (one or two sentences).
- Classify the tier and say so.
- Note what you'd need to look at (files, docs, prior art) to firm it up.

### 2. Clarify
Investigate the codebase first — most questions answer themselves from the
code. Ask the human only questions where the answer changes the outcome
(product intent, tradeoffs, unknowable preferences). Batch them; don't
dribble questions one at a time.

### 3. Contract
Invoke the **contract** skill to produce the contract at the tier's depth.
This is the first hard checkpoint for tier 1+:

- Tier 1: present the mini-contract inline; a quick "yep" is enough.
- Tier 2–3: present the full contract; **do not write implementation code
  until the human approves it.** Exploratory reading/prototyping in scratch
  space is fine.

If a worklog item exists (or should), sync the contract's acceptance summary
into its **Acceptance** field.

### 4. Plan (tier 2–3)
Derive the implementation plan from the contract: ordered steps, files
touched, test strategy. Tier 2: state the plan briefly and proceed. Tier 3:
the plan is its own checkpoint — get approval, and break work into
milestones, each with its own mini-contract.

### 5. Implement
Write the code. While implementing:
- Stay inside the contract's scope. Wanting to touch something out-of-scope
  is a signal to stop and amend, not to quietly expand.
- If reality contradicts the contract (wrong assumption, hidden constraint),
  **stop and propose an amendment** — a short "the contract said X, I found
  Y, propose changing Z". Get a yes before continuing.
- Surface load-bearing discoveries as brief status notes; don't narrate.

### 6. Verify
Before showing anything, run the contract's verification plan: execute each
acceptance criterion's check, plus the standing definition-of-done items
(tests, lint, no unrelated changes). Record pass/fail honestly — a failed
check is reported, never papered over.

### 7. Present
Now the human sees code. Present it as a review, not a dump:
- Lead with the contract scorecard: each acceptance criterion with ✅/❌ and
  how it was verified.
- Walk the diff by intent ("criterion 2 is satisfied by the change in
  `foo.py:42`"), grouping files by purpose.
- Call out anything that deviated from the plan and why.
- List known gaps or follow-ups explicitly — an honest ❌ beats a hidden one.

The task is **done** when the human accepts the review. Mark the contract
fulfilled; close or update the worklog item.

### 8. Ship
Committing, pushing, and PR interaction. Every action in this phase is
gated by the hard checkpoints below — acceptance of the review (phase 7)
authorizes none of them on its own.

## Hard checkpoints — the human MUST be involved

Unlike the phase checkpoints, these do not scale with tier. They apply to
every task, tier 0 included, every time — prior approval of one instance
never carries over to the next.

1. **Never commit before the human has seen a work summary.** Before any
   `git commit`, present what the human NEEDS to know to understand how
   the feature was implemented — not a diffstat, but the substance: the
   approach taken and why, key decisions and tradeoffs, which files carry
   the core change, anything surprising or fragile, and any deviation from
   the contract. Commit only after the human acknowledges. (If phase 7's
   review already covered all of this and the human accepted, a brief "as
   reviewed — committing with message: …" plus their go-ahead suffices.)

2. **Never reply to PR comments without showing the exact text first.**
   Draft every reply, show each one verbatim alongside the comment it
   answers, and post nothing until the human approves. This covers review
   replies, discussion comments, and resolving threads. The human may edit
   or veto individual replies — post only what survives.

3. **Never push without prompting the human first.** Approval to commit is
   not approval to push. Ask explicitly before every `git push` (any
   remote, any branch), stating what will be pushed and where.

If any of these actions would happen "incidentally" via another command
(a tool that auto-commits, a `gh` command that posts), the gate applies to
that command too.

## Showing code — the rules

- **Before contract approval (tier 2+):** no implementation code in the
  conversation. Pseudocode, interface sketches, and "here's roughly the
  shape" are allowed inside the contract itself.
- **During implementation:** show snippets only to support a question or an
  amendment proposal.
- **At present:** full diff walkthrough mapped to criteria, always.

## Devboard sync

Devboard is the human's dashboard — a browser view rendered from task files
(see `devboard/schema.md` in the claude-skills repo). **If the devboard data
dir exists** (`$DEVBOARD_DATA`, default `~/.local/share/devboard/`), keeping
the task file current is part of the workflow, not optional polish. A stale
dashboard misleads the human; that's worse than no dashboard.

The file: `<data-dir>/<repo-name>/<task-slug>.yaml`, `schema: 1`. Always
write atomically (temp file + rename). Mandatory sync points:

1. **Intake (tier 1+):** create the file — `title`, `tier`, `phase`,
   `branch`, and `session` from `$CLAUDE_CODE_SESSION_ID` (this powers the
   dashboard's `claude --resume` button).
2. **Every phase transition:** update `phase`.
3. **Contract agreed:** mirror acceptance criteria into `scorecard`
   (status `pending`); plan steps into `plan` when the plan is derived.
4. **During implement:** flip `plan` item states as they change; append to
   `decisions` when a real decision or amendment is made; add a `code`
   entry when a load-bearing change lands.
5. **The moment anything waits on the human** — a question, or a hard
   checkpoint (commit summary, PR replies, push) — add a `needs_you` entry
   with the substance in `detail`. **Remove it as soon as it's resolved**;
   a stale needs_you poisons the attention queue.
6. **Verify:** set each `scorecard` status to `pass`/`fail` as checks run.
7. **Done:** set `phase: done`, clear remaining `needs_you`. (Deleting the
   file is the human's call, not the agent's.)

Tier 0 tasks skip devboard. If the data dir doesn't exist, skip silently —
never create it unprompted.

## Amendments

Contracts change; they just don't change silently. Any scope, criteria, or
deliverable change gets a one-line amendment proposal and a human yes. Log
amendments in the contract file (tier 2+) so the document stays the source
of truth.
