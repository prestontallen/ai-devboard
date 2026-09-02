# Research sweep

Fan-out instance for the research phase and the spike track ([fan-out](../SKILL.md)
axes: led seeding · lens-diverse · union + dedup · gates the synthesis
document). Distilled from a real run (adb-research-mode, 2026-09-02) — every
rule below traces to a moment in that session, not to theory.

Research is not "go read things". It is a choreography that ends in one
pre-vetted document and a short list of decisions only the human can make.

## The choreography

1. **Orient from persistent state.** Prior notes, decisions, and archives
   are findings. Re-deriving a fact already recorded is wasted scope — a
   scout will flag it as such.
2. **Facts before questions.** Enumerate every surface the question
   touches; read them directly. Every fact carries evidence (`file:line`,
   command output). The output is a current-state map a stranger could
   verify claim by claim.
3. **Persist the map before involving the human** (`worklog note <id>`).
   Findings that live only in conversation die with the session.
4. **Split facts from decisions.** A question the repo can answer is not a
   question for the human. What remains — product intent, tradeoffs,
   taste — goes to the human **batched, once**, each question with
   concrete options, the consequence of each option, and exactly one
   recommendation. An option list without a recommendation offloads
   judgment; an unbatched dribble burns attention.
5. **Record each answer as a decision with its why, immediately**
   (`worklog task decision`), before building on it.
6. **Draft the synthesis while verification runs.** Spawn the scout sweep
   (lens-diverse, led — see [risk-scout.md](risk-scout.md) for the
   contract-phase lens set), then keep working: skeleton the document,
   fill what's already firm. Never idle-block on subagents.
7. **Fold, don't relay.** Every scout finding lands in the one synthesis
   document as exactly one of: an out-scope line, a criterion, a risk
   entry, or an open question. The human reviews one pre-vetted document,
   never a document plus a report pile.
8. **When evidence falsifies a premise, the premise loses.** Fix the
   artifact, then persist the correction somewhere durable (memory, note)
   so no future session pays for it again. Do not defend, do not quietly
   patch without recording.
9. **Present judgment calls as veto-able.** Decisions you made from
   findings are listed explicitly at the checkpoint so the human can
   reverse any of them cheaply. A silently-resolved call is scope creep
   in decision space.

## Judgment rules (the part that doesn't come free)

- **Fact-vs-preference triage** — before asking anything, ask: could a
  grep answer this? If yes, it's step 2 work, not a question.
- **Evidence discipline** — a claim without `file:line` doesn't enter the
  record. This is what lets scouts falsify your premises instead of
  arguing with them.
- **Honest complexity rating** — reading two files of a ten-file flow is
  not knowing the flow (the familiarity trap, see SKILL.md). Rate
  complexity by what you haven't read.
- **One-document rule** — findings are edits, not attachments.
- **Recommendation-always** — every option set carries your pick and why.

## Rubric

Each rule is observable, so a judge (or you, before presenting) can score
a research run:

- **R1** — zero questions to the human that the repo could answer
- **R2** — questions batched; each has options + exactly one recommendation
- **R3** — every factual claim cites `file:line` or a command
- **R4** — findings persisted (note/decision) before the human checkpoint
- **R5** — every scout finding lands in the document exactly once
- **R6** — falsified premises corrected in the artifact and persisted
- **R7** — work product advanced while subagents ran

A research run that fails R1, R5, or R6 isn't done — fix before presenting.
