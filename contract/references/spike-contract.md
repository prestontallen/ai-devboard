# Spike contract

A spike's deliverable is an answer, so its contract is scoped by a
**question**, not a change. Tier-1 weight: inline, human nods before
research starts.

```
**Spike contract — ⟨title⟩**
- Question: ⟨the one thing this research answers⟩
- Answered when: ⟨finding the research must produce⟩ (verify: ⟨what would
  ground it — file:line, command output, doc reference⟩)
- Answered when: ⟨finding⟩ (verify: ⟨how⟩)
- Not answering: ⟨adjacent question deliberately left alone⟩
- Plus standing spike DoD: findings persisted to `worklog note`, forks
  recorded via `worklog task decision`, recommendation presented, follow-up
  tickets proposed.
```

Rules that differ from a change contract:

- **The question is singular.** Two questions is two spikes; a brief that
  fans across unrelated unknowns produces a report nobody acts on.
- **Criteria describe knowledge, not behavior** — "we know whether X can be
  done without Y, with evidence" beats "investigate X".
- **Verification is evidence, not tests.** Every answered-when names what
  would ground the finding.
- **"Not answering" carries the weight** the Out scope does elsewhere; it
  is where a spike's sprawl dies.
- **No implementation criteria.** If a criterion describes shipped code,
  this isn't a spike — reclassify.

At present time this becomes the scorecard the same way a change
contract's criteria do: each answered-when gets ✅/❌ plus the evidence
that settled it.
