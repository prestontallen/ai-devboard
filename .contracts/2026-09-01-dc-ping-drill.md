# Contract — Drill: `worklog ping`

- **Date:** 2026-09-01
- **Tier:** 2 Feature
- **Status:** in-progress
- **Worklog:** dc-ping-drill

## Intent

Exercise the full dev-context loop inside Cursor (intake through present and the ship gates) against a throwaway, user-visible CLI command. The command itself is not product work; it exists so every checkpoint has a real artifact to approve, verify, and review.

## Scope

**In:**
- A `worklog ping` cobra subcommand that writes `pong` plus a newline to stdout and exits 0
- Registration on the existing root command
- A cobra `Execute` test covering the happy path and extra-args refusal

**Out (explicitly not doing):**
- JSON mode, flags, workdir, or any worklog file I/O
- Docs, README, skill text, installer, or `worklog sync`
- Help-text polish beyond cobra defaults
- Mixing this drill into a commit with the in-flight installer/`wl-install-cmd` diff
- Shipping to remote unless you ask after the push gate

## Deliverables

- `worklog/internal/cli/ping.go`
- `worklog/internal/cli/ping_test.go`
- One-line register in `worklog/internal/cli/root.go`
- This contract file

## Acceptance criteria

Each criterion is observable and carries its verification. At review time this table becomes the scorecard.

| # | Criterion (given/when/then) | Verify | Status |
|---|-----------------------------|--------|--------|
| 1 | When `worklog ping` runs with no args, stdout is exactly `pong\n` and the process exits 0 | `go test ./internal/cli -run TestPing` from `worklog/` (happy-path case) | ✅ |
| 2 | When `worklog ping` is given extra args, the command errors (non-zero) and does not print `pong` as success output | same test file, extra-args case | ✅ |
| 3 | `worklog ping` appears as a subcommand of the root command | `go test` case that the root command tree includes `ping`, or `worklog ping --help` after build | ✅ |

## Definition of done (standing bar)

- [x] All existing tests pass (`go test ./...` in `worklog/`)
- [x] New behavior covered by tests
- [x] Lint/format clean (`gofmt` on touched files)
- [x] No unrelated changes in the drill commit (installer WIP stays unstaged)
- [x] User-facing behavior: cobra `--help` is enough; no README update

## Constraints & assumptions

- Complexity is **low**: no fan-out / risk scout
- The working tree is already dirty with installer work; this drill only adds the three files above
- After present, we stop at the commit gate. Keep `worklog ping` until you say discard. Do not commit.
- `--json` on `ping` is not required (assumption #1)

## Risks & open questions

- Dirty tree could leak installer files into a drill commit — mitigation: stage only ping files if we commit
- Keep vs revert — answered: keep until you say discard; do not commit

## Amendments

| Date | Change | Why | Approved |
|------|--------|-----|----------|
| 2026-09-01 | Keep ping until discard; never commit this drill | Human at contract approval | Preston |
