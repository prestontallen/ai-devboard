---
name: contract
description: >-
  Generate a work contract from any arbitrary dev task — the single document
  that encapsulates everything required to call the work "done". Use when
  starting a task (via dev-context phase 3), when the human asks "what would
  done look like", or when scoping ambiguous work. Scales from a one-line
  done-when to a milestone-structured major contract.
---

# Contract: what "done" means, written down first

A contract is the agreement between human and agent about what will exist
when the work is finished. It answers three questions no code review can:
*what are we building, what are we explicitly not building, and how will we
prove it's done?*

Two layers, kept distinct:

- **Acceptance criteria** — task-specific. "Did we build the right thing?"
- **Definition of done** — the standing project bar. "Is it finished to our
  standard?" Same list every task; not renegotiated per task.

## Writing acceptance criteria

This is the load-bearing section. Rules:

1. **Observable from outside.** "Handles errors gracefully" is not a
   criterion. "When the API returns 500, the CLI prints a retry hint and
   exits 1" is — you can run it and watch.
2. **Every criterion carries its verification.** A command to run, a test
   name, or a concrete manual check. If you can't say how it'll be verified,
   the criterion isn't done being written.
3. **Behavioral form.** Prefer "when/given X, then Y". For tricky
   requirements, EARS phrasing helps: "While ⟨state⟩, when ⟨trigger⟩, the
   ⟨system⟩ shall ⟨response⟩."
4. **Cover the sad paths.** Error handling, empty inputs, and boundaries are
   criteria, not afterthoughts.
5. **Few and sharp beats many and mushy.** If a criterion doesn't change
   what you'd build or test, cut it.

## Contract by tier

Tier comes from dev-context. If working standalone, classify the same way
(trivial / small / feature / major).

### Tier 0 — Trivial
One sentence, inline: *"Done when: ⟨observable result⟩, verified by ⟨check⟩."*

### Tier 1 — Small (inline mini-contract)

```
**Contract — ⟨title⟩**
- Done when: ⟨criterion⟩ (verify: ⟨how⟩)
- Done when: ⟨criterion⟩ (verify: ⟨how⟩)
- Not doing: ⟨explicit exclusion⟩
- Plus standing DoD: tests pass, lint clean, no unrelated changes.
```

### Tier 2 — Feature (full contract, written to file)

Write to `.contracts/⟨yyyy-mm-dd⟩-⟨slug⟩.md` in the project (create the
directory; committing it is the human's call). Use the template in
[references/contract-template.md](references/contract-template.md).
Sections: Intent · Scope (In/Out) · Deliverables · Acceptance criteria ·
Definition of done · Constraints & assumptions · Risks & open questions ·
Amendments log.

### Tier 3 — Major
Full contract plus a **Milestones** section. Each milestone is independently
completable and gets its own mini-contract (tier-1 form) when its turn
comes. The top-level contract's criteria are the union that matters; the
milestones sequence the work and the human checkpoints.

## Generating a contract from an arbitrary task

1. **Extract intent.** What outcome does the human actually want, and why?
   The why constrains the what — write both down.
2. **Investigate before drafting.** Read the relevant code. Half the scope
   questions ("does this touch the API too?") are answerable from the repo.
3. **Draft scope aggressively.** The **Out** list is worth more than the
   **In** list — it's where scope creep dies. Put every plausible-but-not-
   requested adjacency there explicitly.
4. **Write criteria per the rules above**, sad paths included.
5. **Attach the standing DoD.** Pull from the project's conventions
   (CLAUDE.md, CI config) if defined; otherwise the default: all tests pass,
   new behavior has tests, lint/format clean, no unrelated diffs, docs
   updated where behavior is user-facing.
6. **Run the risk scout** (tier 1+, complexity medium/high): fan out
   read-only subagents over the draft scope per the fan-out skill's
   [risk-scout reference](../fan-out/references/risk-scout.md), at the
   throttle's depth for the rated complexity. Fold every finding into the
   contract (out-scope line, sad-path criterion, risk, or open question)
   BEFORE presenting it; a blocker-severity finding must be resolved or
   explicitly accepted by the human first. The human reviews one
   pre-vetted document, never a contract plus a separate report.
7. **Surface open questions** instead of silently resolving them — each one
   is either answered at contract review or logged as an assumption.
8. **Present for approval** (per dev-context). The contract isn't real
   until the human says so.

## Contract lifecycle

`draft → agreed → in-progress → fulfilled` (or `abandoned`). Status lives in
the contract header. Changes after "agreed" go through the amendments log —
one line each: date, what changed, why, who approved. At verification time
the contract becomes the scorecard: every criterion gets ✅/❌ plus evidence.

## Devboard integration

When the contract reaches **agreed**, mirror it into the devboard task
file via the worklog CLI (never hand-edit the YAML; the commands no-op
when devboard isn't set up):

- each acceptance criterion: `worklog task scorecard add "<criterion>"
  --verify "<check>" --id <slug>`
- each open question for the human: `worklog task needs-you add
  "<question>" --type question --id <slug>`

From then on the task file's scorecard tracks verification live —
`worklog task scorecard pass|fail <n>` as checks run (dev-context phase 6).

## Worklog integration

If the task has (or warrants) an item in Preston's worklog, mirror the
one-line acceptance summary into the item's **Acceptance** field and link
the contract file from **Notes**:

    worklog edit <id> --acceptance "<one-line summary>"
    worklog note <id> "Contract: .contracts/<yyyy-mm-dd>-<slug>.md"

`worklog edit` works on a ticket in any section and inserts either field if
the block doesn't carry it yet. Never hand-edit `WORK.md` to do this.
