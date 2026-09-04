# Adopting a corpus that predates the store

Read this when a write says **"this machine has not adopted the store
yet"**, or when `worklog adopt` refuses.

`worklog adopt` converts the corpus into the store and rewrites the files
as projections. It refuses rather than proceeding whenever it finds
something it cannot migrate faithfully. Every refusal below is a construct
that would otherwise change *silently*, so the refusal is the feature.

## The loop

```
worklog adopt              # dry run; writes nothing
# adjudicate whatever it refuses (below), one at a time
worklog adopt              # until it prints a plan instead of a refusal
worklog adopt --commit     # after the human approves the plan
worklog verify             # must print clean
```

**The human approves the plan before `--commit`.** Show them the counts and
the deletes. `--commit` rewrites and deletes real files; a dry run does
not.

If anything goes wrong afterwards, at any later date:

```
worklog adopt --rollback <snapshot-dir>
```

The snapshot is a full copy with a sha256 manifest, taken before the first
byte was written. Restoring it is byte-exact and does not depend on any of
adopt's checks having been right.

## Hard rules

- **Never resolve a refusal by deleting worklog content.** Every fix below
  either moves information into a shape the model represents, or corrects a
  disagreement between two places. If the only way you can see to clear a
  refusal is to delete a ticket, a note, or an archive entry, stop and ask.
- **There is no `--force`.** A convert refusal in particular is a corpus a
  human repairs; nothing bypasses it.
- **Fix the source, not the symptom.** Editing a projection to match the
  store is backwards: the corpus is the input here, and adoption is what
  makes the store agree with it.
- **One at a time.** Re-run the dry run after each fix. Refusals are
  reported per file and line, and fixing one often clears several.

## Refusal classes

### census — "N file(s) no rule accounts for"

A file under the worklog or devboard root that none of the readers would
have read. It is named with its path.

- Not worklog data (a stray `.bak`, an editor swap file, notes you keep
  there by hand) → move it outside the worklog directory.
- Genuinely worklog data in a shape the readers skip — `archive/2026-08.MD`
  with a capital extension, a notes file nested a level deeper, a devboard
  file below `<repo>/_archive/` → rename it into the shape they read.

Do not delete it to make the message go away. The census exists precisely
because the readers would have skipped it without saying so.

### convert — a parser refusal

The strict converter's own refusals, carrying `file:line` where it has one:
a duplicate slug, a note whose slug names no ticket, a board file joining a
ticket that does not exist, a child naming a missing parent, an epic whose
`Active children` disagrees with its roster, or an unmodeled line inside a
section.

These are corpus repairs. Read the named line and make the corpus
consistent. Nothing bypasses them, and that is deliberate: a loud refusal
is recoverable, and the alternative is a silent drop.

### hazard — a construct the parsers drop without refusing

These are the ones worth understanding, because each would have changed
your data quietly.

| Construct | What happens | Fix |
|---|---|---|
| `workmd-preamble` | Anything before WORK.md's first `## ` section is discarded on read, and the title is re-emitted from a fixed string | Move the prose into a note, then delete it from WORK.md |
| `empty-field-value` | A field bullet with no value is not re-emitted (only `PR` is deliberately kept empty) | Give it a value, or remove the bullet |
| `duplicate-field-label` | Extra fields are a map, so the last of a repeated label wins | Keep one |
| `archive-entry-missing-completed` | The day heading is discarded on read and re-derived from `Completed`. Without one the entry renders under a bare heading that the *next* parse refuses | Add the `Completed` date |
| `archive-day-mismatch` | `Completed` disagrees with the `## YYYY-MM-DD` it sits under, so the entry silently moves day | Correct whichever is wrong |
| `feedback-unknown-field` | FEEDBACK.md's reader skips unknown `**Field**:` lines and keeps the entry. This surface has no refusal path of its own | Rename to a known field (`Trigger`, `Excerpt`, `Context`, `Resolved`) or remove the line |
| `yaml-comment`, `yaml-anchor`, `yaml-duplicate-key` | Devboard YAML is decoded to plain values; comments, anchors and duplicate keys have nowhere to live and vanish on the next render | Remove them. If a comment carries information worth keeping, move it into the ticket's notes first |
| `devboard-title-mismatch` | A board file's top-level `title:` is never read; the ticket's title replaces it on render | Delete the `title:` line — the ticket is the source of a title. If the *ticket* has the wrong title, fix it with `worklog edit <id>` |
| `devboard-duplicate-join` | Two board files claim one `worklog:` slug and merge with no duplicate check | Decide which is current and move the other out |

### stale rows

A ticket in the store with no counterpart in this corpus. Rendering would
write it back onto disk, resurrecting something that was removed. The store
has no delete operation, so adopt refuses instead of pruning.

Usually this means the store is a leftover generation. Start from a fresh
one (remove `worklog.db` from the migration data dir and re-run), which is
safe: the corpus on disk is the input, and adoption rebuilds the store from
it.

## After it succeeds

`worklog verify` must print clean. It compares whole-struct with the strict
converter on both sides, so a clean result means the store and the files
agree on every modeled field, not merely on the ones a summary view checks.

Then run a real write (`worklog note <id> "…"`) and confirm it succeeds.
That is the actual proof the machine is adopted — adoption reporting
success is not the same thing.
