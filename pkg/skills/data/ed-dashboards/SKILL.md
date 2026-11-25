---
name: ed-dashboards
description: Dashboards - create, update, inspect, validate and screenshot metric dashboards from the CLI.
metadata:
  version: "1.3.0"
  author: edgedelta
  repository: https://github.com/edgedelta/agent-skills
  tags: edgedelta,dashboards,metrics,visualization
  alwaysApply: "false"
---

# Edge Delta Dashboards

List, inspect, create, update, render and delete dashboards.

## Prerequisites

The `edx` CLI must be installed and authenticated. See the **ed-edx** skill.

## Inspect

```bash
edx dashboards list --output table --columns dashboard_id,dashboard_name,creator
edx dashboards get <dashboard-id>          # full definition - use as a template
```

## Build by Validating, Not by Guessing

**Do not write a dashboard JSON and hope it renders.** Validation is offline,
instant and needs no API call, so run it after every edit. A dashboard can
**save fine via the API yet fail to render** in the UI ("Dashboard could not be
found"), and validation is what catches that before you pollute the backend.

The loop:

```bash
# 1. Check the query on its own, before it goes anywhere near a dashboard.
edx cql validate --type metric 'sum:service.tokens{*} by {model}.rollup(60)'

# 2. Draft the definition, then validate after every edit. Repeat until clean.
edx dashboards validate --file dashboard.json

# 3. Only once it validates, create it - tagged, so it is identifiable.
edx dashboards create --file dashboard.json --tag generated --tag preview

# 4. Render it and look at the image. Slow, so run it in the background.
edx dashboards screenshot <id> --out shot.png

# 5. Once it looks right, drop the preview tag.
edx dashboards tag <id> --remove preview
```

Steps 1-3 are offline and fast. Steps 4-5 need a live backend and take a minute,
so treat them as a separate phase: see **Look At It** below.

Iterate on step 2 until it prints `definition is valid`. Errors are JSON
Pointers into your file, so fix exactly what is named and re-run:

```
$ edx dashboards validate --file dashboard.json
! /widgets/1/displayOptions: additional properties 'titel' not allowed
! /widgets/1/visualizer/type: value must be 'raw-table'
! /widgets/1/visualizer/type: value must be one of 'empty', 'json', 'table', 'list',
  'geomap', 'bignumber', 'gauge', 'pie', 'donut', 'column', 'radar', 'sunburst', ...
Error: dashboard definition failed validation (3 issue(s)); fix, or pass --skip-validation to override
```

Fix one reported path at a time and re-run rather than rewriting the file — the
error lists the accepted values, so you rarely have to guess.

One path can be reported more than once, as `visualizer/type` is above. Widget
shapes are variants, and some (like `raw-table`) accept a different set of
sibling fields, so each variant reports its own accepted values. Treat the
**union** of those messages as the valid set.

`validate` takes either a full dashboard body or a bare `definition` object, so
you can validate a draft before you have wrapped it in one.

`create` and `update` run the same checks automatically and refuse to submit a
bad definition. Validating first is still worth it: you iterate without network
round-trips.

### What validation does and does not catch

| Checked offline | Not checked |
| --- | --- |
| Definition structure, against the schema generated from the UI's own types | Whether a facet or metric name **exists** (needs the backend) |
| Unknown/misspelled keys, wrong enum values, wrong types | Whether a query returns any **data** |
| Query **syntax**, per data type, with the backend's own grammars | Whether a group-by dimension is **indexed** (see Gotchas) |
| | Whether the dashboard actually **looks** like anything (see **Look At It**) |

So a definition that validates is well-formed, not necessarily populated. After
creating it, confirm the queries actually return data with `edx metrics query`
or `edx logs search`, and render it to see the result.

One finding is reported as a **warning** and does **not** fail the command, so
read the output rather than only the exit code: `unsupported definition version`
means you got **no** schema validation at all. See below.

## Use `version: 4`

Author new dashboards with `definition.version: 4`. It is the current schema and
**the only version `edx` can validate**. Older versions still render (the UI
migrates them) but validation skips them entirely:

```
$ edx dashboards validate --file old.json
! unsupported definition version: got 3, edx can only validate version 4; skipping schema validation
definition is valid          # <- this says nothing was checked, not that it is correct
```

That output is a trap: exit code 0 with no schema check performed. If you see
it, bump the definition to version 4.

When you use `edx dashboards get <id>` as a template, check its `version` — an
older dashboard hands you an older schema.

## Start From an Example

Four validated examples ship with this skill in `examples/`. Copy the closest one
and edit it rather than writing a definition from scratch — each is a complete
dashboard body you can pass straight to `edx dashboards create --file`:

| File | Shows |
| --- | --- |
| `examples/01-minimal.json` | The smallest thing that renders: a root grid and one `bignumber`. |
| `examples/02-timeseries-with-variables.json` | A `line` chart, a `facet-option` variable and the `variable-control` widget that exposes it. |
| `examples/03-logs-and-markdown.json` | Mixed data types — a log count, a log breakdown `table`, a `markdown` panel. |
| `examples/04-formula.json` | Two hidden queries (`A`, `B`) plus a `formula` visual (`W`) dividing one by the other. |

All four are validated in CI, so they are correct for the schema this skill
describes. If you are stuck, `edx dashboards validate --file examples/01-minimal.json`
gives you a known-good baseline to diff against.

## Every Option

`type` fields are closed sets. These are the accepted values; the authoritative
list comes from the CLI, which reads the same schema validation enforces:

```bash
edx dashboards options                              # all of them, as JSON
edx dashboards options | jq -r '.visualizerTypes[]'
```

**Widget types** (`widgets[].type`) — 6:

| Value | Purpose |
| --- | --- |
| `grid` | Layout container. One with `id: "root"` holds everything else. |
| `viz` | A chart or number driven by queries. The main one. |
| `markdown` | Static text/HTML via `params.content`. |
| `variable-control` | Renders a variable's picker; needs `variableId`. |
| `tabs` | Tabbed container; children use `position.type: "tab"`. |
| `empty` | Placeholder. |

**Data types** (`visuals[].dataSource.type`) — 7. The query for each lives in
`params.query`, except `formula` which uses `params.formula`:

| Value | Query dialect for `edx cql validate --type` |
| --- | --- |
| `metric` | `metric` |
| `log` | `log` |
| `event` | `event` (same grammar as log) |
| `pattern` | `pattern` (same grammar as log) |
| `trace` | `trace` (same grammar as log) |
| `formula` | `formula` — references other visuals by id, e.g. `A / B` |
| `empty` | none; carries no query |

**Visualizer types** (`viz` widget's `visualizer.type`) — 23. Grouped by what
they are for; the `resultType` column is the pairing the shipped dashboards use:

| Group | Values | Usual `resultType` |
| --- | --- | --- |
| Single value | `bignumber`, `gauge` | `aggregate` |
| Over time | `line`, `area`, `bar`, `column`, `step`, `smooth`, `scatter` | `timeseries` |
| Proportion / breakdown | `pie`, `donut`, `treemap`, `sunburst`, `sankey`, `radar`, `bubble`, `boxplot` | `aggregate` |
| Tabular | `table`, `list`, `json` | `aggregate` |
| Raw rows | `raw-table` | `raw` |
| Map | `geomap` | `aggregate` |
| Placeholder | `empty` | `empty` |

`bignumber` also appears with `timeseries` in shipped dashboards (it renders the
latest point with a sparkline).

**Variable types** (`variables[].type`) — 6:

| Value | Meaning |
| --- | --- |
| `facet-option` | Dropdown of a facet's values. Needs `params.facet` and `params.scope`. |
| `facet` | Pick a facet *name* rather than a value. |
| `metric-name` | Pick a metric name. |
| `query` | A free-form filter expression, substituted into widget queries. |
| `string` | Free text; `params.options` restricts it to a list. |
| `duration` | A time span, e.g. for rollups. |

**Position types** (`widgets[].position.type`) — 5: `grid` (needs `area`), `tab`
(needs `index`), `none`, `subtitle`, `inline`.

**Result types** (`viz` widget's `resultType`) — 4: `aggregate`, `timeseries`,
`raw`, `empty`.

**Visual ids** (`visuals[].id`): `A`-`F` for queries, `W`-`Z` for formulas.

## Anatomy of a Metric Dashboard

```json
{
  "dashboard_name": "Service Usage",
  "description": "Tokens and cost",
  "definition": {
    "version": 4,
    "timeFilters": {"lookback": "1h"},
    "widgets": [
      {"id": "root", "type": "grid", "displayOptions": {"hideBackground": true},
       "grid": "72px 72px 72px / 1fr 1fr 1fr 1fr 1fr 1fr 1fr 1fr 1fr 1fr 1fr 1fr"},
      {"id": 1, "type": "viz", "displayOptions": {"title": "Total Tokens"},
       "position": {"area": {"column": 1, "columnSpan": 3, "row": 1, "rowSpan": 3},
                    "targetId": "root", "type": "grid"},
       "resultType": "aggregate",
       "visualizer": {"type": "bignumber"},
       "visuals": [{"id": "A",
                    "dataSource": {"type": "metric",
                                   "params": {"query": "sum:service.tokens{*}"}}}]}
    ]
  }
}
```

Key fields:

- **root grid** is 12 columns (`1fr` x12), rows sized `72px`. Each viz widget is
  placed with `position.area.{column,columnSpan,row,rowSpan}` (1-indexed). Add
  more `72px` row tracks as the dashboard grows.
- **`resultType`**: `aggregate` for a single value (bignumber), `timeseries` for
  a trend (line/area/bar), `raw` for a raw-table, `empty` for none.
- **`visualizer.type`**: `bignumber` | `line` | `area` | `bar` | `pie` | `table`
  | `gauge` | ... — validation lists every accepted value if you get it wrong.
- **`visuals[].id`**: `A`-`F` for queries, `W`-`Z` for formulas.

## Validating Queries On Their Own

Query syntax differs by data type and the grammars are **not** interchangeable —
a metric query is a syntax error in a log widget and vice versa. Check a query
before embedding it:

```bash
edx cql validate --type metric 'sum:ed.host.cpu{*} by {host.name}.rollup(60)'
edx cql validate --type log '{error AND ed.tag:$fleet} by {host.name}'
edx cql validate --type formula 'timeshift(q1, 3600) / q2'
```

`--type` takes the data type the widget targets: `log`, `event`, `pattern`,
`trace`, `metric` or `formula`. Errors point at a column with a caret:

```
$ edx cql validate --type metric 'sum:foo{*}.rollup(abc)'
! invalid metric query at column 18: mismatched input 'abc' expecting NUMBER
    sum:foo{*}.rollup(abc)
                      ^
```

To check every query in an existing dashboard at once:

```bash
edx dashboards get <id> \
  | jq -r '.definition.widgets[]?.visuals[]?.dataSource | select(.type=="metric") | .params.query // empty' \
  | edx cql validate --type metric --file -
```

`$variable` references are valid in a dashboard query and parse unsubstituted,
so you can validate a template as-is.

## Create / Update / Delete

```bash
edx dashboards create --file dashboard.json --tag generated --tag preview
edx dashboards update <dashboard-id> --file dashboard.json --tag generated   # update replaces the record, so re-pass tags
edx dashboards delete <dashboard-id> --yes
```

Fastest authoring path: `edx dashboards get <id>` of a working dashboard, swap
the widget queries and name, bump `version` to 4 if it is older, then validate
and `create`.

**Put the link in your reply** — an ID is not clickable. `create` returns
`dashboard_id`; the page is `https://app.edgedelta.com/dashboards/view/<id>`.

## Look At It (Post-Validation)

A definition that validates can still be an empty grid or a wall of "no data".
Rendering is the only way to find out — then **read the image**:

```bash
edx dashboards screenshot <id> --out shot.png        # whole dashboard, however tall
edx dashboards screenshot <id> --from ... --to ...   # pin the range to compare renders
edx dashboards screenshot <id> --facet env=prod      # set variables by key
```

**Run it in the background**: a render takes 30-60s, so start it, do other work
and collect the image. It waits up to `--wait` (default 3m), then exits **3**
with `"timed_out": true` — still rendering, not broken, so re-run later. Exit 1
is a real failure; 0 means the file is written.

| What you see | What it means |
| --- | --- |
| A widget says "no data" | Valid query, no match. Check the query and the time range. |
| One series where you expected a breakdown | The `by {...}` dimension is not indexed. See **Gotchas**. |
| Everything empty, though the queries return data elsewhere | Authorization: re-run `edx dashboards update` to regenerate `resource_accesses`, then render again. |
| A widget shows an error tile | Still captured, with `reported_error` set — that tile is the failing widget. |

`edx dashboards pdf <id>` covers the same ground for a **person** to open. It
needs a PDF renderer, so do not use it to check your own work.

## Gotchas

- Group-by breakdowns only work on **indexed** metric dimensions (see
  **ed-metrics**). `by {model}` on a non-indexed OTLP attribute collapses to a
  single series - break those down with logs or a dedicated `log_to_metric`.
  Validation cannot detect this; it is a data-modelling issue, not a syntax one.
- `--skip-validation` exists as an escape hatch, but a validation failure almost
  always means the dashboard won't render. Fix the definition instead; reach for
  the flag only when you have confirmed the validator is wrong.
- `position.targetId` is a widget reference, and validation checks its type but
  not that the target exists. Every shipped dashboard points grid-positioned
  widgets at the root grid widget (`"root"`), so follow that and confirm the
  target id is spelled the same in both places yourself.
