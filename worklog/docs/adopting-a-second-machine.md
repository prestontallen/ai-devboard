# Adopting a second machine

A handoff prompt for an agent on a machine whose worklog corpus predates
the SQLite store. Paste the block below into a session on that machine.

Provenance: written during `adb-migrate-render` and used successfully on
two machines as of 2026-09-04. Kept here as a starting point, not as a
specification — `worklog adopt --help` and
[`skill/references/adoption.md`](../skill/references/adoption.md) are the
authorities, and this prompt should be re-checked against them when the
command changes.

## Two notes for the operator, not the agent

- **Expect at least one refusal.** On the corpus this was developed against
  it refused exactly once, on a devboard title that disagreed with its
  ticket. A refusal means adoption found something it would otherwise have
  changed silently; it is the mechanism working.
- **The delete count in the plan looks worse than it is.** On that same
  corpus it was 19, of which 17 were halves of *moves* — a create and a
  delete for the same slug, healing repo attribution — and only 2 were true
  removals. Check that pairing before approving a commit.

## The prompt

````markdown
# Task: adopt this machine's worklog corpus into the SQLite store

## Background you need

This machine's worklog data (`~/.local/share/worklog/`, plus
`~/.local/share/devboard/` if it exists) predates a cutover in which a
SQLite store became the system of record. WORK.md, notes/, archive/,
FEEDBACK.md and the devboard YAML are now *rendered projections* of that
store, not sources.

Because this machine never went through that cutover, every `worklog` write
verb will refuse — you should see "this machine has not adopted the store
yet". That is expected. The fix is `worklog adopt`.

## Step 0 — get a binary that has `adopt`

`adopt` shipped in v0.12.0. Check what you have:

    worklog --version
    worklog adopt --help

If the version is below 0.12.0, update. If this machine was set up from a
checkout of https://github.com/prestontallen/ai-devboard, prefer:

    git pull && ./install.sh

That also deploys the `worklog` skill, including `references/adoption.md`,
which step 3 below depends on. Otherwise fetch the release binary directly:

    curl -sL -o ~/.local/bin/worklog \
      https://github.com/prestontallen/ai-devboard/releases/latest/download/worklog_linux_amd64
    chmod +x ~/.local/bin/worklog

(swap `linux_amd64` for `darwin_arm64` on a Mac)

Known wrinkle: if `install.sh` reports everything current but
`worklog --version` still says an older version, it compared a
release-stamped binary against the latest tag and skipped the rebuild. Use
the `curl` path instead.

Confirm `worklog adopt --help` works before continuing.

## Step 1 — take your own backup first

`adopt --commit` takes a digest-verified snapshot before it writes anything,
and that is the supported rollback. Take an independent copy anyway — it
costs nothing and does not depend on `adopt` being correct:

    cp -r ~/.local/share/worklog  ~/worklog-backup-$(date +%Y%m%dT%H%M%S)
    cp -r ~/.local/share/devboard ~/devboard-backup-$(date +%Y%m%dT%H%M%S)

Put those OUTSIDE `~/.local/share/worklog/`. A backup inside the corpus gets
read back as corpus and breaks every later conversion.

## Step 2 — dry run

    worklog adopt

This writes nothing. It either prints a plan (counts of
create / rewrite / delete / keep / producer) or refuses.

**A refusal is the feature, not a failure.** `adopt` refuses on anything it
cannot migrate faithfully, because the alternative is changing the data
silently.

## Step 3 — adjudicate refusals

Read `references/adoption.md` in the installed `worklog` skill (usually
`~/.claude/skills/worklog/references/adoption.md`). If it isn't installed
because you took the `curl` shortcut, read
`worklog/skill/references/adoption.md` from the ai-devboard repo instead.

It has one table row per refusal class, saying what would have changed
silently and what the fix is. Work one refusal at a time, re-running
`worklog adopt` after each — fixing one often clears several.

Hard rules:

- **Never clear a refusal by deleting worklog content.** If the only way you
  can see to clear one is deleting a ticket, a note, or an archive entry,
  STOP and ask the human.
- **There is no `--force`.** A converter refusal is a corpus a human repairs.
- **Never hand-edit WORK.md, notes/, archive/ or FEEDBACK.md** to work
  around something. Use `worklog` subcommands.
- Fix the *source* of a disagreement, not whichever side is easier to edit.

Likely ones on a real corpus: a devboard `title:` that disagrees with the
ticket's title (delete the `title:` line — the ticket is the source of a
title); YAML comments in devboard files (remove them, after moving anything
worth keeping into the ticket's notes); an archive entry with no
`Completed`; a file the readers skip, like a stray `.bak` in `notes/`.

## Step 4 — show the human the plan, then commit

Do not run `--commit` without showing the human the dry-run plan first,
especially the delete count. Then:

    worklog adopt --commit

It prints a snapshot path. Keep it. `worklog adopt --rollback <that path>`
restores both directories byte-exact, at any later date, and does not depend
on any of adopt's checks having been right.

## Step 5 — prove it worked

    worklog verify          # must print: clean
    worklog note <some-existing-ticket-id> "adoption smoke test"

The write succeeding is the actual proof. `adopt` reporting success is not
the same thing: an earlier version of this command canonicalised the corpus
and left the machine with no database, and `verify` still said clean.

If anything looks wrong at any point, stop and roll back rather than
improvising.
````
