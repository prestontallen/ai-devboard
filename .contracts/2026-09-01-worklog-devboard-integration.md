# Contract — Worklog ↔ Devboard integration

- **Date:** 2026-09-01
- **Tier:** 3 (Major)
- **Status:** in-progress (agreed 2026-09-01)
- **Worklog:** csk-integration (epic)

## Intent

Three systems now live in one repo — the workflow skills, devboard (live
dashboard), and worklog (system of record) — but they are wired together by
prose: the skills tell agents to hand-edit devboard YAML at seven sync
points, worklog knows nothing about devboard, and worklog notes are
invisible to the dashboard. This work makes the **worklog binary the
privileged writer of devboard task files** (lifecycle side effects + an
explicit `task` subcommand family), renders worklog notes **live** in
devboard (read, never copied), and rewrites the skills' sync instructions
into CLI one-liners. Governing model, agreed 2026-09-01: worklog = system
of record; devboard = disposable live telemetry; **every shared field has
exactly one author** and mirroring flows worklog→devboard only. Worklog
stays the privileged writer, never a required one — a bare schema-valid
YAML dropped by hand must keep working.

## Scope

**In:**
- M1 Wiring: deploy skill files via `worklog/scripts/sync.sh` (fixes the
  missing `~/.claude/skills/worklog/` mirror); top-level README covers
  `worklog/`; note in `worklog/README.md` that the repo moved into
  claude-skills
- M2 Schema: `worklog:` join field + one-author ownership table in
  `devboard/schema.md`; UI renders the worklog id as a badge
- M3 Writer: `internal/devboard` package in the Go module (atomic
  read-modify-write of task files, schema v1); side effects on
  `worklog start` (create/update task file: title, worklog id, session
  from `$CLAUDE_CODE_SESSION_ID`), `worklog done` (phase done, clear
  needs_you), `worklog pr` (links entry); new `worklog task` family:
  `phase`, `plan add|start|done|block`, `scorecard add|pass|fail`,
  `decision`, `needs-you add|resolve`, `code` — all with `--json`,
  following the repo's existing exit-code/JSON conventions (PLAN.md)
- M4 Notes: devboard container gains a second read-only mount (worklog
  data dir); a task with a `worklog:` id renders `notes/<id>.md` in a
  Notes section; note edits hot-reload like task files
- M5 Skills: dev-context "Devboard sync" + contract "Devboard integration"
  sections rewritten to use the CLI; `worklog/skill/SKILL.md` gains a
  devboard section; redeployed via sync.sh
- M6 Install: `install.sh` at repo root — one command from fresh checkout
  to working setup. Detects OS (reusing `worklog/scripts/detect-platform.sh`),
  preflights the toolchain (Go required; Docker optional, warn-only), builds
  the worklog binary and installs it into a PATH dir (`~/.local/bin`,
  warn if not on PATH), deploys ALL skills from the repo — worklog
  SKILL/command via sync.sh plus dev-context and contract — replacing any
  older copies, creates the devboard data dir, and offers (prompt, never
  forces) the CLAUDE.md directive and devboard container build. Modes
  matching sync.sh conventions: default, `--check`, `--dry-run`.
  First-class targets: Linux and macOS; Windows is detected and directed
  to WSL with a clear message (see Q4)
- M7 Risk scout: extend the intake/contract process with (a) a difficulty
  rating alongside the tier, and (b) a fan-out of subagents at contract
  time that hunt for blockers, unexpected side effects, and impact on
  downstream consumers — anything that would slow the work or surprise us
  later — feeding findings into the contract's risks/scope. Deliberately
  under-specified: per the tier-3 process, M7's mini-contract settles the
  design (agent count, prompts, how findings gate the contract) when its
  turn comes

**Out (explicitly not doing):**
- Rendering WORK.md Now/Next as the dashboard home view (v-next)
- Any devboard→worklog write path (one direction only)
- Requiring a worklog ticket for devboard tasks (bare YAML stays legal)
- UI write actions, auth, multi-user (unchanged from devboard contract)
- day2day GitHub repo disposition (archive vs mirror — human decision)
- Pushing this repo anywhere; remote setup is separate
- TUI changes in worklog

## Deliverables

- `worklog/internal/devboard/` (+ tests), `worklog/internal/cli/task.go`
  (+ tests), side-effect edits in `start.go`/`done.go`/`pr.go`
- `devboard/schema.md` v1 additions; `devboard/server.py`,
  `devboard/static/index.html`, `devboard/compose.yaml` (notes mount)
- Edited: `dev-context/SKILL.md`, `contract/SKILL.md`,
  `worklog/skill/SKILL.md`, both READMEs
- Rebuilt `worklog` binary installed to `~/.local/bin` (Makefile path)
- `install.sh` at repo root (+ any helpers under `scripts/`)

## Acceptance criteria

| # | Criterion | Verify | Status |
|---|-----------|--------|--------|
| 1 | `sync.sh --check` exits 0 and `~/.claude/skills/worklog/SKILL.md` exists and matches the repo copy | run it; diff | ☐ |
| 2 | When `worklog start <id>` runs in a git repo and the devboard data dir exists, a task file appears at `<data>/<repo>/<id>.yaml` with title, `worklog: <id>`, and `session` from the env | run in a throwaway repo; cat file | ☐ |
| 3 | When `worklog done <id>` runs, the task file's phase becomes `done` and `needs_you` is emptied | run; cat file | ☐ |
| 4 | When `worklog pr <id> <url>` runs, the task file gains a `links` entry with that URL | run; cat file | ☐ |
| 5 | `worklog task needs-you add/resolve`, `plan …`, `scorecard …`, `decision`, `phase`, `code` each mutate exactly their field, atomically, and support `--json` | package + CLI tests; manual spot-check | ☐ |
| 6 | While the devboard data dir does NOT exist, every side effect and `task` command is a silent no-op with exit 0 (explicit `task` prints a notice to stderr) | unset dir in test env; run all | ☐ |
| 7 | When the target task file is malformed YAML, `task` commands fail with a clear error and leave the file byte-identical | corrupt a file; run; cmp | ☐ |
| 8 | Concurrent `task` invocations never produce a torn/corrupt file | stress test in package tests (parallel writers) | ☐ |
| 9 | A devboard task with `worklog: <id>` renders `notes/<id>.md` in a Notes section; editing the note updates the open page within 2s, no reload | browser check | ☐ |
| 10 | A task whose `worklog:` id has no notes file, or with no `worklog:` at all, renders without a Notes section and without errors | browser check | ☐ |
| 11 | A hand-dropped schema-valid YAML with no worklog ticket still renders fully (no regression) | drop example; browser check | ☐ |
| 12 | dev-context/contract skill sync sections contain only CLI invocations (no hand-YAML instructions); commands they cite exist verbatim in `worklog task --help` | grep skills vs help output | ☐ |
| 13 | Rebuilt binary reports the new version and `go test ./...` passes in `worklog/` | `worklog --version`; go test | ☐ |
| 14 | On Linux with Go present, `./install.sh` against a sandbox `$HOME` builds and installs the binary (`worklog --version` works from PATH) and deploys all four repo skill surfaces (worklog SKILL + command, dev-context, contract); it checks for a personal `*tone*` skill, warning (not failing) when none is found | run with `HOME=<tmpdir>`; diff each deployed file; check warning in bare sandbox | ☐ |
| 15 | A second `./install.sh` run reports up-to-date and changes nothing; `--dry-run` prints intended actions and touches no files; `--check` exits 0 when current, 1 when drifted | run twice; mtime/diff audit | ☐ |
| 16 | When Go is missing from PATH, install fails before any file change with a clear message and nonzero exit; when Docker is missing, install completes with a warning | PATH-stripped env runs | ☐ |
| 17 | On an unsupported platform combination, the script exits with an explanatory message (Windows → WSL pointer), never a half-install | fake `uname` via stub on PATH | ☐ |

## Definition of done (standing bar)

- [ ] All worklog Go tests pass (existing + new); no new deps beyond what
      the module already carries unless amended
- [ ] Devboard keeps zero deps beyond python-alpine + PyYAML
- [ ] No unrelated changes in the diff; commits per milestone
- [ ] READMEs and schema.md updated where behavior is user-facing
- [ ] Malformed input can never blank the dashboard or corrupt a store

## Constraints & assumptions

- Devboard data dir from `$DEVBOARD_DATA`, default `~/.local/share/devboard`
- Repo name for the task-file path derives from the git repo basename of
  the cwd; `--repo` flag overrides (assumption — flag shape settled at plan)
- `worklog start` does NOT set phase (phases are agent-driven via
  `task phase`); it only guarantees the file exists with identity fields
  (assumption)
- Notes render as plain markdown (monospace block or minimal md-to-HTML);
  full markdown fidelity is not required in this contract

## Risks & open questions

- **Q1:** milestone commits land on `master` directly, or a branch per
  milestone? (assumed: master, single-user repo)
- **Q2:** should `worklog done` also set every pending scorecard entry to
  `fail`-if-unverified, or leave untouched? (assumed: leave untouched)
- **Q3:** notes files can be large — render full file or last N lines with
  a "full" toggle? (assumed: full file, revisit if slow)
- **Q4:** Windows: native support means %USERPROFILE% skill paths, no
  symlinks, and a .exe build — real surface area. Assumed: detect and
  point to WSL for now; native Windows is v-next if ever needed
- **Q5:** skill deployment mode — dev-context/contract are currently
  symlinked (repo edits apply live); worklog's sync.sh copies. Assumed:
  install.sh standardizes on symlinks for repo-local installs so the whole
  repo behaves like a dev checkout; sync.sh copy mode remains for
  non-checkout deploys
- Verification honesty: macOS path in install.sh is reviewed but not
  executed (no macOS host here); Linux is the tested platform
- Risk: YAML round-tripping in Go can reorder/reformat hand-written files —
  mitigated by tests asserting comment-free schema fields survive; noted
  limitation: comments in task files are not preserved

## Milestones

1. **M1 Wiring** — sync deployed, READMEs coherent · checkpoint: review of
   file moves + README diff
2. **M2 Schema** — join field + ownership table + badge · checkpoint: schema
   diff review
3. **M3 Writer** — Go package, side effects, `task` family · checkpoint:
   demo transcript of criteria 2–8 + code review
4. **M4 Notes** — second mount, live notes rendering · checkpoint: browser
   demo of criteria 9–11
5. **M5 Skills** — CLI-based sync sections, redeployed · checkpoint: skill
   diff review
6. **M6 Install** — install.sh, sandbox-verified · checkpoint: transcript
   of criteria 14–17 + script review
7. **M7 Risk scout** — difficulty rating + subagent blocker sweep in the
   skills · checkpoint: mini-contract (with its own criteria) approved at
   milestone start, then skill diff review; contract closes

## Amendments

| Date | Change | Why | Approved |
|------|--------|-----|----------|
| 2026-09-01 | M7 Risk scout milestone added: difficulty rating + subagent blocker/side-effect sweep at contract time; criteria deferred to M7's mini-contract | Requested during M4→M5 transition; design intentionally deferred | yes |
| 2026-09-01 | Tone hook added: dev-context ship phase drafts outbound text via any installed `*tone*` skill, with a lead-with-the-point fallback; personal tone skills stay out of the repo (gitignored); install.sh (M6) checks for one and warns when absent | Tone is personal/per-context (the human's concise-tone skill carries work-specific footers); process defines the hook, the person supplies the voice | yes |
