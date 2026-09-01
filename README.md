# claude-skills

Skills for structured software development with Claude Code.

| Component | Purpose |
|-----------|---------|
| [dev-context](dev-context/SKILL.md) | **Entry point skill.** The workflow: task intake → clarify → contract → plan → implement → verify → present. Defines task tiers, human checkpoints, and when code is shown. |
| [contract](contract/SKILL.md) | Skill: generate a work contract from any task — scope, acceptance criteria, definition of done — scaled to the task's tier. Invoked from dev-context phase 3; also usable standalone for scoping. |
| *tone hook* | Not a repo component: dev-context's ship phase drafts all outbound text (commits, PR text, replies, messages) using whatever personal `*tone*` skill is installed on the machine, falling back to a lead-with-the-point default. Personal tone skills are gitignored here; install.sh checks for one and reports. |
| [worklog](worklog/README.md) | System of record: Go CLI + skill for the persistent task journal at `~/.local/share/worklog/` (tickets, epics, archives, notes, search). Imported from `day2day` with history; deploys its skill files via `worklog/scripts/sync.sh`. |
| [devboard](devboard/README.md) | Live telemetry: dockerized dashboard rendering per-task state (plan, scorecard, decisions, needs-you) from `~/.local/share/devboard/`. |

Ownership model: worklog is the durable system of record, devboard is
disposable live telemetry; every shared field has exactly one author, and
mirroring flows worklog→devboard only (worklog is devboard's privileged
writer, never a required one).

## How it fits together

- **dev-context is the entry point.** Every dev task starts there; it
  classifies the task and drives the phases. contract is a dependency it
  calls, not a parallel starting point.
- **Tiers** (0 trivial / 1 small / 2 feature / 3 major) are the shared
  scaling mechanism: they decide contract depth and how many checkpoints
  the human sees. Defined in dev-context, consumed by contract.
- **Acceptance criteria vs definition of done**: criteria are per-task and
  observable ("when X, then Y", each with a verification command); the DoD
  is the standing project bar, reused unchanged.
- The human approves the contract *before* implementation code exists
  (tier 2+), and sees code *after* it's verified — presented as a scorecard
  against the contract, not a raw dump.
- **Hard checkpoints** (all tiers, never scaled away): no commit before the
  human sees a work summary covering how the feature was implemented; no PR
  comment replies without showing the exact text for approval; no push
  without an explicit prompt — approval to commit is not approval to push.
- Tier 2+ contracts are written to `.contracts/<date>-<slug>.md` in the
  target project ([template](contract/references/contract-template.md)).

## Devboard

[devboard/](devboard/README.md) is the human's live dashboard: a Docker
container renders task files from `~/.local/share/devboard/<repo>/` —
plan, contract scorecard, decisions, code-to-know, a "needs you" attention
queue, and a copy-`claude --resume` button per task. Hot-reloads on file
change. The skills treat keeping these files current as part of the
workflow ("Devboard sync" in dev-context): file created at intake, phase
tracked, scorecard mirrored at contract-agreed and updated at verify, and
`needs_you` entries added the moment anything waits on the human — and
removed when resolved. Sync activates only when the data dir exists.

## Wiring (how Claude Code picks these up)

Two layers, both required:

1. **Discoverability** — skills must live under `~/.claude/skills/` to be
   invocable. Symlink them so repo edits apply immediately:

   ```sh
   ln -sfn ~/claude-skills/dev-context ~/.claude/skills/dev-context
   ln -sfn ~/claude-skills/contract ~/.claude/skills/contract
   ```

2. **Guaranteed pickup** — skill invocation is normally probabilistic (the
   model matches task against description). To make it deterministic, a
   CLAUDE.md directive tells Claude to invoke dev-context before any dev
   task. [CLAUDE.md](CLAUDE.md) in this repo holds that directive; deploy it
   where you want the workflow enforced:
   - **Globally** (every session): copy/append into `~/.claude/CLAUDE.md`.
   - **Per-project**: copy/append into the repo's `CLAUDE.md`.
   - Currently kept repo-local only, while the skills are under development.

   Caveat: CLAUDE.md is a top-of-context instruction, followed very
   reliably, but not a hard harness gate — nothing can *force* a skill
   invocation. The graceful failure mode is noticing code appear without a
   contract. A `UserPromptSubmit` hook injecting a per-message reminder is
   the belt-and-suspenders escalation if drift is ever observed.

## Design notes

- Influences: GitHub Spec Kit's spec→plan→tasks→implement pipeline with
  artifacts and gates, the acceptance-criteria/DoD split from
  agent-workflow references (addyosmani/agent-skills), EARS requirement
  phrasing for tricky criteria.
- Contract lifecycle: draft → agreed → in-progress → fulfilled, with an
  amendments log — contracts change, but never silently.
- Worklog integration: a task's contract mirrors its acceptance summary
  into the worklog item's **Acceptance** field.

## Open questions

- Should `.contracts/` live in target repos, or centrally (worklog notes)?
- Should tier classification get mechanical heuristics (files touched,
  risk), or stay judgment-based?
- When to promote the CLAUDE.md directive from repo-local to global.
