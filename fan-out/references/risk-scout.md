# Risk scout

Fan-out instance for the contract phase ([fan-out](../SKILL.md) axes:
led seeding · lens-diverse · union + dedup · gates contract approval).

Runs during contract generation, after the draft scope exists and BEFORE
the human sees the contract — the document that reaches them is pre-vetted,
findings already folded in. Skip when complexity is low or tier is 0.

## Lenses

Each scout is a read-only agent given the draft scope (led) plus exactly
one lens:

1. **Blockers** — missing deps, environment constraints, auth/permissions,
   rate limits, anything that stops the work mid-flight.
2. **Side effects** — shared state, config, migrations, concurrency, and
   shared namespaces (labels, ids, index positions); what else changes when
   this changes, including cross-ticket seams another ticket owns.
3. **Downstream consumers** — who imports/calls the touched surface;
   blast radius; which tests and callers break.
4. **Prior art & coupling** — past attempts, TODOs, workarounds this
   change invalidates, adjacent tickets.

Scaling by complexity: medium → lenses 1 + 3 · high → all four.

## Prompt shape

Give each scout: the draft intent + in/out scope, the repo location, its
single lens, and the output contract below. Withhold: other scouts'
findings and any tentative implementation plan (scope is shared;
solution bias is not).

Output contract — each scout returns only a list of:

```
finding:    one sentence
evidence:   file:line, command output, or doc reference
severity:   blocker | risk | note
suggested:  the contract change this implies (out-scope line, criterion,
            risk entry, or open question)
```

## Folding findings into the contract

Union the lists, dedup, then every surviving finding lands as exactly one
of:

- an **Out** scope addition (the adjacency it exposed)
- a new **acceptance criterion** (usually a sad path)
- a **risk + mitigation** entry
- an **open question** for contract review

Then the gate: any `severity: blocker` finding must be resolved — answered,
scoped out, or explicitly accepted by the human — before presenting the
contract for approval. Name the scout findings in the contract's risks
section so the human can see what the sweep surfaced.

Every `severity: risk` finding is dispositioned too — mitigated-by-criterion,
accepted-by-human, or scoped-out — before the contract is presented. Naming a
risk is not discharging it.
