---
name: ed-edx
description: Primary Edge Delta CLI - edx commands, authentication, output formats and conventions.
metadata:
  version: "1.0.0"
  author: edgedelta
  repository: https://github.com/edgedelta/agent-skills
  tags: edgedelta,cli,edx,auth,setup
  alwaysApply: "false"
---

# Edge Delta CLI (edx)

`edx` is the canonical way for agents to interact with Edge Delta: Pipeline
(fleet management, configs, live capture), Observability (logs, patterns,
metrics, traces, events, monitors) and AI Teammate (connectors, activity,
knowledge graph, workflows).

## Install

```bash
brew install edgedelta/tap/edx       # macOS/Linux
# or
go install github.com/edgedelta/edx@latest
edx version                          # verify
```

## Auth

New to Edge Delta? `edx signup` creates the account and logs in. Already have one?
`edx auth login`. Signup and OAuth login are interactive, so the user runs them.

```bash
# New account: confirms your email, emails a sign-in link, then confirm the code shown:
edx signup                                             # --profile <name> --env staging for staging

# OAuth (default; interactive browser login, org read from the token, auto-refreshed):
edx auth login                                         # --device when there is no local browser

# Token auth (good for CI/automation):
edx auth login --token <api-token> --org-id <org-id>   # saved to ~/.config/edx/config.yaml

edx auth status                                        # verifies credentials against the API
```

With no saved profile, `edx auth status` prints "No profiles yet"; offer `edx signup`
for a new user and `edx auth login` for an existing one.

Environment variables override the config file: `ED_API_TOKEN`, `ED_ORG_ID`,
`ED_ENV` (`prod` (default), `staging` or `local` — selects the API and AI
service hosts together). Multiple orgs/envs: `edx auth login --profile <name> ...`
then `edx --profile <name> ...`.

If commands fail with 401, the credentials are invalid or do not match the org.
Re-run `edx auth login` (OAuth by default, or `--token` from Admin > API Tokens).

## Command Map

| Domain | Commands |
|--------|----------|
| Logs | `edx logs search`, `edx logs graph` |
| Audit trail | `edx logs search/graph --scope audit` |
| Patterns | `edx patterns list`, `edx patterns samples` |
| Metrics | `edx metrics list`, `edx metrics query` |
| Traces | `edx traces search`, `edx service-map` |
| Events | `edx events search` |
| Monitors | `edx monitors list/get/create/update/delete/states` |
| Pipelines | `edx pipelines list/get/create/delete/history/save/deploy/validate/agents/status` |
| Rehydrations | `edx rehydrations list/get/validate/analyze/create/cancel/delete` |
| Fleet | `edx fleet agents`, `edx fleet deployments` |
| Live capture | `edx capture start/task/status/results` |
| Health | `edx health components`, `edx health problems` |
| Dashboards | `edx dashboards list/get/create/update/delete/validate/screenshot/tag` |
| Schema | `edx facets keys/options/list` |
| Query syntax | `edx cql validate --type <type> '<query>'` (offline) |
| Lookup tables | `edx lookup list/get/download/create/update/delete` |
| AI Teammate | `edx ai connectors ...`, `edx ai activity` |
| AI workflows | `edx ai workflows list/get/run`, `edx ai workflows runs list/get/steps` |
| Knowledge graph | `edx ai knowledge stats/topology/search/get/subgraph/blast-radius/criticality` |
| Ingestion | `edx ingest endpoints`, `edx ingest token` |
| Raw API | `edx api <METHOD> <path>` |

## Conventions

- **Output**: pretty JSON by default - parse it directly or pipe to `jq`.
  `--output table --columns a,b,c` for human summaries. See **Large outputs**
  below before parsing anything big.
- **Time ranges**: `--lookback 15m|1h|24h` (Go durations) or
  `--from/--to` ISO 8601 (`2006-01-02T15:04:05.000Z`).
- **Pagination**: list/search responses carry a cursor; a single page's count
  is a lower bound until the cursor comes back empty. Every cursor-paginated
  command takes `--all` to sweep the complete result set. See **Pagination**
  below.
- **Confirmations**: destructive commands (`deploy`, `delete`) prompt; add
  `--yes` in non-interactive contexts.
- **Limits**: default search limit is 20; raise with `--limit`.
- **Query fields use dot paths**: filter with `field.name:"value"` (e.g.
  `event.domain:"Monitor"`, `service.name:"api"`) even though the JSON response
  renders the same field with underscores (e.g. `event_domain`). Filter on the
  dotted form; read the underscore form. Confirm valid values with
  `edx facets options --scope <scope> --facet <field>`.

## Pagination - one page is a lower bound, --all is the total

Cursor pagination is one convention across the API, so every one of these
commands behaves the same way:

| Commands | Response array | Cursor field |
|---|---|---|
| `logs search`, `traces search`, `events search` | `items` | `next_cursor` |
| `monitors list` / `monitors states` | `monitors` / `states` | `next_cursor` |
| `rehydrations list` | `rehydrations` | `next_cursor` |
| `ai issues list/threads`, `ai channels list/messages`, `ai threads list/messages`, `ai workflows list`, `ai workflows runs list` | `data` | `nextCursor` |
| `ai knowledge search` | `data.matches` | `data.nextCursor` |

The contract:

- A response with a **non-empty cursor** has more results: the page you got is
  a lower bound, never a total. Continue by passing the cursor back as
  `--cursor` with the query and time flags unchanged; an **empty cursor**
  means the set is complete.
- Defaults silently under-read: `logs/traces/events search` default to
  `--limit 20`, and `monitors list` returns the server's 50. Any org with more
  than a page's worth looks like exactly the default until you check the
  cursor.
- **`--all` completes the read**: it follows the cursor until the set is
  complete and prints one combined `{<array>, pages, total_items,
  next_cursor: ""}` response, with per-page progress on stderr. A failed page
  is retried with backoff (search backends 500 transiently under load); if it
  still fails, the command exits non-zero, prints **nothing** (a partial sweep
  never masquerades as a total), and names the cursor to resume from with
  `--all --cursor`.
- **`--all` is capped at 10,000 items** (`--max-items`). Every page is a
  fresh search on the backend, so a broad raw-log sweep (a full-text query
  over 24h) can mean thousands of searches. When the cap is reached with
  results remaining, the command stops, prints nothing, exits non-zero and
  names the resume cursor. Don't raise the cap by reflex: to **count** or
  **trend**, use one aggregate query (`logs graph`, `metrics query`); to
  **summarize**, use `patterns list`; otherwise narrow the query or time
  range. Raise `--max-items` only when the user needs every item.
- Without `--all`, a page that leaves results behind prints a note on
  **stderr** (`more results exist: ...`). Keep stderr out of your jq pipe.
- Under `--all`, `--limit` is the page size; leave it unset for a sensible
  default.

```bash
# every monitor, not just the first server-default page
edx monitors list --all --output-file monitors.json
# a complete 30-day event sweep
edx events search -q 'event.domain:("Monitor" OR "Monitor Alerts")' \
  --lookback 720h --all --output-file alerts-30d.json
```

Not everything paginates: `patterns list/samples`, `metrics query`,
`logs graph` and `facets` return bounded or aggregated results with no cursor.

## Large outputs - write to a file, do not parse stdout

`edx` never truncates its own JSON, but the thing **reading** it often does:
an agent tool's captured stdout is capped, and a big response gets clipped
mid-token. The result parses as broken JSON (`Unterminated string ...`), which
looks like a malformed API response but is not one.

So for anything that can be large - a full monitor estate, a knowledge-graph
topology, a multi-page event sweep, wide `--lookback` searches - use
`--output-file` and read the file:

```bash
edx monitors list --all --output-file monitors.json
# stderr: wrote monitors.json (109741 bytes)
jq '.monitors | length' monitors.json
```

`--output-file` is a global flag (requires `edx` >= 0.20.0); it works on every
command and honors `--output` (`--output yaml --output-file x.yaml` writes
YAML). stdout stays empty, and the path plus byte count is confirmed on
stderr. Write and close errors are surfaced, so a file that exists is a file
that is complete.

Rules of thumb:

- Reach for `--output-file` **before** parsing, not after a parse fails.
- Never "fix" a truncated parse by re-running with a smaller `--limit` - that
  silently changes the answer. Write the full response to a file instead.
- `--output table` truncates individual cells to 120 characters for
  readability. Tables are for reading, not for extracting values - use JSON
  (to a file) when a full field value matters.

## Escape Hatch

For endpoints without a dedicated command:

```bash
edx api GET /v1/orgs/{org}/users          # {org} auto-substituted
edx api GET /tokens                       # org-relative shorthand
edx api POST /monitors --data @monitor.json
edx api GET /settings_v2/rehydration-settings
```

## Troubleshooting

| Problem | Fix |
|---------|-----|
| 401 Invalid authentication token | `edx auth login` with a valid token + matching org ID |
| 404 on a path | Check the org ID; try `edx api` with the full /v1 path |
| Empty search results | Widen `--lookback`; verify fields with `edx facets options` |
| 500 / "Failed to query ..." | Server-side error, not your query. Retry the same command; if it persists, narrow the time range. Do NOT keep editing the query - widening `--lookback` will not fix a 5xx. |
| Timeout on large queries | Narrow the time range or add `--timeout 120s` |
| "Unterminated string" / truncated JSON when parsing output | Not an API error - your stdout capture is capped. Re-run with `--output-file <path>` and parse the file |
| A count looks suspiciously round (20, 50, 1000) | You read one page. Check the cursor. For lists (monitors, events, rehydrations) re-run with `--all`; for log or trace **counts**, use `edx logs graph` instead of sweeping raw rows |
| `--all` stopped at `--max-items` | The result set is too big to sweep. Narrow the query or time range, or aggregate (`logs graph`, `patterns list`); raise `--max-items` only if every item is truly needed |
