# Contract — Devboard UI redesign: glanceable, not a wall of text

- **Date:** 2026-09-01
- **Tier:** 2 Feature
- **Status:** fulfilled
- **Worklog:** csk-devboard-ui

## Intent

The devboard exists so a human can open a browser and understand, in seconds,
what agents are working on, how far along each task is, and what needs their
attention — without reading terminal text. The current UI renders every task
as uniform text lists (plan bullets, scorecard bullets, decision bullets),
so state and progress are buried in prose of equal visual weight.

Redesign the renderer so the board *informs, prompts action, and confirms
progress* visually: attention ("needs you") dominates, phase and progress are
drawn (stepper, bars, pips) rather than written, and long text (notes, code,
detail blobs) sits behind progressive disclosure. Data, schema, and server
stay exactly as they are — this is a renderer-only change.

## Scope

**In:**
- Rewrite of `devboard/static/index.html` (markup, CSS, JS) — the only file
  changed.
- Overview: aggregate header strip (in-flight / needs-you / stale counts),
  attention-first "Needs you" panel, task cards with phase stepper, plan
  progress bar, scorecard pips, freshness indicator; repo as chip on the card.
- Detail view: phase-stepper hero, visual plan/scorecard, decisions timeline,
  collapsible notes and code sections (collapsed by default).

**Out (explicitly not doing):**
- No `server.py`, `schema.md`, Dockerfile, or compose changes. The server
  serves only `/index.html` (404s all other paths — scout-verified), so no
  second static asset; the page stays one self-contained file.
- No new fields, no phase timestamps: "time in phase" / velocity is **not
  buildable** from the current payload (only file `mtime` exists) and is out
  of scope. Effort is conveyed via plan-completion ratio, scorecard ratio,
  started/decision dates when present, and mtime freshness.
- No worklog CLI changes; no frameworks, build steps, or CDN assets.
- No epic/milestone hierarchy rendering — an epic's task file renders as a
  single task with a flat plan, same as today.
- No automated UI test harness (none exists; manual verification is the bar).
- No auth, no light theme, no mobile-first work (responsive enough not to
  break, but desktop is the target).

## Deliverables

- Rewritten `devboard/static/index.html`.
- Approved visual mockup (scratch artifact, referenced from worklog notes).
- This contract, updated to fulfilled.

## Acceptance criteria

| # | Criterion (given/when/then) | Verify | Status |
|---|-----------------------------|--------|--------|
| 1 | Given the example repos + live data, the overview shows in one screen: header counts (in-flight, needs-you, stale), a "Needs you" panel first when any `needs_you` exists, and per-task cards each drawing phase position, plan progress (n/m), scorecard pips, and freshness — no scrolling needed to know what needs the human | manual browser check vs examples + live data | ☐ |
| 2 | When no task has `needs_you` items, the panel is absent and a calm empty state shows; when items exist, each links (`#repo/id`) to its task | fixtures with and without `needs_you` | ☐ |
| 3 | The phase stepper hardcodes the CLI enum (intake→…→done); an unknown or missing `phase` renders a neutral state without breaking layout | hand YAML with `phase: x` and no phase | ☐ |
| 4 | No field regression: everything the current UI renders still appears — grid: title, phase, tier, complexity, worklog badge, needs-you count, resume button, ago, >2h stale dimming, parse-error card; detail: repo, branch, phase, tier, complexity, worklog, ago, resume, needs_you{type,text,detail}, plan{text,state}, scorecard{text,verify,status}, decisions{what,why,when}, code{file,lines,note,snippet}, links{label,url}, worklog notes (markdown+highlighting), unknown fields in "Other" (incl. examples' `custom_field`) | render both `devboard/examples/` files + a bare minimal YAML; tick each field | ☐ |
| 5 | Notes and code snippets are collapsed by default in detail; expanding one, then touching a task file (SSE re-render), preserves expanded state and scroll position | expand, `touch` a task file, observe | ☐ |
| 6 | A task file containing `<script>` in text fields and a `javascript:` URL in links renders inert (all text escaped; unsafe link schemes neutralized) | malicious fixture file | ☐ |
| 7 | Sad-path tolerance: scalar list items (`plan: ["bare string"]`), non-ISO `decisions[].when` (timeline falls back to file order), parse-error entries without `mtime` — all render without crash | fixture file | ☐ |
| 8 | Deep links `#<repo>/<task-id>` open the detail view; copy/resume buttons work in non-secure contexts (execCommand fallback kept) | open hash URL directly; test copy over LAN-style origin | ☐ |
| 9 | The page makes no external requests (only `/`, `/api/tasks`, `/events`) and works served by bare `python3 server.py` | browser network tab | ☐ |

## Definition of done (standing bar)

- [ ] No unrelated changes in the diff
- [ ] `worklog` Go tests still pass (`go test ./...` — untouched, sanity only)
- [ ] Docs stay true: schema.md / README rendering promises (Other section,
      resume button, worklog badge, error card, stale dimming) all still hold
- [ ] Final verification against the rebuilt container
      (`docker compose up --build -d`), not just the bare server

## Constraints & assumptions

- Single self-contained HTML file — enforced by the server, not preference.
- Iteration happens on a spare port via bare `server.py` (8484 is held by the
  running container); container rebuilt once at the end.
- Freshness keys off file `mtime` only; the dead `updated`/`updated_ts` paths
  in the current code are dropped (no producer writes them — scout-verified).
- `code[].lang` (documented, CLI-written, currently unrendered) will be
  surfaced as a small label on code blocks — cheap and already in the data.
- Dark theme retained and refined; palette stays in the current family.

## Risks & open questions

Scout sweep (blockers + downstream lenses) surfaced and folded in:

- **Risk:** full innerHTML re-render on every SSE tick wipes transient UI
  state → mitigated by criterion 5 (state kept outside the re-rendered DOM).
- **Risk:** the renderer is the only XSS boundary for agent-authored YAML →
  criterion 6.
- **Risk:** docs promise specific behaviors while schema.md is frozen →
  criterion 4 + DoD docs item.
- ~~Open question: dirty tree~~ **Resolved 2026-09-01:** human said commit;
  the complexity-badge work landed in `a469da8` before implementation began —
  clean baseline confirmed.
- ~~Open question: effort scope~~ **Resolved 2026-09-01:** effort stays
  ratios + mtime freshness; follow-up ticket `csk-devboard-timestamps` filed
  for schema phase-timestamps.
- **Design decision for review:** attention-first card grid, *not* kanban
  phase columns — with ≤5 tasks in flight a 9-column board is mostly empty
  space; the stepper on each card carries the same information densely.

## Amendments

| Date | Change | Why | Approved |
|------|--------|-----|----------|
| 2026-09-01 | Status draft → agreed; both open questions resolved (commit landed as a469da8; effort scoped down + follow-up ticket csk-devboard-timestamps) | Contract review | Preston |
