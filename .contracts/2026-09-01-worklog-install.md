# Contract — worklog install: Go installer + release-first bootstrap

- **Date:** 2026-09-01
- **Tier:** 3 (Major — Go subcommand + release pipeline + bootstrap rewrite)
- **Complexity:** medium
- **Status:** in-progress (agreed 2026-09-01)
- **Worklog:** wl-install-cmd

## Intent

The bash installer's logic has outgrown bash (three structural bugs in one
day: set -e traps, the loop-redirect prompt bug, untestable interactive
paths). Move the logic into a `worklog install` subcommand — huh
multi-select target picker, styled output, unit-testable deploy/config
code — and shrink install.sh to a bootstrap that obtains the binary
(latest GitHub release for the platform, falling back to local `go build`)
and execs it. Go stops being a hard requirement for users; the script
stays the one-command entrypoint.

## Scope

**In (by milestone):**

- **M1 — `worklog install` subcommand** (`internal/installer` +
  `internal/cli/install.go`): target config read/write, detection, huh
  TUI (TTY-guarded), skill deployment, `--check`/`--dry-run`, symlink
  migration, tone check, devboard dir, opt-in prompts, self-rebuild;
  `worklog sync` resolved per Q2; install.sh keeps building locally but
  delegates everything after the build to `worklog install`; **module
  rename**: `github.com/prestontallen/day2day` →
  `github.com/prestontallen/ai-devboard/worklog` (go.mod + all imports),
  severing the last code-level tie to the old repo
- **M2 — release pipeline**: GoReleaser config updated for ai-devboard
  (predictable asset names `worklog_<os>_<arch>`, checksums.txt), GitHub
  Actions workflow on tag push, first tag cut (human-gated: tag push and
  any workflow file are outward-facing)
- **M3 — release-first bootstrap**: install.sh downloads the latest
  release asset for the platform, verifies its sha256 against
  checksums.txt, falls back to `go build` when download fails, errors
  clearly when neither path works; drift becomes mode-aware
  (release-installed → compare to latest tag; dev-built → rev compare)

**Out:**
- Windows-native anything (WSL pointer unchanged)
- Auto-update daemon / self-update outside install runs
- Homebrew/AUR/package-manager distribution
- Deleting `worklog/scripts/sync.sh` (per Q2 resolution it stays or
  delegates, but is not removed this contract)
- Changing what gets deployed (same four skills + claude command file)

## Deliverables

- `worklog/internal/installer/` (+ tests), `worklog/internal/cli/install.go`
- Rewritten `install.sh` (bootstrap), `.goreleaser.yaml` update,
  `.github/workflows/release.yml`
- README + worklog/README corrections (incl. the false "install.sh runs
  sync.sh" claim)

## Acceptance criteria

### M1 — subcommand core

| # | Criterion | Verify | Status |
|---|-----------|--------|--------|
| 1 | `worklog install` with a TTY and no config shows a huh multi-select of detected agent dirs (pre-checked) + custom-path input; selection persists (targets + repo root) and deploys | manual TTY run | ☐ |
| 2 | **TTY guard is ours, not huh's**: with piped stdin the TUI never appears (huh would silently grab /dev/tty otherwise — verified library behavior); fallback order is saved config → detection | pipe test |✅ |
| 3 | **Zero targets is an error**: an empty resolved target set (incl. huh accessible-mode EOF under TERM=dumb returning zero selections) exits non-zero with a message — never a silent success | TERM=dumb + EOF test |✅ |
| 4 | Existing plain-path `targets` files parse unchanged (comments, blanks, literal `~` lines); richer config is additive with auto-migration, never a parse error | fixture test with pre-move file |✅ |
| 5 | **Source verification precedes any destructive step**: every skill source must exist and be readable before the first rm/copy; a missing source aborts with nothing deleted | test with renamed source |✅ |
| 6 | Repo root persists in config; `worklog install --check` from outside the repo works, and a moved/deleted checkout yields a distinct "repo not found at <path>; re-run install.sh from a checkout" error — not drift, not a deploy | move-repo test |✅ |
| 7 | Deploy parity: copies with diff idempotency, legacy symlink→copy migration (with check/dry-run wording), claude-only command.md extra, all four skills incl. fan-out | Go tests + sandbox diff |✅ |
| 8 | Mode parity: `--check` exits 1 on drift/0 clean and writes nothing (targets config included); `--dry-run` narrates and touches nothing; opt-in prompts only in interactive install mode | sandbox matrix |✅ |
| 9 | Small-behavior parity, one check each: PATH warning; tone-skill glob (still `~/.claude/skills` scope); "targets: from config (...)" note on silent runs; `INSTALL_PROMPT_FORCE` seam (or documented replacement) | tests/greps | ☐ |
| 10 | Streams: TUI on stderr (huh default), report lines on stdout via `internal/style`; `--check` output grep-stable | code review + pipe test |✅ |
| 11 | Version plumbing: raw `commit`/`version` exposed as accessors (not just the joined string); self-rebuild uses the same rev algorithm as bash (`-- worklog` scope, `-dirty`, UTC date) so the strings match byte-for-byte | unit test both sides | ☐ |
| 12 | Self-rebuild handoff per Q1's answer, tested (a process cannot swap its own executable and continue) | integration test | ☐ |
| 13 | `worklog sync` never writes to a target the user declined (per Q2 resolution); worklog/README's false claim fixed | test + grep |✅ |
| 14 | All 19 Go packages stay green; new installer package tested | go test ./... |✅ |
| 14b | Module renamed to `github.com/prestontallen/ai-devboard/worklog`: zero `day2day` references remain in Go source; build, tests, and install.sh all green after | grep + go test + install run |✅ |

### M2 — release pipeline

| # | Criterion | Verify | Status |
|---|-----------|--------|--------|
| 15 | GoReleaser builds `worklog_<os>_<arch>` assets + checksums.txt for linux/darwin × amd64/arm64 from a tag | `goreleaser release --snapshot` locally | ☐ |
| 16 | Actions workflow runs GoReleaser on tag push only; workflow file and first tag are human-approved before push | review + gated push | ☐ |
| 17 | The published release's `worklog install --check` runs on this machine (release binary is functional, contains the subcommand) | download + run | ☐ |

### M3 — release-first bootstrap

| # | Criterion | Verify | Status |
|---|-----------|--------|--------|
| 18 | **Bash still owns binary drift**: `./install.sh --check` on a fresh clone with no binary exits 1 reporting the absent/stale binary and touches nothing (no build, no download) | fresh-clone sandbox | ☐ |
| 19 | Default mode: download latest release asset for the platform, verify sha256 against checksums.txt (mismatch = hard fail, file discarded), place binary, exec `worklog install --repo <root>` | sandbox with real release | ☐ |
| 20 | Download unavailable (offline/no release) → falls back to `go build` with a note; neither available → clear error naming both remedies; exit before any file change | PATH/network-stripped runs | ☐ |
| 21 | Mode-aware drift: release-installed binary checks against latest tag, dev-built against repo rev; skew between release binary and newer checkout skills is surfaced, not silently ignored | scenario tests | ☐ |

## Definition of done

- [ ] No new Go deps (huh/lipgloss/cobra already present)
- [ ] install.sh under ~80 lines; all logic that CAN live in Go does
- [ ] No unrelated changes; other agent's in-flight files untouched
- [ ] READMEs updated (install flow, config format, sync clarification)

## Risks & open questions

- **Q1:** self-rebuild handoff — proposed: rebuild, then `syscall.Exec`
  the fresh binary with the same args (clean continuation); fallback on
  exec failure: print "rebuilt; re-run". Alternative: always exit with
  "stale, re-run" (simpler, worse UX).
- **Q2:** `worklog sync` fate — proposed: it delegates to the
  targets-aware installer deploy (respecting declined targets) and its
  fixed-pair behavior is retired; sync.sh script stays for bootstrap-less
  worklog-only use. Alternative: document both as separate tools (drift
  risk stays).
- **Q3:** first release version — proposed `v0.3.0` (v0.1.0 was day2day;
  0.2.0-dev is the current stamp).
- Risk: huh MultiSelect/Confirm are new to this module (existing forms
  are Input/Text/Select only) — UX verified manually at M1 checkpoint
- Risk: Actions workflow + tag are outward-facing — both pass through
  the push gate explicitly
- Sequencing: M3 cannot flip until M2's release exists; between M1 and
  M3 the bootstrap builds locally (today's behavior, no regression window)
- **Q4:** archive the day2day GitHub repo once M2's first ai-devboard
  release is live (outward-facing; human executes or approves `gh repo
  archive`) — proposed: yes, immediately after criterion 17 passes
- Note: module-in-subdir path means `go install ...@version` won't
  resolve without subdir-prefixed tags — accepted; releases + install.sh
  are the distribution story

## Milestones

1. **M1 subcommand** · checkpoint: criteria 1-14 transcript + code review
2. **M2 release pipeline** · checkpoint: workflow file review + explicit
   approval to push the workflow and tag
3. **M3 bootstrap flip** · checkpoint: criteria 18-21 transcript; contract
   closes

## Amendments

| Date | Change | Why | Approved |
|------|--------|-----|----------|
