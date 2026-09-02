# Worklog

> **Note:** this project moved into the
> [ai-devboard](../README.md) repo as `worklog/` (imported from
> `github.com/prestontallen/day2day` with history, 2026-09-01). It is the
> system of record behind the repo's dev workflow: the `worklog` binary is
> also the privileged writer for [devboard](../devboard/README.md) task
> files. Paths in this README are relative to `worklog/`.

A markdown-based personal task journal that survives across Cursor and Claude Code
sessions. The agent reads `WORK.md` at the start of every session, archives
completed tickets to monthly files, keeps long-form notes per epic, and maintains
a searchable index so old work isn't lost.

This README is for **you, the human**. The agent-facing instructions live in the
skill files (see "How the agent learns this system" below). If you're trying to
remember how to use the system, edit the policies, or recover from a broken
state, start here.

---

## What this solves

Three problems that compound across long-running engineering work:

1. **Cross-session amnesia.** Both Cursor and Claude Code start fresh every
   session. Re-explaining "where did I leave off?" eats the first five minutes
   of every conversation.
2. **WIP sprawl.** Without a forcing function, you end up with 7 half-done
   branches, no idea which is most important, and a Jira board that doesn't
   match reality.
3. **Lost institutional memory.** Three months from now, when a reviewer asks
   *"didn't we already deal with the CRDB partial-index quirk?"*, the answer
   is in a Slack thread no one can find. The archive solves that.

It is **not** a replacement for Jira, GitHub Issues, or your in-repo `*-plan.md`
files. It's the live state on top of those — the "today board" + searchable
historical record.

---

## File map

```
~/.local/share/worklog/
├── WORK.md            ← the front page; agent reads this every session
├── INDEX.md           ← search spine: by ticket / by tag / by repo / by month
├── FEEDBACK.md        ← captured friction events; read it with `worklog feedback`
├── archive/
│   └── YYYY-MM.md     ← completed tickets, full metadata + summary + feedback
└── notes/
    └── <id>.md        ← per-ticket and per-epic notes; an epic's canonical
                          child-ticket list lives here
```

This README is not in that tree — it lives in the repo at `worklog/README.md`.

| Path | What it is | Who writes it |
|---|---|---|
| WORK.md | Four sections: `## Now` (≤5 tickets), `## Waiting` (cap-exempt, parked on someone else), `## Next`, `## Someday`. Epic blocks live in `Next`/`Someday`; only child tickets and standalone tickets occupy `Now`. | The CLI, on every state transition |
| INDEX.md | Tag/ticket/repo/month index that points into archive and notes files | `worklog reindex` (derived; regenerated wholesale) |
| archive/ | One file per calendar month, `YYYY-MM.md`. Completed tickets get a structured entry with Summary + Feedback / Notes | `worklog done`, on ticket completion |
| notes/ | Long-form per-ticket / per-epic notes. The `[ ]` checkbox child list inside an epic's notes file is the **canonical backlog** for that epic. | `worklog note`, plus `add`/`done` maintaining the child list |
| FEEDBACK.md | Friction events the agent captured during sessions (missing feature, TUI error, frustration) | `worklog feedback append` |

The agent and slash command live elsewhere — see "How the agent learns this
system" at the bottom.

---

## How you actually use it day-to-day

The agent does almost everything. You mostly just talk to it.

| You say | Agent does |
|---|---|
| (start of any session) | Reads `WORK.md`, summarizes `## Now` and any epic in `Next` with active children before asking what you want |
| *"I'm starting ENT-3794"* | Cap-checks `Now`, pulls metadata from `notes/ent-3634.md`, creates a ticket block in `Now` with `Parent: ent-3634`, flips `[ ]` -> `[~]`, adds `Started: YYYY-MM-DD`, updates the epic's `Active children` |
| *"ENT-3794 is merged, PR was #4521"* | Asks for a one-line summary + any feedback/notes; writes archive entry to `archive/2026-05.md`; ticks `[x]` in `notes/ent-3634.md`; prunes `Active children`; removes block from `WORK.md`; updates `INDEX.md`. If it was the last child, asks whether to also archive the epic. |
| *"have we hit anything weird with CockroachDB partial indexes?"* | Greps `INDEX.md`, then expands into the matching archive/notes file, cites the source |
| *"add a follow-up to fix the typo in INDEX.md"* | Appends a `[ ]` block to `## Next` |
| *"add an epic for the new analytics export feature"* | Creates a container block in `## Next` and a new `notes/<id>.md` skeleton |
| *"park ENT-3794, it's waiting on review"* | Moves it `Now` -> `## Waiting` and stamps `Waiting since`, freeing a `Now` slot; `start` again to resume |
| *"that ticket's repo is wrong — it's assessments-api"* | `worklog edit <id> --repo assessments-api`, in place, no re-creation |

For explicit, scripted invocations: `/worklog [status|start|done|add|note|search]`
in Claude Code (see [~/.claude/commands/worklog.md](skill/claude/command.md)).

### Everything goes through the CLI

The worklog is CLI-mutated, not hand-mutated — by you and by the agent
alike. The data files are plain markdown and nothing stops you editing
them, but a hand edit is how `INDEX.md`, an epic's `Active children`, and
the parent's notes checkbox drift apart, and the agent's hard rules forbid
it outright. There's a verb for the everyday corrections:

| Want to... | Use |
|---|---|
| Fix a title, repo, tags, files, acceptance, or status line | `worklog edit <id> --<field> "<value>"` (empty value removes the line) |
| Add freeform context to a ticket or epic | `worklog note <id> "<text>"`, or `worklog note <id> --editor` for real editing |
| Retire something you no longer care about | `worklog done <id> --summary "dropped: <why>"` — it lands in the archive, not the bin |
| Park work that's blocked on someone else | `worklog wait <id>` |
| Fix a drifted index | `worklog reindex` |

What the CLI won't do is deliberate, not a gap to route around: it won't
delete an archive entry, won't move a ticket between sections outside the
lifecycle verbs, and won't let `## Now` exceed 5. If you need one of those,
say so out loud rather than reaching for the file — and run
`worklog validate` after any surgery to confirm the invariants still hold.

---

## Hard rules (one-page version)

These are the rules the agent will refuse to violate. Knowing them lets you
spot when the agent is acting weird and call it out.

1. **Worklog data is mutated only through `worklog` subcommands**, never by
   editing the files directly. Live tickets are corrected with
   `worklog edit <id>`; anything the CLI refuses is a deliberate limit, and
   the agent will say so rather than reach for the file.
2. **`## Now` is capped at 5 tickets.** Epics never occupy `## Now`;
   `## Waiting` is exempt from the cap.
3. **Completion is move-then-delete, atomic.** A completed ticket only leaves
   `WORK.md` after `archive/YYYY-MM.md` and `INDEX.md` are both updated.
4. **No top-level `[x]` lingers in `WORK.md`.** `[x]` is transient — it appears
   only between "I'm done with this" and "the move-to-archive finished".
5. **The epic↔child relationship is consistent in three places**:
   - The child's checkbox in `notes/<epic>.md`
   - The parent's `**Active children**:` in `WORK.md`
   - The child's `**Parent**:` field in its `## Now` block
6. **`INDEX.md` is updated on every add / archive / relocate.** Stale index
   means search misses things.
7. **Notes files reference in-repo plans, not duplicate them.** If the truth
   moves, only one place needs updating.
8. **Nothing in the worklog is silently deleted, ever.**

---

## Customization points

If you want to change behavior, here's where to look. There's no config file —
behavior is defined in markdown that the agent reads.

Edit the repo copies under `skill/`, never the deployed ones — a
redeploy overwrites those. `worklog sync --check` tells you when the two
have diverged.

| Want to change... | Edit this file | Look for |
|---|---|---|
| The cap of 5 | WORK.md policy block + [skill](skill/SKILL.md) "Hard rules" + [command](skill/claude/command.md) + this README's rules block | the literal number 5 |
| Section names (Now/Waiting/Next/Someday) | All of the above (rename consistently) | section headings |
| Ticket block format / required fields | [skill](skill/SKILL.md) "Ticket block format" | the markdown code fence |
| Archive entry format | [skill](skill/SKILL.md) "Archive entry format" | the markdown code fence |
| Auto-trigger aggressiveness | [skill](skill/SKILL.md) frontmatter `description:` (richer triggers = more eager auto-invoke) | YAML frontmatter |
| Where the data tree lives | Runtime: `--dir <path>` or `$WORKLOG_DIR` (flag wins). Permanently: the path is quoted in `skill/SKILL.md`, `skill/claude/command.md`, and this README — ripgrep for it. | `~/.local/share/worklog` |
| Add a new persistent section (e.g. `## Blocked`) | WORK.md seed + [skill](skill/SKILL.md) "Required behavior" | section list |

After editing the skill files in the repo, redeploy:

```bash
worklog sync           # deploy this skill to every configured target
worklog sync --check   # verify the deployed copies match the repo
../install.sh          # deploy all four skills (dev-context, contract, fan-out, worklog)
```

The repo's `skill/` dir is the single source of truth. `worklog sync`
reads the install config (`~/.config/ai-devboard/targets`) and never
writes to a target you declined; with no config it falls back to
`~/.claude` and `~/.cursor` for standalone worklog use. The top-level
`install.sh` → `worklog install` flow deploys this skill along with every
other one, and is the only path that reaches a target you added by hand.

`scripts/sync.sh` is the pre-CLI version of the same thing, kept for
bootstrapping a machine with no binary yet. It deploys only this skill and
only to the two hardcoded paths — it does not read the install config, so
prefer `worklog sync` once a binary exists.

---

## Maintenance & recovery

### Backups

There's no git history (you opted out). Three options if you change your mind:

1. **`git init` the worklog directory.** Pro: free history. Con: you have to
   remember to commit.
2. **Time Machine + macOS file history.** Probably already running.
3. **Periodic `tar` snapshot to iCloud Drive or similar.** Cron-friendly:
   ```bash
   tar -czf ~/Library/Mobile\ Documents/com~apple~CloudDocs/worklog-$(date +%Y%m%d).tgz -C ~/.cursor worklog
   ```

### If `WORK.md` is corrupt or missing

The skill is configured to **stop and report**, never silently recreate. Steps:

1. Check `archive/` and `notes/` — they're untouched and contain enough state
   to rebuild a sensible `WORK.md`.
2. Open the most recent archive file and the notes files for any in-flight
   epics. Reconstruct the `## Now` and `## Next` sections by hand.
3. Or ask the agent: *"WORK.md is broken. Reconstruct it from the notes and
   archive."* It will read those files, propose a new `WORK.md`, and ask you
   to confirm before writing.

### If `INDEX.md` drifts

Symptom: the agent says *"I don't see anything about X"* but you remember
working on X. Recovery:

1. `rg -i "X" ~/.local/share/worklog/` will find it directly.
2. Ask the agent: *"Rebuild INDEX.md from scratch by scanning all archives
   and notes files."* It can regenerate the index deterministically.

### If the cap is silently violated

Symptom: `## Now` has 6+ tickets. Probably means the agent skipped the cap
check on a fast turn.

Fix: ask *"What's in `## Now`? Anything we should demote or finish?"* The
agent will surface the conflict and the right next step.

---

## Cross-machine sync

The worklog is local-only by default. If you want it on multiple machines:

- **Easy**: `git init` and push to a private repo.
- **Easier**: drop `~/.local/share/worklog/` inside iCloud Drive / Dropbox /
  Syncthing and update `WORK.md`'s path references in the skill / rule /
  CLAUDE.md.
- **Hardest**: don't sync, use `WORK.md` only on your primary machine. (This
  is what's set up now.)

If you sync via cloud storage, beware of conflict files (`WORK 2.md`). The
agent doesn't know how to resolve those; you'd have to merge by hand.

---

## How the agent learns this system

The installer (`install.sh` → `worklog install`) puts a copy of
[skill/SKILL.md](skill/SKILL.md) in every configured target. Targets are
detected on the first interactive run (`~/.claude`, `~/.cursor`,
`~/.windsurf`, `~/.codex`), confirmed one by one, and remembered in
`~/.config/ai-devboard/targets` — one path per line, editable by hand, so
an agent that isn't auto-detected just gets a line added.

| What lands where | Path |
|---|---|
| The skill, once per configured target | `<target>/worklog/SKILL.md` |
| The `/worklog` slash command (Claude Code only) | `~/.claude/commands/worklog.md` |

Every other agent relies on skill auto-invocation instead of a slash
command. There is no Cursor `.mdc` rule file today — `internal/sync` keeps
the hook for one if reinforcement ever proves necessary.

The skill description is loud and includes trigger phrases ("what am I working
on", "log this", "have we hit X before") so semantic discovery fires reliably.
There's no `disable-model-invocation` flag — both agents are expected to
auto-invoke it.

If you ever want to turn the always-on behavior **off** without deleting the
skill: add `disable-model-invocation: true` to the SKILL.md frontmatter. The
skill will then only fire on explicit `/worklog` calls or when the agent
is told to use it by name.

---

## Design rationale (so future-you knows why)

We evaluated four prior-art systems before building this:

| System | Why we didn't just adopt it |
|---|---|
| [TASKS.md spec (tasksmd)](https://github.com/tasksmd/tasks.md) | Deletes completed tasks; relies on `git log -S` for archive. We wanted in-file searchable history. |
| [Sam French's TODO.md](https://samfrenchblog.com/2026/02/15/how-i-use-todo-md-with-claude-code-to-never-lose-context-between-sessions/) | Closest match. Lacks the cap, the epic/child split, the dedicated archive, and the index. |
| [work-plan-toolkit](https://github.com/stylusnexus/work-plan-toolkit) | Treats GitHub as canonical; we wanted the markdown to be canonical because not every task has a GitHub presence. |
| [Worklog Claude Code Skill (mcpmarket)](https://mcpmarket.com/tools/skills/unified-worklog-system) | Auto-synthesizes daily logs from session activity. Useful for standups but doesn't model an active task list. |

Key decisions and their reasons:

- **5-ticket cap, not 3**: your epic phases naturally run 3 parallel children
  (worktrees), and you sometimes have 2 epics in flight. 3 is too tight; 5 is
  a real WIP limit without being theatrical.
- **Epics in `Next`, children in `Now`**: makes the cap a true Kanban WIP
  limit on actual units of work, while still surfacing epic-level progress
  via `Active children`.
- **Archive lives in monthly files, not git log**: human-readable, scannable,
  and the file format itself is the source of truth — you don't need git to
  read it.
- **`INDEX.md` separate from raw files**: lets the agent grep cheaply on
  every search query instead of scanning the whole archive every time.
- **Copies per agent, not symlinks**: simpler than configuring symlinks
  across each agent's skill loader, and a copy can't break when the
  checkout moves. The cost is a redeploy after edits, which is why
  `sync --check` and `install.sh --check` report drift.

---

## Known limitations

1. **Concurrent agents racing.** If you have a Cursor session and a Claude
   Code session both modifying `WORK.md` at the same instant, last-write-wins.
   In practice you only run one at a time, so this is theoretical.
2. **Drift between `INDEX.md` and reality.** Possible if the agent skips the
   index update step. Recoverable (see "Maintenance & recovery").
3. **The agent has to be trusted to follow the rules.** `worklog validate`
   checks the structural invariants after the fact (cap, lingering `[x]`,
   three-place epic consistency, dangling index refs), but nothing
   *prevents* a rule violation at the moment it happens. The hard rules in
   `SKILL.md` are still enforced by convention, and by you noticing when
   they're broken.
4. **The rules text lives in three copies** — this README, `skill/SKILL.md`,
   and `skill/claude/command.md` — each written for a different reader, and
   nothing checks that they still agree. If they drift far enough to matter,
   collapse them to one source rather than reinstating a comparison.
5. **No cross-device locking.** If you sync to iCloud and edit on two
   machines simultaneously, you'll get a conflict file. The agent doesn't
   resolve those.
6. **No automation around Jira/GitHub.** When you say *"ENT-3794 is merged"*,
   the agent records it in the worklog but doesn't update Jira or post to
   the PR. Wire that up via your existing commit-and-pr workflow if you want.
7. **Notes files are not validated.** Nothing checks that the child checklist
   in `notes/<epic>.md` matches the children referenced in `WORK.md`. If you
   manually delete a child from the notes file, `INDEX.md` will still list it.

---

## Extending

### Add a new agent (e.g., Codex CLI)

1. Add its skills dir to `~/.config/ai-devboard/targets`, one path per
   line — or rerun `install.sh` interactively and pick it from the
   detected list (`~/.claude`, `~/.cursor`, `~/.windsurf`, `~/.codex`).
2. Rerun `install.sh` (or `worklog sync` for this skill alone). Both
   `--check` modes report the new target as drift until it's deployed.
3. Optionally add a reinforcement to that agent's global instructions file
   (whatever the equivalent of `CLAUDE.md` is) — skill discovery is
   probabilistic, a top-of-context directive isn't.

### Add a new section (e.g., `## Blocked`)

1. Add the section heading to `WORK.md` (`## Waiting` already sits between
   `Now` and `Next`). Scaffolding a new section is the one structural edit
   the CLI has no verb for.
2. Update the policy comment at the top of `WORK.md` to describe its purpose.
3. Update the "Required behavior" section in [SKILL.md](skill/SKILL.md)
   so the agent knows when to move things into `## Blocked`, then
   `worklog sync` to redeploy.

### Add a new metadata field (e.g., `**Estimate**:`)

1. Add it to the "Ticket block format" example in `SKILL.md`.
2. Optionally extend the archive entry format too.
3. The agent will start populating it after you ask once.

### Add a new agent command (e.g., `/worklog estimate <id>`)

1. Add the subcommand to [~/.claude/commands/worklog.md](skill/claude/command.md)
   under "Subcommands".
2. Document its behavior in 3–5 lines. The agent will follow the description.

---

## Cheat sheet

```
SESSION START
  agent reads WORK.md, summarizes Now + epics with active children

ADD A NEW STANDALONE TASK
  "add: refactor the auth middleware" -> appended to Next

ADD A NEW EPIC
  "add an epic: cross-region failover" -> Next + new notes/<id>.md

PROMOTE A CHILD OF AN EPIC TO ACTIVE
  "starting ENT-3794" -> Now (cap-checked), epic gains Active children entry

PROMOTE A STANDALONE TICKET TO ACTIVE
  "starting that auth middleware refactor" -> moves Next -> Now

PARK A BLOCKED TICKET
  "ENT-3794 is waiting on review" -> Now -> Waiting (cap-exempt), Waiting since
  stamped; "starting ENT-3794" moves it back

CORRECT A TICKET'S METADATA
  "its repo is assessments-api" -> worklog edit <id> --repo assessments-api

FINISH A TICKET
  "ENT-3794 is merged" -> archive entry, INDEX update, parent's checkbox ticked,
  Active children pruned, prompt for epic auto-archive if last child

ASK ABOUT PRIOR WORK
  "have we touched X" -> grep INDEX -> expand into archive/notes -> cite source

EXPLICIT COMMANDS (Claude Code)
  /worklog                  status (default)
  /worklog start <id>       promote to Now
  /worklog done <id>        archive
  /worklog add <desc>       new ticket; "epic:" prefix for epics
  /worklog note <id>        open/create notes/<id>.md
  /worklog search <term>    grep INDEX, then archive
```
