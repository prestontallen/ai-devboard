# Development workflow

Before starting ANY software development task (bugfix, feature, refactor,
research spike, config change, project work), invoke the **dev-context**
skill first. It classifies the task tier and governs the workflow: intake →
clarify → research (optional) → contract → plan → implement → verify →
present. Investigation-first work takes the collapsed spike track: intake →
research → present → done. Do not write implementation code for tier 2+
tasks until the contract is approved.

The **contract** skill is invoked from dev-context (phase 4) — don't skip it
by improvising acceptance criteria inline.

This applies to dev tasks only; conversational questions and non-code chores
don't need it — but "research X" is a spike, which does.
