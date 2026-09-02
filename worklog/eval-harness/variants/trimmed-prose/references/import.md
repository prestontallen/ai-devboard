# Importing tickets from a tracker

Read this when the user references a tracker ticket — a URL, an issue key, a
pasted body, or a screenshot — and it should become a worklog block.

`worklog import` accepts ticket JSON on stdin or via `--file`. The CLI is
deliberately generic: all tracker-specific mapping happens here, then gets
piped to the binary.

## JSON shape

    {
      "id": "auth-1234",
      "title": "Refactor auth middleware",
      "type": "ticket",
      "parent": "epic-auth",
      "repo": "api",
      "tags": ["auth", "refactor"],
      "pr": "#42",
      "section": "next",
      "source": "https://company.atlassian.net/browse/AUTH-1234"
    }

A single object or an array is accepted. Required: `id`, `title`. Defaults:
`type=ticket`, `section=next`. `parent` is optional; when set, the epic must
already exist in WORK.md — imports never auto-create parents.

## Workflow

    echo '{"id":"auth-1234","title":"Refactor auth","section":"next","source":"https://..."}' \
      | worklog import --json

Batches:

    cat <<'JSON' | worklog import --json
    [
      {"id":"foo-1","title":"One","parent":"epic-x"},
      {"id":"foo-2","title":"Two"}
    ]
    JSON

## Tracker mapping

| Tracker | `id` | `title` | `parent` | `pr` / `source` |
|---|---|---|---|---|
| Jira | `key` lowercased (auth-1234) | `summary` | parent issue `key`, lowercased | linked PR URL / Jira issue URL |
| Linear | `identifier` (lin-567) | `title` | parent issue `identifier` | linked PR URL / Linear issue URL |
| GitHub Issues | repo-prefixed (e.g. api-42) | `title` | (no native parent) | linked PR URL / Issue URL |
| Asana | task short-name or GID slug | `name` | parent task short-name | linked PR URL / Asana task URL |

Always omit upstream status fields — the section is the user's choice, not
the tracker's. When unsure where a ticket goes, default to `next` and ask.

## When the parent epic is missing

Ask whether to import standalone (dropping the parent link) or to pause so
the user can `worklog add --type epic` first. Do not silently strip the
parent.
