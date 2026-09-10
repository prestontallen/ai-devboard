# ai-devboard

Skills for structured software development with Claude Code.

| Component | Purpose |
|-----------|---------|
| [dev-context](worklog/skills/dev-context/SKILL.md) | **Entry point skill.** The workflow: task intake → clarify → research (optional) → contract → plan → implement → verify → present. Investigation-first work takes the collapsed spike track (intake → research → present → done). Defines task tiers, human checkpoints, and when code is shown. |
| [contract](worklog/skills/contract/SKILL.md) | Skill: generate a work contract from any task — scope, acceptance criteria, definition of done — scaled to the task's tier. Invoked from dev-context phase 4; also usable standalone for scoping. |
| [fan-out](worklog/skills/fan-out/SKILL.md) | Skill: patterns for spawning parallel subagents — seeding (led/blind), diversity, aggregation, gates — throttled by the task's complexity rating. Instances: the contract-phase risk scout and the research sweep. |
| *tone hook* | Not a repo component: dev-context's ship phase drafts all outbound text (commits, PR text, replies, messages) using whatever personal `*tone*` skill is installed on the machine, falling back to a lead-with-the-point default. Personal tone skills are gitignored here; install.sh checks for one and reports. |
| [worklog](worklog/README.md) | System of record: Go CLI + skill for the persistent task journal at `~/.local/share/worklog/` (tickets, epics — archivable via `done` once all children complete — a cap-exempt `## Waiting` section, archives, notes, search, standup, and `edit` for fixing fields in place). Also devboard's privileged writer: `start`/`done`/`pr` mirror automatically, and the `worklog task` family edits in-flight dashboard state. Imported from `day2day` with history; skill files deploy via `install.sh`, or `worklog install` once a binary exists. |
| [devboard](devboard/README.md) | Live telemetry: dashboard served by `worklog serve` (frontend embedded in the Go binary, vendored Preact/HTM included — npm is dev/test-only and never required to build or run; systemd user unit via `worklog install`) rendering per-task state (plan, scorecard, decisions, code-to-know) plus live worklog notes, all read from the worklog store. Archive and un-archive is the board's one write action. |

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
- **Complexity** (low / medium / high) is the second axis, rated at intake:
  uncertainty and blast radius, not size. It throttles fan-out depth —
  at medium+, the contract phase runs the **risk scout** (lensed read-only
  subagents sweeping the draft scope), and findings are folded into the
  contract before the human ever sees it; blocker-severity findings gate
  approval.
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
- Tier 2+ contracts are written to
  `~/.local/share/contracts/<repo>/<date>-<slug>.md`, outside the target
  project ([template](worklog/skills/contract/references/contract-template.md)). They sit
  next to the worklog data rather than inside it, so worklog's
  CLI-only-mutation rule keeps applying to worklog data alone.

## Devboard

[devboard/](devboard/README.md) is the human's live dashboard:
`worklog serve` renders task files from `~/.local/share/devboard/<repo>/` —
plan, contract scorecard, decisions, code-to-know, a "needs you" attention
queue, a "waiting on" queue for questions out with other people,
tier/complexity/worklog badges, a copy-`claude --resume` button per task,
and (for tasks with a `worklog:` join key) the ticket's worklog notes
rendered live. Hot-reloads whenever the store changes. Finished and parked
work stays out of the way: done tasks collapse into a fold below the grid,
the attention queues collapse into slim folds, and the board's one write
action, archive and un-archive, is a flag on the ticket.

All updates flow through the worklog CLI, never hand-edited YAML
("Devboard sync" in dev-context): `worklog start/done/pr` mirror lifecycle
state automatically, and `worklog task` (`complexity`, `phase`, `plan`,
`scorecard`, `decision`, `amend`, `scout`, `code`) edits the in-flight
detail. Every command writes the store, always: there is nothing to set up
and nothing to opt into. Schema and field-ownership rules:
[devboard/schema.md](devboard/schema.md).

## Install

```sh
./install.sh            # obtain worklog, deploy skills, prep devboard
./install.sh --check    # report drift (exit 1 if anything differs)
./install.sh --dry-run  # show what would happen
```

## Uninstall

```sh
worklog uninstall                 # print the plan; change nothing
worklog uninstall --commit        # remove the artifacts, keep your data
worklog uninstall --commit --purge-data    # remove the data too
./install.sh --uninstall --commit          # same thing, via the bootstrap
```

A dry run is the default, as it is for `worklog adopt`. Your worklog
corpus, store and contracts are preserved and listed unless you pass
`--purge-data`; superseded pre-store layouts are reported separately and
removed only with `--purge-legacy`.

Removal is deliberately conservative where this tool's files sit beside
yours. Only skills it deployed are removed, so a third-party skill in the
same directory survives; only its own handler leaves `settings.json`, so
your other hooks and settings stay; and only the marked block leaves
`~/.claude/CLAUDE.md`, so your own text is kept. An unmarked directive is
reported and left alone rather than guessed at. The binary is removed
last, so a failure part-way through never leaves a session hook pointing
at a file that is gone.

Linux/macOS (Windows → WSL). Go is OPTIONAL: the bootstrap downloads the
latest release binary for your platform (sha256-verified) and falls back
to a local `go build` when the download isn't possible; dev machines with
a dev-stamped binary stay on the build path. Docker optional (devboard's
fallback deployment; the primary is a systemd user unit).

**Skill targets**: the first interactive run detects local AI agent dirs
(`~/.claude`, `~/.cursor`, `~/.windsurf`, `~/.codex`), prompts per target,
and accepts additional custom paths; the selection persists to
`~/.config/ai-devboard/targets` (one path per line — edit it, or rerun
interactively, to change). Non-interactive runs use the saved config, or
fall back to detection when none exists. Every target gets the same
treatment: full copies of all four skills (dev-context, contract, fan-out,
worklog), with drift caught by `--check` and healed by re-running after
repo edits. The `/worklog` command file is a Claude-Code-only extra.

Prompts before the opt-in pieces (global CLAUDE.md directive, devboard
container); warns when no personal `*tone*` skill is installed.

## Wiring (how Claude Code picks these up)

Two layers, both required:

1. **Discoverability** — skills must live in each agent's skills dir to
   be invocable. `install.sh` copies all four skills to every configured
   target (see Install); after editing skills in the repo, re-run it (or
   `--check` to see drift).

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

- Should tier/complexity classification get mechanical heuristics (files
  touched, risk), or stay judgment-based?
- When to promote the CLAUDE.md directive from repo-local to global.
- Onboarding (`csk-onboarding`, Someday): propose-and-curate import of
  existing work; risk-scout findings already captured in its worklog
  notes.
