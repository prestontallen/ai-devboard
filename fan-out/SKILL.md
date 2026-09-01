---
name: fan-out
description: >-
  Reusable patterns for spawning multiple subagents on one problem —
  scouting risks, exploring unfamiliar systems, generating alternative
  solutions, adversarially verifying claims. Use whenever a workflow phase
  or task calls for parallel subagents, and BEFORE improvising a multi-agent
  approach: it defines the four design axes (seeding, diversity,
  aggregation, gates), the complexity throttle, and when NOT to fan out.
---

# Fan-out: many agents, one problem

Spawning parallel subagents is a power tool with a multiplier on cost and
latency. Every use is a combination of four choices — make each one
deliberately.

## The four axes

### 1. Seeding — led vs blind

- **Led**: the agent gets your context — draft scope, hypothesis, prior
  findings. Fast, focused, confirmatory. Inherits your framing: it finds
  the problems you'd think to look near.
- **Blind**: the agent gets the goal and nothing else. Finds what your
  framing excluded — the alternative approach, the assumption you didn't
  know you'd made. Costs more, wanders more.

Rules:
- **Blindness applies to the hypothesis, never the deliverable.** Blind
  agents still get a strict output contract (see axis 4's format rule) —
  otherwise aggregation turns to mush.
- **Blindness is fragile.** One leaked sentence ("we're planning to add
  retries") contaminates the panel. Write an explicit withhold-list before
  spawning: your preferred approach, the draft plan, prior attempts,
  earlier agents' findings. Always include: the goal, the constraints that
  are truly fixed, and the output format.
- Mixed panels (some led, some blind) hedge when you can afford them.

### 2. Diversity — redundant vs lens-diverse

- **Redundant** (same prompt × N): for *verification*. Independent agents
  reaching the same verdict is signal; disagreement is the finding.
- **Lens-diverse** (each agent one distinct concern): for *discovery*.
  Coverage beats confirmation; never give two scouts the same lens, and
  never give one scout a grab-bag "look for problems" prompt.

### 3. Aggregation

- **Union + dedup** — discovery (scouts, exploration). Merge, drop
  duplicates, triage.
- **Vote** — verification. Majority (or stricter) kills or confirms a
  claim; prompt refuters to *refute*, defaulting to refuted when unsure.
- **Judge + synthesize** — alternatives. Score the N independent
  solutions, take the winner, graft the best ideas from runners-up.

### 4. Gates — what findings do

Decide before spawning: findings **inform** a document, **gate** a
checkpoint (must be resolved or explicitly accepted by the human), or
**block** outright. Every agent returns structured output — at minimum
`{finding, evidence (file:line or command), severity, suggested action}` —
so triage is mechanical, not interpretive.

## The complexity throttle

Fan-out depth is gated by the task's **complexity rating** (low / medium /
high — assessed at intake alongside the tier, see dev-context). Complexity
measures uncertainty and blast radius, not size:

- **low** — no fan-out. One agent (or one grep) answers it.
- **medium** — 2 agents, the two highest-value lenses for the scenario.
- **high** — 3-4 agents, full lens set; blind seeding becomes worth its
  cost here.

Beware the familiarity trap: having read two files in a flow that spans
ten feels like knowing the codebase, and "I already investigated this
myself" is the most tempting reason to skip scouts. Empirically (the
epic-archive contract, 2026-09-01) scouts at medium found blockers a
confident inline read missed. If a task truly needs no scouts, that is
what a **low** rating is for — rate honestly instead of skipping.

Hard cap: 4 parallel agents per fan-out unless the human raises it.

## When NOT to fan out

- The question is answerable by one search or one file read.
- Complexity is low, whatever the tier — a large mechanical change is
  still low-complexity.
- You'd be fanning out to avoid deciding — agents inform judgment, they
  don't replace it.
- The same ground was already swept this session; re-scouting the same
  target with the same lenses buys nothing.

## Scenarios

Proven instances live in references/ — read the one that fits before
spawning:

- **Risk scout** (contract phase) → [references/risk-scout.md](references/risk-scout.md)

Patterns worth reaching for that have no reference yet (write one after
first real use): blind alternatives panel at plan time for high-complexity
work (N independent designs, judge + synthesize); adversarial verification
of acceptance criteria at verify time (redundant refuters, vote); blind
exploration at clarify time for unfamiliar subsystems (lens-diverse,
union).
