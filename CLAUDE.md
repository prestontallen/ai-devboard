# Development workflow

Before starting ANY software development task (bugfix, feature, refactor,
config change, project work), invoke the **dev-context** skill first. It
classifies the task tier and governs the workflow: intake → clarify →
contract → plan → implement → verify → present. Do not write implementation
code for tier 2+ tasks until the contract is approved.

The **contract** skill is invoked from dev-context (phase 3) — don't skip it
by improvising acceptance criteria inline.

This applies to dev tasks only; conversational questions, research, and
non-code chores don't need it.
